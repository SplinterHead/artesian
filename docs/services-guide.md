# Game Services & Platform Guide

Artesian includes pluggable service definitions for major game distribution platforms. Each platform module defines CDN domain patterns, chunk caching constraints, and update detection logic.

---

## 1. Steam (Valve)

### Overview
The Steam client downloads game payloads over plain HTTP using content-addressable chunks (typically 1MB compressed).

### Intercepted Domains
- `*.cs.steampowered.com`
- `*.content.steampowered.com`
- `*.hsar.steampowered.com`
- `content*.steampowered.com`
- `*.steamcontent.com`

### Chunk URL Structure
Steam chunk requests generally follow the format:
```
GET /depot/<depot_id>/chunk/<sha1_chunk_hash> HTTP/1.1
Host: <cdn_host>.cs.steampowered.com
```
### Authentication & Dynamic Library Sync
In addition to CDN proxying, Artesian natively integrates with Steam's account backend:
- **Connection Manager (CM)**: Establishes a direct WebSocket session to Valve's CM infrastructure (`wss://...`).
- **Steam Guard 2FA**: Supports real-time mobile app and email two-factor authentication codes.
- **Session Persistence**: Session login keys are preserved securely in SQLite (`steam_auth_sessions`) and automatically restored across daemon restarts.
- **Owned Game Discovery**: Queries Valve's `IPlayerService/GetOwnedGames/v1` API with the authenticated user token to dynamically populate the user's authentic game library (with titles, App IDs, and supported operating systems).

---

## 2. Epic Games Store

### Overview
Epic Games Launcher distributes game binaries and patches across Fastly and Cloudflare CDNs.

### Intercepted Domains
- `*.download.epicgames.com`
- `*.ol.epicgames.com`

### Chunk URL Structure
Epic uses chunked manifest files (`.manifest`) and hash-addressed `.chunk` files under designated bucket paths.

---

## 3. Battle.net / Blizzard Entertainment

### Overview
Blizzard Battle.net distributes games (World of Warcraft, Diablo, Overwatch, Call of Duty) via content-addressable data files.

### Intercepted Domains
- `*.dist.blizzard.com`
- `*.cdn.blizzard.com`

---

## 4. Riot Games

### Overview
Riot Games Launcher distributes League of Legends, Valorant, and Teamfight Tactics.

### Intercepted Domains
- `*.dyn.riotcdn.net`
- `*.content.riotgames.com`

---

## 5. Xbox / Microsoft Store

### Overview
Microsoft Store and Xbox PC app distribute game packages via Microsoft Delivery Optimization.

### Intercepted Domains
- `*.assets1.xboxlive.com`
- `*.delivery.mp.microsoft.com`

---

## 6. Adding New Services

To register a new game platform in Artesian:
1. Implement the `services.Service` interface in `internal/services/<platform>.go`:
   - `Name() string`
   - `MatchesHost(host string) bool`
   - `Domains() []string`
   - `IsCacheable(req *http.Request) bool`
2. Register the service instance in `internal/services/registry.go` or `cmd/artesian/main.go`.
3. Add configuration toggles to `configs/artesian.yaml`.
