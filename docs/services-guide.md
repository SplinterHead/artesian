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
Because Steam chunks are immutable and addressed by content hash, they can be cached with long retention.

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

## 3. Adding New Services

To register a new game platform in Artesian:
1. Implement the `services.Service` interface in `internal/services/<platform>.go`:
   - `Name() string`
   - `MatchesHost(host string) bool`
   - `Domains() []string`
   - `IsCacheable(req *http.Request) bool`
2. Register the service instance in `internal/services/registry.go` or `cmd/artesian/main.go`.
3. Add configuration toggles to `configs/artesian.yaml`.
