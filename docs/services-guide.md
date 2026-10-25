# Game Services & Platform Guide

Artesian is a unified local network game content cache, DNS interceptor, and management engine supporting **Steam**, **Xbox & Microsoft Store**, and **Epic Games**.

This guide covers CDN domain patterns, chunk caching mechanics, authentication pipelines, and client compatibility across each supported platform.

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
Steam chunk requests follow the format:
```http
GET /depot/<depot_id>/chunk/<sha1_chunk_hash> HTTP/1.1
Host: <cdn_host>.cs.steampowered.com
```

### Authentication & Dynamic Library Sync
In addition to CDN proxying and caching, Artesian natively integrates with Steam's account backend:
- **Connection Manager (CM)**: Establishes a direct pure-Go WebSocket session to Valve's CM infrastructure (`wss://...`).
- **Steam Guard 2FA & Push Approval**: Supports real-time mobile push approval polling, mobile TOTP authenticator codes, and email verification codes.
- **Session Persistence**: Session login keys are preserved securely in SQLite (`auth_sessions`) and automatically restored across daemon restarts.
- **Owned Game Discovery**: Queries Valve's `IPlayerService/GetOwnedGames/v1` API with the authenticated user token to dynamically populate the user's authentic game library (with titles, App IDs, and supported operating systems).
- **Headless Manifest Resolution**: Queries Steam CM Unified Service RPCs (`ContentServerDirectory.GetManifestRequestCode`) to fetch genuine manifest files and prefill game chunks directly from CDN endpoints without requiring a local Steam client.

---

## 2. Xbox & Microsoft Store

### Overview
Xbox and Microsoft Store titles are distributed as monolithic packages (`.msixvc`, `.xvc`) along with small piece hash (`.phf`) and encryption descriptor (`.ecd`) files. Content is delivered via Microsoft Delivery Optimization (DO) and Azure CDN edges using HTTP byte-range requests.

### Intercepted Domains
- `*.dl.delivery.mp.microsoft.com`
- `*.delivery.mp.microsoft.com`
- `dl.delivery.mp.microsoft.com`
- `*.do.dsp.mp.microsoft.com`
- `packagespc.xboxlive.com`

### Chunk & Byte-Range Slicing Architecture
Because Microsoft Store and Xbox distribute games as monolithic multi-gigabyte archives rather than discrete chunk files, Artesian utilizes a **1MB byte-range chunk slicer**:
- Monolithic archive URLs are partitioned into uniform 1MB (1,048,576 byte) blocks:
  ```
  {archive_path}@range={start}-{end}
  ```
- Ancillary files (`.phf`, `.ecd`) are pre-cached in full alongside the main archive.
- Temporary query string tokens (SAS tokens like `?P1=...&P2=...`) are stripped from cache storage keys, ensuring persistent cache hits across token rotations.
- Artesian's HTTP proxy intercepts incoming HTTP `Range: bytes=start-end` requests from Delivery Optimization clients and streams `206 Partial Content` directly from local NVMe/SSD cache at maximum LAN speed.

### Authentication & Catalog Discovery
- **MSA OAuth2 Device Code Flow**: Authenticates using the official public Xbox App client credentials (`client_id = 00000000402b5328`) with scope `service::user.auth.xboxlive.com::MBI_SSL offline_access`. Provides a frictionless copyable code with direct link to `https://microsoft.com/devicelogin`.
- **Dual-Token Exchange (XAU → XSTS)**:
  - Exchanges MSA token for Xbox Live User Token (XAU) and user hash (`uhs`).
  - Issues target-scoped XSTS tokens for `http://mp.microsoft.com/` (Store Catalog), `http://xboxlive.com` (TitleHub user history), and `http://update.xboxlive.com` (Package Service CDN resolution).
- **Personal Library & Game Pass Separation**:
  - Displays user's personal owned game library by default (retrieved from TitleHub and Microsoft Entitlements).
  - Offers an on-demand **"Include Game Pass"** toggle in the Add Game modal that enumerates active PC and Console Game Pass catalog signatures when the user possesses an active subscription.
- **Display Catalog Hydration**: Batch queries `displaycatalog.mp.microsoft.com` to resolve human-readable titles, Content GUIDs, package architectures, and platform compatibility (`Windows`, `Xbox Series X|S`, `Xbox One`).

