# Artesian System Architecture

Artesian is designed as a single, unified, high-performance Go daemon coupled with a Vue.js web control center. It replaces multi-container legacy game caching setups with an integrated, lightweight appliance.

```
                           +-------------------------------------+
                           |            Clients / LAN            |
                           +------------------+------------------+
                                              |
                   +--------------------------+--------------------------+
                   | DNS Queries (:53)                                   | HTTP Requests (:80)
                   v                                                     v
   +-------------------------------+                     +-------------------------------+
   |      Artesian DNS Server      |                     |    Artesian HTTP Proxy        |
   +---------------+---------------+                     +---------------+---------------+
                   |                                                     |
       +-----------+-----------+                             +-----------+-----------+
       |                       |                             |                       |
Non-Game Queries        Game CDN Domains                Cache HIT               Cache MISS
       |                       |                             |                       |
       v                       v                             v                       v
Upstream DNS           Redirect to Artesian          Zero-Copy Stream        Fetch Upstream CDN
(e.g., 1.1.1.1)        (A Record = Proxy IP)         from NVMe Cache         & Async Disk Cache
```

---

## 1. Subsystem Breakdown

### 1.1. Hexagonal Ports & Adapters Architecture (`internal/ports`, `internal/services`, `internal/adapters/*`)
Artesian follows Hexagonal Architecture (Ports & Adapters) to completely decouple game-platform specifics from the core proxy, prefill, and scheduling engines:
- **Port Interfaces (`internal/ports`)**: Define pure, platform-agnostic boundaries:
  - `NetworkPort`: Platform identity (`ID()`, `DisplayName()`, `Company()`, `Description()`), CDN domain wildcard interception patterns, host matching, and request cacheability checks.
  - `AuthPort`: Platform account authentication, 2FA challenges, mobile push approvals, and session persistence.
  - `LibraryPort`: Owned games discovery and per-platform capability resolution (`Windows`, `Mac`, `Linux`).
  - `MetadataPort`: Asset / depot sizing, base App ID resolution, periodic update check evaluation (`CheckAppUpdate`), and metadata queries.
  - `ContentPort`: CDN endpoint rotation, chunk URL generation, chunk key parsing, store/lookup links, and manifest chunk extraction.
  - `PlatformAdapter`: Composite interface unifying all ports into a single, cohesive contract.
- **Adapter Registry (`internal/services.Registry`)**:
  - Central registry holding all registered `PlatformAdapter` instances.
  - Core subsystems (`proxy`, `prefill`, `scheduler`, `api`, `dns`) interact strictly with `Registry`.
  - Adding a new gaming service (e.g., Battle.net, Riot, Xbox) only requires implementing `ports.PlatformAdapter` and calling `registry.Register(adapter)` at startup.
- **Steam Adapter (`internal/adapters/steam`)**:
  - `auth.go`: Direct Steam Auth API integration, RSA encryption, 2FA submission, push notification polling.
  - `cm.go`: Pure-Go WebSocket Steam Connection Manager client (`wss://.../cmsocket/`), logon, RPC manifest request codes.
  - `content.go`: CDN host rotation, chunk URL generation, manifest parsing and chunk hash extraction.
  - `games.go`: Owned game retrieval via `IPlayerService/GetOwnedGames/v1` and Steam Community XML fallback.
  - `metadata.go`: SteamCMD PICS / Store API queries, depot metadata caching, and platform formatting.
  - `network.go`: Steam CDN domain patterns and request cacheability checks (implements `ports.NetworkPort`).
  - `updates.go`: Periodic PICS change number polling and automated depot refresh.
- **Xbox / Microsoft Store Adapter (`internal/adapters/xbox`)**:
  - `auth.go`: MSA OAuth2 Device Code authentication flow (`microsoft.com/devicelogin`), XAU user token and XSTS target token exchange (`http://mp.microsoft.com/`, `http://xboxlive.com`, `http://update.xboxlive.com`).
  - `content.go`: Package Service relative archive resolution, 1MB byte-range chunk partitioning (`@range=start-end`), and full caching of `.phf` / `.ecd` descriptor files.
  - `games.go`: TitleHub user play history, Microsoft Store Entitlements query, dynamic Game Pass catalog signature discovery, and batch Display Catalog hydration.
  - `metadata.go`: Monolithic package depot metadata tracking, base App ID mappings, and update checks.
  - `network.go`: Delivery Optimization (`*.delivery.mp.microsoft.com`) and Xbox Live CDN wildcard domains and cacheability matching.
  - `packages.go`: Package resolution engine querying `packagespc.xboxlive.com/GetBasePackage/{contentId}`.
