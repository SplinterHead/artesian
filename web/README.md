# Artesian Web UI (Vue 3 + Vite)

The management console and control center for Artesian.

## Features

- **Real-time Telemetry & Active Downloads:** Cache hit ratio, bandwidth saved, active connection rates, and live downloads table with custom client badges (e.g., `Artesian`), mini progress bars, and clickable SteamDB depot links.
- **Game Library & Store Selection:** Manage watched titles across Steam, Xbox, and Epic Games. The Add Game modal provides store tabs, genuine owned game dropdowns, platform locking, and an on-demand Game Pass catalog toggle.
- **Accounts & Modules Hub:** Multi-store authentication hub supporting Steam (Connection Manager WebSockets & 2FA), Xbox / Microsoft Store (MSA OAuth2 Device Code), and Epic Games (OAuth2 launcher code emulation), along with auto-update toggles and domain inspection.
- **Automatic Depot Prefill:** Automatically warm the local tank storage with chunks and package slices upon adding games to your library.
- **Scheduler & Automation:** Automated platform update polling (Steam PICS, Xbox Package Service, Epic Asset manifests) and customizable cron update schedules.

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
