# Artesian

[![Built with Gemini](https://img.shields.io/badge/Built%20with-Gemini-8E75C2?style=for-the-badge&logo=googlegemini&logoColor=white)](https://deepmind.google/technologies/gemini/)

**Artesian** is a unified, modern, high-performance local network game content cache, DNS interceptor, and management engine written in **Go + VueJS**.

Designed to replace complex, fragmented, and multi-container legacy network caching environments, Artesian consolidates DNS interception, HTTP reverse proxying, intelligent chunk caching, automated update polling, and proactive prefill orchestration into a single, cohesive binary with a sleek modern web interface.

---

## Key Features

- **Multi-Platform Game Caching Engine:**
  Unified caching support for **Steam**, **Xbox & Microsoft Store**, and **Epic Games** in a single appliance.
- **Embedded DNS Interceptor & Upstream Forwarder:**
  Intercepts DNS queries for game distribution CDNs (Steam, Xbox Delivery Optimization, Epic CDN edges) and rewrites them to the local proxy IP. Transparently forwards standard DNS queries to upstream resolvers (Cloudflare, Google, or local gateways) with zero configuration overhead.
- **High-Throughput HTTP Chunk Cache & Monolithic Slicing:**
  - **Steam & Epic:** Content-addressable chunk storage layer optimized for NVMe and SSD drives. Seamlessly streams cached game chunks to LAN clients at multi-gigabit wire speed.
  - **Xbox / Microsoft Store:** Byte-range alignment and slicing engine that chunks monolithic packages (`.msixvc`, `.xvc`) into uniform 1MB blocks (`@range=0-1048575`), serving `206 Partial Content` directly from local NVMe/SSD storage.
- **Authenticated Account Integrations:**
  - **Steam:** Direct connection to the Steam Connection Manager (CM) and Steam Web API (`IPlayerService`). Supports Steam Guard 2FA, mobile push notification approval polling, and genuine owned game discovery.
  - **Xbox & Microsoft Store:** Frictionless MSA OAuth2 Device Code flow (`microsoft.com/devicelogin`), dual-token exchange (XAU/XSTS), TitleHub personal library enumeration, and an on-demand Game Pass catalog toggle.
  - **Epic Games:** OAuth2 launcher client emulation with persistent token refresh, asset catalog discovery, and support for both binary zlib manifests (`0x44BEC00C`) and decimal-blob JSON manifests.
- **Automated Depot Caching & Prefill Pipeline:**
  Automatically triggers depot caching the moment a game is added to your library, delta-diffing missing chunks against local storage.
- **Live Active Downloads Telemetry:**
  Monitors ongoing client traffic and internal prefill operations in real time. Internal cache warming displays with a distinct **Artesian** client badge, clickable SteamDB depot links, transfer rates, percentage badges, and animated progress bars.
- **Automated Cron Timers & Realtime Update Checks:**
  Built-in scheduler to periodically check for new game patches across all connected platforms and automatically warm the local cache before users initiate downloads.
- **Unified Vue.js Control Center:**
  Single-page web dashboard providing live cache hit ratios, bandwidth savings, service switches, cron management, and prefill progress tracking.

> [!WARNING]
> **Epic Games Client Requirement**: The official Epic Games Launcher strictly enforces HTTPS with SSL certificate pinning for all downloads and will not route through local HTTP proxy caches. Epic Games LAN caching is designed for and requires **unofficial / third-party launchers** (such as [Legendary](https://github.com/derrod/legendary), [Heroic Games Launcher](https://heroicgameslauncher.com/), or [Rare](https://github.com/Dummerle/Rare)) configured to download manifests and chunks over plain HTTP.

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

For a comprehensive explanation of components and request lifecycles, see the [Architecture Documentation](docs/architecture.md) and [Game Services Guide](docs/services-guide.md).

---

## Directory Structure

```
artesian/
├── cmd/
│   └── artesian/              # Unified production daemon entrypoint
│       └── main.go
├── internal/
│   ├── adapters/              # Hexagonal platform adapters
│   │   ├── epic/              # Epic Games OAuth2, manifests, and CDN chunk resolver
│   │   ├── steam/             # Steam CM WebSockets, Steam Guard, and depot resolver
│   │   └── xbox/              # Xbox MSA Device Code, TitleHub, DO range slicer
│   ├── api/                   # REST & WebSocket API endpoints for web UI
│   ├── cache/                 # Content-addressable chunk storage & eviction
│   ├── config/                # Runtime configuration models
│   ├── db/                    # SQLite state store (watched games, chunks, metrics)
│   ├── dns/                   # UDP DNS server, pattern matcher, & forwarder
│   ├── manifest/              # Manifest pool parser and archive extractor
│   ├── ports/                 # Hexagonal architecture port interfaces
│   ├── prefill/               # Content prefill & cache warming orchestrator
│   ├── proxy/                 # HTTP reverse proxy, pass-through, & cache engine
│   ├── scheduler/             # Cron timers & automated update checker
│   ├── services/              # Platform adapter registry & multi-store router
│   └── ws/                    # Real-time WebSocket hub
├── docs/
│   ├── architecture.md        # Deep dive into system components
│   ├── dns-interception.md    # DNS interception and forwarding guide
│   └── services-guide.md      # Game platform CDN specifications & client guide
├── web/                       # Vue 3 + Vite management frontend
│   ├── src/
│   │   ├── components/        # Modals (AddGameModal, PlatformLoginModal, etc.)
│   │   ├── views/             # Dashboard, Accounts, Library, Updates, Settings
│   │   ├── App.vue            # Shell & navigation layout
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
