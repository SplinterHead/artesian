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

### 1.4. Prefill & Cache Warming Engine (`internal/prefill`)
- Allows users to initialize and pre-populate local game depots before LAN events or client downloads.
- Coordinates manifest downloads and missing chunk fetching.

### 1.5. Automated Scheduler (`internal/scheduler`)
- Evaluates cron expressions for background update checks.
- Periodically queries platform APIs for patch updates to watched titles and triggers delta warming jobs.

### 1.6. Control API & Web UI (`internal/api`, `web/`)
- Embeds a modern Vue 3 interface.
- Exposes REST endpoints for configuration, manual prefill triggers, telemetry, and live stats.
