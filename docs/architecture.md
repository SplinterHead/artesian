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

### 1.2. DNS Interception & Forwarding (`internal/dns`)
- Binds to `:53` (UDP).
- Evaluates incoming QNAME against registered game service wildcard patterns (e.g. `*.cs.steampowered.com`).
- **Target Match:** Synthesizes an immediate authoritative DNS `A` response pointing to Artesian's local network IP.
- **Non-Match:** Forwards the query transparently to upstream DNS resolvers (such as Cloudflare `1.1.1.1` or Google `8.8.8.8`) and relays the response back without delay.

### 1.3. HTTP Reverse Proxy & Chunk Cache (`internal/proxy`, `internal/cache`)
- Binds to `:80` (HTTP).
- Intercepts requests that were DNS-redirected to Artesian.
- Interacts with game platforms strictly through registered `ports.PlatformAdapter` instances via `services.Registry`.
- **Cache Hit:** Directly serves content from disk with `X-Artesian-Cache: HIT`.
- **Cache Miss:** Forwards request to upstream game CDN, tee-streams response back to client, and asynchronously stages the chunk to disk storage.
- Logs requests, response latency, and throughput into SQLite.

### 1.4. Prefill & Auto-Depot Caching Engine (`internal/prefill`)
- Automatically triggered upon adding games to the library.
- Consumes registered `ports.PlatformAdapter` instances via `services.Registry` to resolve manifests and build CDN chunk URLs.
- Retrieves depot manifests, hashes chunk identities, and executes rapid delta-diffing against local SQLite records.
- Concurrently downloads missing chunks into content-addressable storage using an internal worker pool.
- Implements the `TrafficRecorder` interface to pipe live byte transfers, depot IDs, and completion events directly into the proxy's active download session monitor.

### 1.6. Active Download Tracking & WebSocket Hub (`internal/proxy`, `internal/ws`)
- Tracks ongoing client downloads and internal prefill operations in memory (`GameDownloadSession`).
- Differentiates internal prefill tasks by tagging `Client = "Artesian"`.
- Streams real-time progress via WebSocket events (`proxy:download_update`, `proxy:download_removed`).
- Automatically prunes completed sessions after a 3-5 second grace period to ensure the 100% completion state is visible to users before dismissal.

### 1.7. Automated Scheduler (`internal/scheduler`)
- Evaluates cron expressions for background update checks.
- Periodically queries platform APIs for patch updates to watched titles and triggers delta warming jobs.

### 1.8. Control API & Web UI (`internal/api`, `web/`)
- Embeds a modern Vue 3 interface compiled with Vite.
- Includes the interactive Game Add modal featuring an adaptive text filter input, keyboard navigation, and platform selectors.
- Displays live throughput, cache hit ratios, bandwidth savings, and the real-time Active Downloads monitor.