- **Epic Games Adapter (`internal/adapters/epic`)**:
  - `auth.go`: Windows Epic Games Launcher OAuth2 emulation, authorization code exchange, long-lived token refresh.
  - `content.go`: Epic CDN host rotation, `BuildChunkURL`, `ParseChunkKey`, signed token preservation.
  - `games.go`: Playable launcher assets query (`Live`), namespace filtering, batch catalog item resolution, and platform detection.
  - `manifest.go`: Binary zlib-compressed manifest parser (`0x44BEC00C`) with Struct-of-Arrays (SoA) unpacking, alongside decimal-blob JSON manifest decoding.
  - `metadata.go`: Asset manifest downloader, SQLite manifest chunk indexing, and update evaluation.
  - `network.go`: Epic CDN wildcard patterns (`*.epicgames.com`, `*.epicgamescdn.com`, `*.akamaized.net`).

> [!NOTE]
> **Epic Games Client Architecture**: The official Epic Games Launcher uses HTTPS with SSL certificate pinning and does not route chunk downloads through local HTTP caches. Epic Games LAN caching is designed for and requires **unofficial / third-party launchers** (such as [Legendary](https://github.com/derrod/legendary), [Heroic Games Launcher](https://heroicgameslauncher.com/), or [Rare](https://github.com/Dummerle/Rare)) configured to fetch manifests and chunks over plain HTTP.

### 1.2. DNS Interception & Forwarding (`internal/dns`)
- Binds to `:53` (UDP/TCP).
- Evaluates incoming QNAME against registered game service wildcard patterns:
  - Steam: `*.cs.steampowered.com`, `*.content.steampowered.com`, `*.steamcontent.com`
  - Xbox: `*.delivery.mp.microsoft.com`, `*.dl.delivery.mp.microsoft.com`, `*.do.dsp.mp.microsoft.com`
  - Epic: `*.epicgames.com`, `*.epicgamescdn.com`, `*.akamaized.net`
- **Target Match:** Synthesizes an immediate authoritative DNS `A` response pointing to Artesian's local network IP.
- **Non-Match:** Forwards the query transparently to upstream DNS resolvers (such as Cloudflare `1.1.1.1` or Google `8.8.8.8`) and relays the response back without delay.

### 1.3. HTTP Reverse Proxy & Chunk Cache (`internal/proxy`, `internal/cache`)
- Binds to `:80` (HTTP).
- Intercepts requests that were DNS-redirected to Artesian.
- Interacts with game platforms strictly through registered `ports.PlatformAdapter` instances via `services.Registry`.
- **Discrete Chunks (Steam & Epic):** Keys cache files by path (or chunk hash), serving directly from NVMe/SSD on cache hit, or streaming from upstream and caching asynchronously on cache miss.
- **Monolithic Byte Ranges (Xbox / Microsoft Store):** Inspects incoming HTTP `Range` headers, serves `206 Partial Content` directly from local range-sliced chunk files, and strips query signatures from cache keys to ensure persistent hits across SAS token rotations.
- Logs requests, response latency, and throughput into SQLite.

### 1.4. Prefill & Auto-Depot Caching Engine (`internal/prefill`)
- Automatically triggered upon adding games to the library or running on-demand prefill jobs.
- Consumes registered `ports.PlatformAdapter` instances via `services.Registry` to resolve manifests and build CDN chunk URLs across Steam, Xbox, and Epic.
- Retrieves manifests, hashes chunk identities, and executes rapid delta-diffing against local SQLite records.
- Concurrently downloads missing chunks into content-addressable storage using an internal worker pool.
- Seamlessly handles platform-specific directory fallbacks (e.g. Epic `ChunksV4` / `Chunks` directory swapping).
- Implements the `TrafficRecorder` interface to pipe live byte transfers, depot IDs, and completion events directly into the proxy's active download session monitor.

### 1.5. Active Download Tracking & WebSocket Hub (`internal/proxy`, `internal/ws`)
- Tracks ongoing client downloads and internal prefill operations in memory (`GameDownloadSession`).
- Differentiates internal prefill tasks by tagging `Client = "Artesian"`.
- Streams real-time progress via WebSocket events (`proxy:download_update`, `proxy:download_removed`).
- Automatically prunes completed sessions after a 3-5 second grace period to ensure the 100% completion state is visible to users before dismissal.

### 1.6. Automated Scheduler (`internal/scheduler`)
- Evaluates cron expressions for background update checks.
- Periodically queries platform APIs for patch updates to watched titles and triggers delta warming jobs across all registered platform adapters.

### 1.7. Control API & Web UI (`internal/api`, `web/`)
- Embeds a modern Vue 3 interface compiled with Vite.
- Includes the interactive Game Add modal featuring store tabs (Steam, Xbox, Epic), genuine owned game selection, Game Pass catalog inclusion, and platform detection.
- Displays live throughput, cache hit ratios, bandwidth savings, and the real-time Active Downloads monitor.
