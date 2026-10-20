# GEMINI.md — Artesian Agent Guidelines & Knowledge Base

This file provides critical architectural context, developer guidelines, conventions, and operational knowledge for future AI agents working on **Artesian**.

---

## 1. Project Overview & Architecture

**Artesian** is a unified, high-performance local network game content cache, DNS interceptor, and management engine written in **Go 1.24** and **Vue 3 (Vite)**. It replaces legacy multi-container caching solutions (like monolithic Nginx/SNI-proxy webs) with a single, highly integrated appliance.

### Core Stack
- **Backend**: Go 1.24 (`cmd/artesian/main.go`), SQLite with CGO (`mattn/go-sqlite3`), `gorilla/websocket`, `miekg/dns`.
- **Frontend**: Vue 3 (Composition API with `<script setup>`), Vanilla CSS design system (tokens, glassmorphism, dark mode), Vite. No Tailwind CSS. Embedded into the Go binary at compile-time using Go `embed.FS` (`web/embed.go`).
- **Data & Cache**: Content-addressable chunk storage (`internal/cache/`), SQLite database (`internal/db/`), DNS interceptor (`internal/dns/`), reverse proxy (`internal/proxy/`), prefill engine (`internal/prefill/`), scheduler (`internal/scheduler/`).

---

## 2. Fundamental Rules & Constraints

1. **No Backwards Compatibility / No Migration Logic**:
   - Do **NOT** write database schema migration scripts, version checks, or fallback wrappers.
   - All tables must be created with their final, correct schema at initial setup (`CREATE TABLE IF NOT EXISTS`).
   - If breaking changes are introduced, the database is wiped and recreated from scratch.
2. **Zero YAML Configuration / Database-Driven Settings**:
   - Do **NOT** use or create `.yaml` configuration files (`artesian.yaml`).
   - All settings and default values are seeded into SQLite tables (`config_settings` and `scheduler_rules`) at database initialization time (`db.Open()`).
   - Settings modified in the Web UI (Upstream DNS, Interception IP, Storage Path, Cache Quota, Scheduler Cron timers) are written directly to SQLite and persist across restarts.
3. **Per-Service & Per-App Auto-Update Variable**:
   - `auto_update` is a dynamic variable configured per-service and per-app, not hardcoded.
   - The platform service (Steam) has an `auto_update` toggle seeded into SQLite `config_settings` (`service_steam_auto_update`) and toggleable from the Accounts & CDN Modules UI.
   - Games in `watched_apps` store `auto_update` as a variable in SQLite, defaulting to the service's auto-update preference upon addition, and toggleable directly in the Game Library table.
   - The scheduler update check worker checks both the service-level auto-update setting and the app-level `auto_update` variable before running update checks.
4. **Game Update Check Subsystem (`update-check`)**:
   - The periodic game update detection rule is named `update-check` (with `task_type = "update_check"`).
   - Configurable via robfig/cron (`scheduler_rules` in SQLite) and editable in real-time from the Updates page UI.
   - **Update Log Silence on No Change**: Routine checks that result in no changes are silently dropped. The Update Log only records genuine discovered updates/patches and new game additions; `Update check completed...` messages are never logged to SQLite.
5. **Zero Fictional Data / Real Authentication Only**:
   - Never hardcode lists of games, mock credentials, or fake tokens.
   - All game titles, owned licenses, depot IDs, and authentication tokens must come from genuine account sessions or live API responses.
6. **Container Volumes & Persistence**:
   - The SQLite database is mounted at `/data/artesian.db` inside the container, mapped to `./data/db/artesian.db` on the host.
   - Cache storage is at `/data/cache/` inside the container, mapped to `./data/cache/` on the host.
   - **Crucial**: Always use `docker compose build && docker compose up -d`. Never start containers with ephemeral volumes, or the user's active Steam session will be lost.

---

## 3. Steam Authentication & Library Integration

