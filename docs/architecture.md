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

### 1.1. DNS Interception & Forwarding (`internal/dns`)
- Binds to `:53` (UDP).
- Evaluates incoming QNAME against registered game service wildcard patterns (e.g. `*.cs.steampowered.com`).
- **Target Match:** Synthesizes an immediate authoritative DNS `A` response pointing to Artesian's local network IP.
- **Non-Match:** Forwards the query transparently to upstream DNS resolvers (such as Cloudflare `1.1.1.1` or Google `8.8.8.8`) and relays the response back without delay.

### 1.2. HTTP Reverse Proxy & Chunk Cache (`internal/proxy`, `internal/cache`)
- Binds to `:80` (HTTP).
- Intercepts requests that were DNS-redirected to Artesian.
- Preserves client context (`X-Forwarded-For`), matches host against platform modules, and verifies chunk existence in the content-addressable storage.
- **Cache Hit:** Directly serves content from disk with `X-Artesian-Cache: HIT`.
- **Cache Miss:** Forwards request to upstream game CDN, tee-streams response back to client, and asynchronously stages the chunk to disk storage.
- Logs requests, response latency, and throughput into SQLite.

### 1.3. Service Provider Engine (`internal/services`)
- Pluggable platform abstractions for game networks (Steam, Epic Games, Battle.net, Riot, Xbox).
- Handles domain pattern resolution, chunk URL patterns, and depot layout rules.

### 1.4. Steam Authentication & Account Client (`internal/steam`)
- Direct WebSocket connection to Steam Connection Manager (CM) servers.
- Handles user authentication, Steam Guard mobile/email 2FA code verification, and persistent session restoration from SQLite.
- Integrates with the Steam Web API (`IPlayerService/GetOwnedGames/v1`) using the authenticated user token to pull genuine game libraries, App IDs, and supported platforms (Windows, Mac, Linux).

### 1.5. Prefill & Auto-Depot Caching Engine (`internal/prefill`)
- Automatically triggered upon adding games to the library.
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
