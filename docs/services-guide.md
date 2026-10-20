# Game Services & Platform Guide

Artesian is purpose-built and streamlined for the **Steam** gaming platform. This guide covers CDN domain patterns, chunk caching mechanics, and authentication integration.

---

## Steam (Valve)

### Overview
The Steam client downloads game payloads over plain HTTP using content-addressable chunks (typically 1MB compressed).

### Intercepted Domains
- `*.cs.steampowered.com`
- `*.content.steampowered.com`
- `*.hsar.steampowered.com`
- `content*.steampowered.com`
- `*.steamcontent.com`

### Chunk URL Structure
Steam chunk requests follow the format:
```
GET /depot/<depot_id>/chunk/<sha1_chunk_hash> HTTP/1.1
Host: <cdn_host>.cs.steampowered.com
```

### Authentication & Dynamic Library Sync
In addition to CDN proxying and caching, Artesian natively integrates with Steam's account backend:
- **Connection Manager (CM)**: Establishes a direct pure-Go WebSocket session to Valve's CM infrastructure (`wss://...`).
- **Steam Guard 2FA & Push Approval**: Supports real-time mobile push approval polling, mobile TOTP authenticator codes, and email verification codes.
- **Session Persistence**: Session login keys are preserved securely in SQLite (`steam_auth_sessions`) and automatically restored across daemon restarts.
- **Owned Game Discovery**: Queries Valve's `IPlayerService/GetOwnedGames/v1` API with the authenticated user token to dynamically populate the user's authentic game library (with titles, App IDs, and supported operating systems).
- **Headless Manifest Resolution**: Queries Steam CM Unified Service RPCs (`ContentServerDirectory.GetManifestRequestCode`) to fetch genuine manifest files and prefill game chunks directly from CDN endpoints.