### Authentication Flow (`internal/adapters/steam/auth.go`, `Steam2FAModal.vue`, `AccountsView.vue`)
- Connects directly to the Steam Authentication Service (`IAuthenticationService`) and Steam Connection Manager (CM).
- Handles username/password login and both Steam Mobile push notification ("Approve/Deny") challenges and manual code entries (Mobile TOTP authenticator or Email codes).
- **Mobile Push Notification Approval ("Approve / Deny")**:
  - When Steam returns mobile push confirmation (`AllowedConfirmations` with `ConfirmationType == 1`), `auth.go` launches a background polling goroutine (`pollForApproval`) calling `PollAuthSessionStatus` every ~1.5s.
  - Tapping **"Approve"** on the Steam Mobile app is detected automatically; Artesian exchanges the token via `/jwt/finalizelogin`, acquires `steamLoginSecure` cookies, saves the session to SQLite `auth_sessions`, and broadcasts `steam:status` over WebSockets to log in seamlessly without requiring manual code entry.
  - Tapping **"Deny"** (`EResult 15`) or expiration (`EResult 27`) terminates the poll cleanly and broadcasts an appropriate user notification.
- **Accurate Code Type Submission**:
  - Submitting codes manually via `UpdateAuthSessionWithSteamGuardCode` uses `code_type = 3` (`k_EAuthSessionGuardType_DeviceCode`) for 5-character Mobile Authenticator TOTP codes or `code_type = 2` for email codes, avoiding invalid code rejections caused by submitting push prompt type `1`.
- **2FA Modal UI (`Steam2FAModal.vue`)**:
  - Displays a dedicated Steam Mobile App approval card with a radar pulse animation ("Waiting for mobile approval...").
  - Presents a styled divider (`OR ENTER CODE MANUALLY`) above the 5-character code input with automatic uppercase conversion.
  - Automatically capitalizes all entered characters, disables inputs/buttons, and shows an animated loading spinner (`fa-circle-notch fa-spin`) while verifying.
- **Resuming Incomplete 2FA Setup**: When the Steam connection is in `"awaiting_2fa"` state, clicking the "Connect Steam Account" / "Enter 2FA / Approve" button in the Accounts tab skips the username/password form and routes directly to the 2FA code modal.
- Includes a "Different account? Restart login" option to abort pending 2FA and restart fresh if needed.
- Successful sessions are stored in SQLite in `auth_sessions` (keyed by `service`).
- Upon daemon boot, active sessions are restored automatically (`Restored genuine Steam session for <username>`).

### Dynamic Library Retrieval (`internal/adapters/steam/games.go`)
- Uses the authenticated user's access token with `IPlayerService/GetOwnedGames/v1` to pull the user's real owned game library (including App IDs and titles).
- Provides fallbacks to public Community XML/JSON profiles, cached depot metadata, and the local database.
- Queries platform availability (Windows, Mac, Linux) per game.

---

## 4. Game Library & Add Game Modal (`AddGameModal.vue`, `LibraryView.vue`)

- **Game Library Table**:
  - Columns: `Store`, `Title`, `Status`, `Auto-Update`, `Size` (on disk), `Last Sync`, `Actions`.
  - The `Auto-Update` column provides an interactive badge (`ENABLED` / `OFF`) allowing instant toggling per title.
  - The `Size` column reflects total downloaded chunk bytes on disk (`formatBytes(game.size_bytes)`), computed in SQLite via `COALESCE((SELECT SUM(c.size_bytes) FROM chunk_cache c WHERE c.app_id = w.app_id AND c.service = w.service), 0)`.
  - While prefill is running, `size_bytes` updates in real-time over WebSockets as chunks stream to disk.
- **Modal Scenarios**:
  - *No Connected Accounts*: Displays an informational alert guiding the user to authenticate an account.
  - *1 Authenticated Account (e.g. Steam)*: Shows a dedicated connected store header with store logo and username.
  - *Multiple Accounts*: Displays clickable store tabs with real-time loading states.
