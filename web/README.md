# Artesian Web UI (Vue 3 + Vite)

The management console and control center for Artesian.

## Features
- **Real-time Telemetry:** Cache hit ratio, bandwidth saved, upstream miss count, active connections.
- **Service Hub:** Toggle and configure game platforms (Steam, Epic Games, Blizzard, Riot, Xbox) and view intercepted domain patterns.
- **Cache Prefill & Warm-up:** Trigger fresh initialisations, monitor chunk download pipelines, and see per-game sync status.
- **Scheduler & Cron Timers:** Manage background update checks and retention windows.

## Development

```bash
cd web
npm install
npm run dev
```

The Vite dev server proxies API requests directly to the Artesian Go backend on `:8080`.

## Production Build

```bash
npm run build
```

This compiles static assets into `web/dist`, which can be served directly by the Artesian Go binary.
