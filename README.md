# Artesian

[![Built with Gemini](https://img.shields.io/badge/Built%20with-Gemini-8E75C2?style=for-the-badge&logo=googlegemini&logoColor=white)](https://deepmind.google/technologies/gemini/)

**Artesian** is a unified, modern, high-performance local network game content cache, DNS interceptor, and management engine written in **Go + VueJS**.

Designed to replace complex, fragmented, and multi-container legacy network caching environments, Artesian consolidates DNS interception, HTTP reverse proxying, intelligent chunk caching, automated update polling, and proactive prefill orchestration into a single, cohesive binary with a sleek modern web interface.

---

## Key Features

- **Embedded DNS Interceptor & Upstream Forwarder:**
  Intercepts DNS queries for Steam game distribution CDNs and rewrites them to the local proxy IP. Transparently forwards all regular DNS queries to upstream resolvers (Cloudflare, Google, or local gateways) with zero configuration overhead.
- **High-Throughput HTTP Chunk Cache:**
  Content-addressable local storage layer optimized for NVMe and SSD drives. Seamlessly streams cached game chunks to LAN clients at multi-gigabit wire speed.
- **Authenticated Account Integration (Steam):**
  Direct connection to the Steam Connection Manager (CM) and Steam Web API (`IPlayerService`). Automatically discovers your genuine owned game library with Steam Guard 2FA support and persistent session storage.
- **Dynamic Game Library with Searchable Text Filter:**
  Interactive Game Add modal that transforms into a real-time text filter input when opened, allowing instant lookup by title or App ID across hundreds of owned games, complete with arrow-key navigation.
- **Automated Depot Caching & Prefill Pipeline:**
  Automatically triggers depot caching the moment a game is added to your library, delta-diffing missing chunks against local storage.
- **Live Active Downloads Telemetry:**
  Monitors ongoing client traffic and internal prefill operations in real time. Internal cache warming displays with a distinct **Artesian** client badge, clickable SteamDB depot links, transfer rates, percentage badges, and animated progress bars.
- **Automated Cron Timers & Realtime Update Checks:**
  Built-in scheduler to periodically check for new game patches and automatically warm the local cache before users even initiate downloads.
- **Unified Vue.js Control Center:**
  Single-page web dashboard providing live cache hit ratios, bandwidth savings, service switches, cron management, and prefill progress tracking.
- **Lightweight & Single-Binary Architecture:**
  No fragile webs of separate DNS containers, Nginx reverse proxies, or external glue scripts.

---

## Architecture Overview

```
                      +-----------------------------+
                      |         LAN Clients         |
                      +--------------+--------------+
                                     |
              +----------------------+----------------------+
              | DNS Queries (:53)                           | HTTP Requests (:80)
              v                                             v
     +-----------------+                           +-----------------+
     |  DNS Forwarder  |                           |   HTTP Proxy    |
     |  & Interceptor  |                           |  & Chunk Cache  |
     +--------+--------+                           +--------+--------+
              |                                             |
   +----------+----------+                       +----------+----------+
   |                     |                       |                     |
Standard DNS         Game CDN                Cache HIT             Cache MISS
   |                     |                       |                     |
   v                     v                       v                     v
Upstream DNS         Redirect to           Direct NVMe Stream      Pass-Through &
(e.g. 1.1.1.1)       Artesian Proxy        to Client               Async Disk Cache
```

For a comprehensive explanation of components and request lifecycles, see the [Architecture Documentation](docs/architecture.md).

---

## Directory Structure

```
artesian/
├── cmd/
│   └── artesian/              # Unified production daemon entrypoint
│       └── main.go
├── internal/
│   ├── api/                   # REST & WebSocket API endpoints for web UI
│   ├── cache/                 # Content-addressable chunk storage & eviction
│   ├── config/                # Runtime configuration models & YAML parser
│   ├── db/                    # SQLite state store (watched games, chunks, metrics)
│   ├── dns/                   # UDP DNS server, pattern matcher, & forwarder
│   ├── prefill/               # Content prefill & cache warming orchestrator
│   ├── proxy/                 # HTTP reverse proxy, pass-through, & cache engine
│   ├── scheduler/             # Cron timers & automated update checker
│   └── services/              # Game platform modules (Steam)
├── configs/
│   └── artesian.example.yaml  # Default configuration template
├── docs/
│   ├── architecture.md        # Deep dive into system components
│   ├── dns-interception.md    # DNS interception and forwarding guide
│   └── services-guide.md      # Game platform CDN specifications
├── web/                       # Vue 3 + Vite management frontend
│   ├── src/
│   │   ├── App.vue            # Dashboard, services, prefill, & cron views
│   │   └── main.js
│   ├── embed.go               # Go embed.FS for compiling Vue dist into binary
│   ├── index.html
│   └── package.json
├── Dockerfile                 # Multi-stage builder (Node + Go -> Minimal Alpine)
├── docker-compose.yml         # Single-container appliance orchestration
├── go.mod
└── README.md
```

---

## Single-Container Appliance Deployment (Recommended)

Artesian is built as a single, fully-integrated container appliance with multi-stage builders:
1. **Frontend Builder (`node:22-alpine`):** Compiles the Vue 3 control panel via Vite.
2. **Backend Builder (`golang:1.24-alpine`):** Ingests the compiled web dist and compiles the single Go daemon using `embed.FS`.
3. **Minimal Runtime Image (`alpine:3.21`):** A lightweight (~8MB) container housing only the binary, certificates, and default configuration.

### Run with Docker Compose

```bash
docker compose up -d
```

### Run with Docker CLI

```bash
# Build the container image
docker build -t artesian:latest .

# Run container with host networking (recommended for DNS & game client visibility)
docker run -d \
  --name artesian \
  --restart unless-stopped \
  --network host \
  -v $(pwd)/data/cache:/data/cache \
  -v $(pwd)/data/db:/data \
  -v $(pwd)/data/manifests:/data/manifests \
  artesian:latest
```

The container exposes:
- **Port 53 (UDP/TCP):** Transparent DNS forwarder & game CDN interceptor
- **Port 80 (TCP):** HTTP chunk caching reverse proxy
- **Port 8080 (TCP):** Vue.js Web UI & REST Control API

---

## Local Development Quickstart

### 1. Running the Full Production Daemon Locally (`cmd/artesian`)

```bash
# Build the unified daemon (embeds web/dist directly)
go build -o artesian ./cmd/artesian

# Run with SQLite state database
sudo ./artesian -db ./data/db/artesian.db
```

The daemon will simultaneously bind:
- **DNS Server:** UDP port `:53` (intercepts game CDNs, forwards other queries upstream)
- **Proxy Engine:** HTTP port `:80` (serves cached chunks or proxies upstream)
- **Web UI & Control API:** HTTP port `:8080` (accessible via browser at `http://localhost:8080`)

### 2. Running the Vue.js Frontend (Vite Dev Server)

```bash
cd web
npm install
npm run dev
```

Visit `http://localhost:3000` to preview changes against the Go backend.