- **Game Dropdown List**:
  - Displays a clean native dropdown `<select>` element populated with live owned games from the connected account (`game.name (ID: game.app_id)`).
  - Handles loading states, empty account libraries, and API fetch error messages.
- **Platform Selection**:
  - Single-platform titles automatically lock and select the platform, enabling the "Add" button immediately.
  - Multi-platform titles present an interactive multi-select checkbox menu.
- **Game Deletion & Cache Reclaim (`DELETE /api/v1/library` & `DELETE /api/v1/library/cached`)**:
  - Available for both managed Library games and unmanaged Intercepted/Cached games.
  - Cancels active prefill workers and jobs immediately (`prefill.Manager.CancelJobsForApp`).
  - Removes and prunes active download sessions in proxy (`proxy.Server.CancelActiveDownloadsForApp`), broadcasting `proxy:download_removed` to remove UI rows instantaneously.
  - Deletes all cached chunk files on disk for all associated depots (`storage.DeleteDepot`).
  - Purges manifest files (`<depot_id>_*.manifest`) from disk pools (`manifest.DeleteManifestFiles`).
  - Deletes SQLite records from `depot_manifest_chunks`, `chunk_cache`, `watched_apps`, and `depot_meta_cache`.
  - Broadcasts `library:updated` and `stats:updated` over WebSocket so library state, disk size, and cache metrics update in real-time.
- **Cached Games Grouping & Accurate Platform Detection (`GetInterceptedGames`)**:
  - Groups chunks by resolved effective `app_id` (via `depot_meta_cache`), preventing duplicate rows caused by early chunks arriving before Steam PICS depot metadata resolution.
  - `UpsertDepotMeta` retroactively backfills `chunk_cache.app_id` for all chunks associated with the depot.
  - Dynamically detects the genuine platform (`Mac 64bit`, `Windows`, `Linux`) directly from the depots actually downloaded in `chunk_cache`, preventing Mac downloads from displaying Windows icons.

---

## 5. Auto-Prefill & Active Download Monitoring

### Full Depot Sizing & Automatic Caching on Add
- When a game is added (`POST /api/v1/library`), the app status is set to `"prefilling"` and a prefill job is triggered immediately via `prefill.Manager.StartJob`.
- The prefill manager runs on an independent detached context (`context.WithCancel(context.Background())`) to prevent premature cancellation by short-lived HTTP handler contexts.
- Adding a game keeps the user on the Game Library tab so they can continue queueing games without UI disruption.
### Full Depot Manifest Resolution & Genuine CDN Chunk Prefill
- When a game is added (`POST /api/v1/library`), the app status is set to `"prefilling"` and a prefill job is triggered immediately via `prefill.Manager.StartJob`.
- The prefill manager runs on an independent detached context (`context.WithCancel(context.Background())`) to prevent premature cancellation by short-lived HTTP handler contexts.
- Adding a game keeps the user on the Game Library tab so they can continue queueing games without UI disruption.
- **Genuine Manifest Parsing & Storage (`internal/manifest/manifest.go`)**:
  - Steam manifest files (`<depot_id>_<manifest_id>.manifest`) are ZIP archives with a magic header `0x71F617D0` wrapping a protobuf payload (`ContentManifestPayload`).
  - While file mapping paths require AES depot decryption keys, chunk records (`sha` 20-byte SHA-1, `cb_original`, `cb_compressed`) are **unencrypted plaintext**.
  - `manifest.ParseManifestBytes` extracts genuine 40-character SHA-1 chunk hashes and true sizes directly.
  - Manifests are resolved from:
    1. SQLite indexed cache: `depot_manifest_chunks` (`depot_id`, `manifest_id`, `chunk_id`, `size_bytes`).
    2. Artesian manifest pool at `/data/manifests/` and `/data/cache/manifests/`.
    3. Proxy intercepted live manifests from `/depot/<depot_id>/manifest/<manifest_id>/...`.
    4. Native pure-Go Steam CM WebSocket engine (`internal/adapters/steam/cm.go`) querying RPC `ContentServerDirectory.GetManifestRequestCode` on demand.
