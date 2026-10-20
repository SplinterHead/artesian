# Artesian Web UI (Vue 3 + Vite)

The management console and control center for Artesian.

## Features
- **Real-time Telemetry & Active Downloads:** Cache hit ratio, bandwidth saved, active connection rates, and live downloads table with custom client badges (e.g., `Artesian`), mini progress bars, and clickable SteamDB depot links.
- **Game Library & Interactive Filter Modal:** Manage watched titles. The Add Game modal turns into a live search filter input upon opening, offering keyboard navigation across hundreds of authentic owned games.
- **Accounts & Modules Hub:** Authenticate with Steam via Connection Manager WebSockets and 2FA, configure Steam auto-updates, and view intercepted CDN domain patterns.
- **Automatic Depot Prefill:** Automatically warm the local tank storage with depot chunks upon adding games to your library.
- **Scheduler & Automation:** Automated Steam PICS polling and customizable cron update schedules.

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
