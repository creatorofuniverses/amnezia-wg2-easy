# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AmneziaWG Easy — a web UI for managing an AmneziaWG (obfuscated WireGuard) VPN server on Linux. Fork of the archived wg-easy, updated with AmneziaWG 2.0 support (S1-S4 padding, H1-H4 header ranges, I1-I5 CPS signatures for DPI evasion).

## Build & Development Commands

All development commands run from `src/`:

```bash
cd src && npm run serve              # Dev server with nodemon (DEBUG=Server,WireGuard)
cd src && npm run serve-with-password # Dev server with PASSWORD=wg
cd src && npm run lint               # ESLint
cd src && npm run buildcss           # Compile Tailwind CSS (src/www/src/css/app.css → src/www/css/app.css)
```

Docker build & run from root:

```bash
npm run build                    # docker build --tag amnezia-wg-easy .
npm run start                    # docker run with required caps (NET_ADMIN, SYS_MODULE)
docker compose up --detach       # or use docker-compose.yml (set WG_HOST and PASSWORD first)
```

There is no test suite. Quality relies on ESLint and manual testing.

## Architecture

**Backend** (`src/`): Node.js 18+ using the **H3** HTTP framework (not Express — H3 uses `defineEventHandler`, `readBody`, `getQuery`, etc.).

- `server.js` — Entry point; starts server, handles SIGINT for graceful shutdown
- `lib/Server.js` — H3 router with all API routes, session auth via express-session, static file serving
- `lib/WireGuard.js` — Core VPN logic: client CRUD, config generation, `wg` CLI interaction, stats polling
- `lib/Util.js` — Shell exec helper (`Util.exec()`), IP validation
- `services/` — Singleton exports of Server and WireGuard instances

**Frontend** (`src/www/`): Vue.js 2 SPA with vendored dependencies (no build step except Tailwind CSS).

- `index.html` — Full SPA template with Vue directives inline
- `js/app.js` — Vue instance with all application state and methods
- `js/api.js` — Fetch-based API client class
- `js/i18n.js` — Translations (en, ua, ru, tr, no, pl, fr, de, ca, es)
- `js/vendor/` — Minified third-party libs (Vue, ApexCharts, timeago, sha256)

**Config storage**: WireGuard state lives in `/etc/wireguard/wg0.json` (structured data) synced to `/etc/wireguard/wg0.conf` (WireGuard format).

## Key Patterns

- **H3 framework idioms**: Use `defineEventHandler`, `readBody`, `getQuery`, `sendError` — not Express `req/res` patterns
- **Singleton services**: `services/Server.js` and `services/WireGuard.js` export single instances used everywhere
- **Shell execution**: All system commands go through `Util.exec()` which wraps child_process
- **Session auth**: Password is optional (env `PASSWORD`); when set, routes check `req.session.authenticated`
- **Security guards**: Prototype pollution prevention on route params; `safePathJoin` for static files
- **ESLint config**: Extends `athom`; `no-shadow`, `consistent-return`, `max-len` are disabled

## Environment Variables

Key config (all optional except `WG_HOST` for production):

| Variable | Purpose | Default |
|----------|---------|---------|
| `WG_HOST` | Public IP/hostname | (required) |
| `WG_PORT` | UDP port | 51820 |
| `PORT` | Web UI port | 51821 |
| `PASSWORD` | Admin password | (none) |
| `WG_DEFAULT_DNS` | Client DNS | 1.1.1.1 |
| `WG_DEFAULT_ADDRESS` | Client subnet | 10.8.0.x |
| `JC, JMIN, JMAX, S1, S2` | AmneziaWG junk/padding params | Random |
| `S3, S4` | AWG 2.0 padding (cookie reply, data packet) | Random |
| `H1-H4` | Header magic ranges (format: `min-max` or single value) | Random |
| `I1-I5` | CPS signatures (client-only, AWG 2.0) | (none) |
| `UI_TRAFFIC_STATS` | Enable RX/TX stats | false |
| `UI_CHART_TYPE` | Chart type (0-3) | 0 |

## Deployment

Docker image: `ghcr.io/spcfox/amnezia-wg-easy` (multi-arch: amd64, arm/v6, arm/v7, arm64/v8). Base image `amneziavpn/amnezia-wg:latest` provides AWG tools (amneziawg-go userspace + awg/awg-quick symlinked as wg/wg-quick). Production deploys from `production` branch via GitHub Actions. Requires `NET_ADMIN` and `SYS_MODULE` capabilities plus TUN device access.