- **Headless Cross-OS Prefilling via Pure-Go Steam CM Client (`internal/adapters/steam/cm.go`, `internal/prefill/prefill.go`)**:
  - Eliminates host OS restrictions (e.g. allows a macOS host or headless Linux server to prefill Windows or Linux games without Steam client installed on the host).
  - Clean, all-Golang native implementation: **zero** external C# / .NET / third-party prefill CLI binaries.
  - Connects directly to Steam Connection Managers (CM) via WebSockets (`wss://<cm-endpoint>/cmsocket/`).
  - Authenticates with genuine session credentials stored in SQLite `auth_sessions` via `k_EMsgClientLogon` (5514).
  - Transparently uncompresses and dispatches `k_EMsgMulti` (EMsg 1) GZIP message bundles.
  - Queries Steam CM via Unified Service RPC `ContentServerDirectory.GetManifestRequestCode#1` (EMsg 151 / 147) for the depot's rotating `manifest_request_code`.
  - Streams the genuine manifest ZIP directly from Steam CDN endpoints (`/depot/<depot_id>/manifest/<manifest_id>/5/<manifest_request_code>`) using dedicated upstream DNS resolution.
  - Automatically parses chunk hashes and sizes via `manifest.ParseManifestBytes`, saving to disk pool and database (`depot_manifest_chunks`).
  - Seamlessly triggers the native Go concurrent chunk downloader pool to pre-cache all missing chunks.
- **Unified Chunk Architecture**:
  - Prefill does **NOT** synthesize dummy chunks.
  - Prefill streams authentic encrypted chunks directly from genuine Steam CDN endpoints (`cache9-lhr1.steamcontent.com`, `alibaba.cdn.steampipe.steamcontent.com`, etc.) into `/data/cache/steam/<depot_id>/<prefix1>/<prefix2>/<chunk_hash>`.
  - Chunks are recorded in SQLite `chunk_cache`.
  - When the user subsequently installs or updates in Steam, every requested chunk matches the prefilled chunk file on local disk, resulting in a **100% CACHE HIT** at maximum LAN speed!

### Active Download Routing & Loopback Prevention
- `internal/prefill/prefill.go` defines the `TrafficRecorder` interface:
  ```go
  type TrafficRecorder interface {
      RecordPrefillProgress(clientIP, service, gameTitle, platform string, depotID int64, appID int64, chunkID string, bytesTransferred, totalBytes int64)
      CompletePrefillSession(clientIP, service string, appID int64)
  }
  ```
- `proxy.Server` implements `TrafficRecorder`. In `cmd/artesian/main.go`, `prefillMgr.SetTrafficRecorder(proxyServer)` connects them.
- **Dedicated Upstream DNS for Prefill**: Prefill uses a custom HTTP transport with dialer configured to query `cfg.DNS.UpstreamDNS` (e.g. `1.1.1.1:53`). This ensures prefill downloads connect directly to Valve's public CDN servers rather than looping back into Artesian's own local DNS interceptor on port 80.
- **Loopback Traffic Suppression**: All internal container requests (`127.0.0.1`, `::1`, `X-Artesian-Prefill`, `X-Artesian-Loop`) are excluded from proxy logging and `activeDownloads` session creation. This completely prevents phantom duplicate download rows (`127.0.0.1` alongside `Artesian`).
- **Platform-Accurate Sizing & Depots**: Prefill sets `job.Platform` from the user's selected platform (`Mac`, `Windows`, `Linux`) and filters out obsolete 32-bit legacy depots when 64-bit depots exist, ensuring prefill progress and size match the genuine client install byte-for-byte.

### Active Downloads UI (`StatsView.vue` & `useDownloads.js`)
- **Client Column**: Detects `cleanClientIP(d.client_ip) === 'Artesian'` and displays:
  `<span class="client-badge-artesian"><i class="fa-solid fa-server mr-1"></i> Artesian</span>`
