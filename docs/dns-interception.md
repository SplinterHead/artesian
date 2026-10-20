# DNS Interception & Upstream Forwarding

Steam game clients resolve CDN hostnames via standard system DNS queries. Artesian serves as your network's primary or secondary DNS resolver to enable seamless, transparent caching without configuring client-side HTTP proxies.

---

## How It Works

1. **Client DNS Resolution**:
   When a client launches a game download or update, it sends a standard DNS `A` query:
   ```
   Query: level3.cs.steampowered.com (Type A, Class IN) -> Sent to Artesian (:53)
   ```

2. **Domain Pattern Matching**:
   Artesian checks whether `level3.cs.steampowered.com` matches any registered service patterns:
   - `*.cs.steampowered.com` => **Match (Steam Service)**

3. **Interception Response**:
   Artesian synthesizes an immediate DNS answer pointing to its own host address:
   ```
   Answer: level3.cs.steampowered.com -> A 192.168.1.100 (TTL: 60s)
   ```

4. **Transparent Upstream Forwarding**:
   If a client queries a regular domain (e.g. `google.com`, `discord.com`, `github.com`), the query does not match game CDN patterns. Artesian immediately relays the query packet to configured upstream resolvers (`1.1.1.1`, `8.8.8.8`) and relays the answer back to the client.

5. **Client HTTP Request**:
   The game client connects to `192.168.1.100:80` with the original HTTP Host header preserved:
   ```http
   GET /depot/731/chunk/3f6... HTTP/1.1
   Host: level3.cs.steampowered.com
   User-Agent: Valve/Steam HTTP Client 1.0
   ```
   The Artesian reverse proxy receives this request, inspects the chunk cache, and either streams it from disk or retrieves it from upstream.

---

## Configuring Network Clients

To route client DNS through Artesian:
- **Router / DHCP Option:** Set the primary DNS server in your DHCP scope to Artesian's IP (e.g. `192.168.1.100`).
- **Pi-hole / AdGuard Home / pfSense:** Set Artesian as a conditional forwarder for game CDN domains, or place Artesian upstream/downstream in your resolver chain.
