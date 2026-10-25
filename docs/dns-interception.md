# DNS Interception & Upstream Forwarding

Game clients resolve CDN hostnames via standard system DNS queries. Artesian serves as your network's primary or secondary DNS resolver to enable seamless, transparent caching without configuring client-side HTTP proxies.

---

## How It Works

1. **Client DNS Resolution**:
   When a client launches a game download or update, it sends a standard DNS `A` query to Artesian on port `:53`:
   ```
   Query: level3.cs.steampowered.com (Type A, Class IN) -> Sent to Artesian (:53)
   ```

2. **Domain Pattern Matching**:
   Artesian checks whether the queried hostname matches any registered platform service patterns:
   - **Steam**: `*.cs.steampowered.com`, `*.content.steampowered.com`, `*.steamcontent.com`
   - **Xbox & Windows Store**: `*.delivery.mp.microsoft.com`, `*.dl.delivery.mp.microsoft.com`, `*.do.dsp.mp.microsoft.com`
   - **Epic Games**: `*.epicgames.com`, `*.epicgamescdn.com`, `*.akamaized.net`

3. **Interception Response**:
   If the domain matches a supported game CDN, Artesian synthesizes an immediate DNS answer pointing to its own host IP address:
   ```
   Answer: level3.cs.steampowered.com -> A 192.168.1.100 (TTL: 60s)
   ```

4. **Transparent Upstream Forwarding**:
   If a client queries a regular domain (e.g. `google.com`, `discord.com`, `github.com`), the query does not match game CDN patterns. Artesian immediately relays the query packet to configured upstream resolvers (`1.1.1.1`, `8.8.8.8`, or local gateway) and relays the answer back to the client.

5. **Client HTTP Request**:
   The game client connects to `192.168.1.100:80` with the original HTTP Host header preserved:
   ```http
   GET /depot/731/chunk/3f6... HTTP/1.1
   Host: level3.cs.steampowered.com
   User-Agent: Valve/Steam HTTP Client 1.0
   ```
   The Artesian reverse proxy receives this request, inspects the chunk cache, and either streams it from disk or retrieves it from upstream.

---

## Platform Client Considerations

### Steam (Valve)
- **Supported Out of the Box**: The official Steam desktop client downloads depot chunks over plain HTTP. DNS interception works seamlessly with zero client modifications.

### Xbox & Microsoft Store (Delivery Optimization)
- **Supported Out of the Box**: Windows Delivery Optimization (DO) connects to `*.delivery.mp.microsoft.com` endpoints via HTTP and requests monolithic package slices using HTTP `Range` headers. Artesian intercepts these requests and responds with `206 Partial Content` directly from local NVMe/SSD cache.

### Epic Games
> [!WARNING]
> **Official Epic Games Launcher Incompatibility**:
> The official Windows and macOS Epic Games Launcher enforces **HTTPS with strict SSL certificate pinning** on all download connections. If DNS intercepts Epic domains to Artesian's HTTP proxy on port 80, the official client will fail TLS negotiation.
>
> To take advantage of Artesian's DNS interception and LAN caching for Epic Games, you **must use an unofficial / open-source launcher** (such as [Legendary](https://github.com/derrod/legendary), [Heroic Games Launcher](https://heroicgameslauncher.com/), or [Rare](https://github.com/Dummerle/Rare)) configured to fetch manifests and chunks over plain HTTP.

---

## Configuring Network Clients

To route client DNS through Artesian:
- **Router / DHCP Scope (Recommended):** Set the primary DNS server in your DHCP settings to Artesian's host IP (e.g. `192.168.1.100`). All LAN devices will automatically route CDN queries through Artesian.
- **Conditional Forwarding (Pi-hole, AdGuard Home, pfSense):** Configure your existing DNS server to forward only the game CDN domain wildcards to Artesian's IP on port 53.