- **Depot ID Column**: Clickable link to SteamDB (`https://steamdb.info/depot/<depot_id>`).
- **Size Column**: Displays transferred / total bytes, a percentage badge, and an animated mini progress bar.
- **Completion Grace Period**: When a download completes (100%), both the backend and frontend enforce a 3–5 second grace period so the user can verify completion before the row is cleanly pruned.
- **Smooth Rendering & Stable Ordering**: `useDownloads.js` and `proxy.Server` sort active downloads by initial start time (`FirstSeen`) descending, updating progress, bytes, and speeds strictly in-place. Rows never swap positions or jitter when concurrent prefill jobs and live client downloads stream chunks simultaneously. Loopback IPs (`127.0.0.1`) are ignored from the active downloads list.

---

## 6. Epic Games Adapter & Pipeline (`internal/adapters/epic/`)

### Authentication & OAuth2 Launcher Emulation (`auth.go`, `EpicLoginModal.vue`)
- Emulates the official Windows Epic Games Launcher OAuth2 client credentials (`client_id = 34a02cf8f4414e29b15921876da36f9a`, `client_secret = daafbccc737745039dffe53d94fc76cf`).
- User opens the official Epic authorization link in browser and pastes the resulting authorization code (or complete JSON payload) into Artesian.
- Exchanged via `POST https://account-public-service-prod03.ol.epicgames.com/account/api/oauth/token` (`grant_type=authorization_code`).
- Session is stored in SQLite `auth_sessions` (`service = 'epic'`). Long-lived `refresh_token` allows scheduled background update checks and prefill jobs without user re-authentication.

### Owned Library & Catalog Resolution (`games.go`)
- Fetches playable assets from `GET https://launcher-public-service-prod06.ol.epicgames.com/launcher/api/public/assets/Windows?label=Live`.
- Filters out Unreal Engine marketplace items (`namespace == "ue"`).
- Resolves human-readable titles, box art, and platform support by querying `GET /catalog/api/shared/namespace/{namespace}/bulk/items` in batches of 50.
- Caches catalog information in SQLite `game_meta_cache`.
- Produces stable positive 63-bit `app_id` integers via `HashAppName` (`fnv64a(appName) & 0x7FFFFFFFFFFFFFFF`).

### Binary Manifest Parsing & Chunk Resolution (`manifest.go`, `metadata.go`, `content.go`)
- Queries asset manifests via `GET /launcher/api/public/assets/v2/platform/Windows/namespace/{namespace}/catalogItem/{catalogItemId}/app/{appName}/label/Live`.
- Retrieves the manifest file with signed CDN tokens and parses modern Epic **zlib-compressed binary manifests** (`Magic == 0x44BEC00C`):
  - Evaluates `FeatureLevel` (`<13`: `Chunks`, `13-14`: `ChunksV2`, `15-17`: `ChunksV3`, `>=18`: `ChunksV4`).
  - Unpacks `ChunkDataList` Struct-of-Arrays (SoA): GUIDs (16 bytes, `%08X%08X%08X%08X`), Hashes (8 bytes, `%016X`), ShaHashes (20 bytes), GroupNumbers (`%02d`), WindowSizes, FileSizes.
- Stores parsed chunks in SQLite `depot_manifest_chunks`.
- Constructs chunk CDN URLs: `{BaseDir}/{ChunksV4}/{GroupNumber}/{Hash}_{GUID}.chunk`.

### Host-Agnostic Chunk Caching (`cache.go`, `proxy.go`, `prefill.go`)
- Epic dynamically routes downloads across `epicgames-download*.akamaized.net`, `download.epicgames.com`, and `fastly-download.epicgames.com`.
- Proxy and prefill cache keys index strictly by URL path (`req.URL.Path`), stripping the `Host` header and temporary query-string auth tokens.
- Chunks are stored in `/data/cache/epic/{prefix1}/{prefix2}/{Hash}_{GUID}.chunk`, guaranteeing a 100% cache hit regardless of CDN domain.