---

## 3. Epic Games

### Overview
Epic Games titles are distributed via content-addressable binary chunk files (`.chunk`) indexed by GUID and 64-bit integer hashes, referenced in asset manifests.

### Intercepted Domains
- `*.epicgames.com`
- `*.epicgamescdn.com`
- `*.akamaized.net`
- `download.epicgames.com`
- `egs-cloudfront-chunks.epicgamescdn.com`
- `egdownload.fastly-edge.com`
- `cloudflare.epicgamescdn.com`

### Chunk URL Structure
Epic Games chunks are served under versioned subdirectories (modern packages use `ChunksV4`):
```http
GET {base_cdn_url}/ChunksV4/{group_number}/{hash}_{guid}.chunk HTTP/1.1
Host: egs-cloudfront-chunks.epicgamescdn.com
```

### Manifest Parsing & Chunk Resolution
Artesian supports both modern and legacy Epic Games manifest structures:
- **Binary zlib Manifests (`0x44BEC00C`)**: Parses compressed Struct-of-Arrays (SoA) containing chunk GUIDs, 64-bit hashes, SHA-1 checksums, and data groups across feature levels 13 through 18+.
- **JSON Manifests with Decimal Blob Encoding**: Dynamically decodes 3-digit little-endian decimal byte sequences (e.g. `"000000016000000000000000"`), hex strings, and numeric types for legacy titles.
- **Automated Directory Fallback**: Prefill automatically navigates between `ChunksV4`, `Chunks`, `ChunksV3`, and `ChunksV2` if CDN paths differ.

---

### IMPORTANT: Client Compatibility & Unofficial Launcher Requirement

> [!WARNING]
> **The official Epic Games Launcher is NOT supported for cache hits.**
>
> **Why?**
> The official Windows and macOS Epic Games Launcher enforces **HTTPS encryption with strict SSL certificate pinning** for all manifest and chunk transfers. When DNS intercepts Epic CDN domains to point to Artesian's HTTP proxy on port 80, the official launcher refuses to connect or fails TLS handshakes, bypassing local caching entirely.
>
> **How to use Artesian with Epic Games:**
> To benefit from Artesian's multi-gigabit LAN caching and prefill for Epic Games, you **must use an unofficial, open-source launcher** configured to fetch game chunks via **plain HTTP**.
>
> Compatible unofficial launchers include:
> 1. **[Legendary](https://github.com/derrod/legendary)**: CLI game launcher for Epic Games. Can be configured to pull manifests and chunks over HTTP (`legendary install <AppName> --base-url http://...` or configuring HTTP CDN routing).
> 2. **[Heroic Games Launcher](https://heroicgameslauncher.com/)**: Modern GUI launcher for Linux, macOS, and Windows that utilizes Legendary under the hood.
> 3. **[Rare](https://github.com/Dummerle/Rare)**: GUI frontend for Legendary.
>
> Artesian prefill and update tracking operate independently via Epic's public APIs; chunk files pre-cached on Artesian's NVMe/SSD storage are served with 100% cache hits whenever an unofficial HTTP client requests them.

---

## 4. Comparison Matrix

| Feature | Steam | Xbox / MS Store | Epic Games |
|---|---|---|---|
| **Payload Format** | Content-addressable chunks (~1MB) | Monolithic `.msixvc` archives + `.phf`/`.ecd` | Content-addressable `.chunk` files |
| **Transport** | Plain HTTP | Plain HTTP (Delivery Optimization) | HTTP (Unofficial clients) / HTTPS (Official) |
| **Official Client Compatible?** | **Yes** (Out of the box) | **Yes** (Windows Delivery Optimization) | **No** (Requires unofficial HTTP client) |
| **Authentication Flow** | Steam Guard / Push / TOTP | MSA OAuth2 Device Code | OAuth2 Authorization Code |
| **Library Sync** | `IPlayerService` owned games | TitleHub + Collections + Game Pass | Launcher assets API (`Live`) |
| **Prefill Support** | Pure-Go Steam CM WebSocket | Package Service + DO Slicing | Manifest API + Fastly/CloudFront CDN |
| **Supported Platforms** | Windows, macOS, Linux | Windows, Xbox Series X\|S, Xbox One | Windows, macOS |