---

## 7. Build & Deployment Commands

```bash
# 1. Build frontend assets
cd web
npm run build
cd ..

# 2. Rebuild and restart the Docker container appliance
docker compose build
docker compose up -d

# 3. View daemon logs
docker compose logs -f --tail 50

# 4. Check container status and open ports
docker compose ps
```

---

## 8. Key File Map

| Path | Purpose |
|---|---|
| `cmd/artesian/main.go` | Entrypoint, orchestrator wiring, composition root |
| `internal/ports/ports.go` | Hexagonal port interfaces (`PlatformAdapter`, `NetworkPort`, `AuthPort`, `LibraryPort`, `MetadataPort`, `ContentPort`) |
| `internal/services/service.go` | Platform adapter registry (`Registry`), multi-platform host identification and routing |
| `internal/adapters/steam/adapter.go` | Hexagonal Steam Adapter constructor, state struct, dependency holder |
| `internal/adapters/steam/auth.go` | Steam Web API authentication, Steam Guard 2FA, session persistence in SQLite |
| `internal/adapters/steam/cm.go` | Native Go Steam CM WebSocket client, logon protocol, manifest request code RPC |
| `internal/adapters/steam/content.go` | CDN hosts pool, chunk URL builder, chunk key regex, manifest chunk resolution |
| `internal/adapters/steam/games.go` | Dynamic owned game fetching via Steam Web API & XML community profile |
| `internal/adapters/steam/metadata.go` | SteamCMD PICS & Store API info, depot metadata caching, base App ID resolution |
| `internal/adapters/steam/network.go` | Steam CDN wildcard domains, host matching, cacheability inspection |
| `internal/adapters/epic/adapter.go` | Hexagonal Epic Adapter constructor, state struct, `HashAppName` mapper |
| `internal/adapters/epic/auth.go` | Epic OAuth2 Launcher emulation, auth code parser, token exchange & refresh |
| `internal/adapters/epic/content.go` | Epic CDN hosts, `BuildChunkURL`, `ParseChunkKey`, chunk resolver |
| `internal/adapters/epic/games.go` | Epic live owned assets fetch, catalog bulk resolution, platform detector |
| `internal/adapters/epic/manifest.go` | Binary zlib manifest parser (`0x44BEC00C`), `FeatureLevel` router, SoA unpacker |
| `internal/adapters/epic/metadata.go` | Epic V2 assets API manifest retriever, manifest download & chunk recorder |
| `internal/adapters/epic/network.go` | Epic CDN domains (`akamaized.net`, `epicgames.com`), cacheability matcher |
| `internal/api/api.go` | HTTP endpoints, library routes, generic auth routes, WebSocket hub handler |
| `internal/proxy/proxy.go` | HTTP caching proxy, host-agnostic path caching, active download sessions |
| `internal/manifest/manifest.go` | Protobuf & ZIP manifest parser, candidate search, raw manifest persistence |
| `internal/prefill/prefill.go` | Prefill engine, multi-platform chunk downloader via adapter registry |
| `internal/scheduler/scheduler.go` | robfig/cron background update check scheduler across all registered platform adapters |
| `internal/db/db.go` | SQLite schema, watched apps, depot meta, chunk records, manifest chunks, auth sessions |
| `internal/ws/hub.go` | Real-time WebSocket event broadcaster |
| `web/src/components/modals/EpicLoginModal.vue` | Epic Games OAuth2 code login modal with direct authorization link |
| `web/src/components/modals/AddGameModal.vue` | Multi-store Game Add modal with interactive text filter input |
| `web/src/views/AccountsView.vue` | Accounts hub with multi-store connect/disconnect and monitoring toggles |
| `web/src/views/StatsView.vue` | Dashboard with Active Downloads table, Artesian badge, progress bar |
| `web/src/composables/useDownloads.js` | Active downloads state, real-time chunk bundling, auto-pruner |
| `docker-compose.yml` | Appliance deployment definition with persistent host volume mounts |
