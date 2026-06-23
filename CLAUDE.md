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
npm run start                    # docker run with required caps (NET_ADMIN)
docker compose up --detach       # or use docker-compose.yml (set WG_HOST and PASSWORD first)
```

The Node app has focused `node:test` unit suites in `src/lib/__tests__/` (run `cd src && node --test`); the shell/`wg-quick` integration side is verified manually. The Go probe-responder (`responder/`) **does** have unit tests; build & test it directly:

```bash
cd responder && go build ./... && go test ./...   # Go 1.25; deps: quic-go, go-nfqueue/v2
```

## Architecture

**Backend** (`src/`): Node.js 18+ using the **H3** HTTP framework (not Express — H3 uses `defineEventHandler`, `readBody`, `getQuery`, etc.).

- `server.js` — Entry point; starts server, handles SIGINT for graceful shutdown
- `lib/Server.js` — H3 router with all API routes, session auth via express-session, static file serving
- `lib/WireGuard.js` — Core VPN logic: client CRUD, config generation, `wg` CLI interaction, stats polling; supports per-client custom AllowedIPs + optional siteMasquerade toggle for relay/site-to-site topologies
- `lib/Util.js` — Shell exec helper (`Util.exec()`), IP validation
- `services/` — Singleton exports of Server and WireGuard instances

**Frontend** (`src/www/`): Vue.js 2 SPA with vendored dependencies (no build step except Tailwind CSS).

- `index.html` — Full SPA template with Vue directives inline
- `js/app.js` — Vue instance with all application state and methods
- `js/api.js` — Fetch-based API client class
- `js/i18n.js` — Translations (en, ua, ru, tr, no, pl, fr, de, ca, es)
- `js/vendor/` — Minified third-party libs (Vue, ApexCharts, timeago, sha256)

**Probe-responder** (`responder/`): a Go module (`package main`, Go 1.25) that runs as an **NFQUEUE ingress filter** on `WG_PORT`, answering active DPI probes with protocol-valid replies so the port doesn't look like a silent WireGuard endpoint. Off the data fast path; enabled by `RESPONDER=true`. Key files: `config.go`/`awg.go` (wg0.conf S/H parse + `classifyAwgPacket`), `detect.go` (QUIC/DNS/STUN discriminators), `dns.go`/`stun.go`/`quicvn.go` (single-shot reply builders), `quiccert.go`/`quicconn.go`/`quichs.go` (embedded `quic-go` TLS-1.3 handshake endpoint over a custom `net.PacketConn`), `egress.go`/`packet.go` (raw-socket reply injection + L3 parse), `responder.go`/`nfqueue.go`/`main.go` (decision logic + NFQUEUE loop + entry). Verdict order is correctness-critical: `classifyAwgPacket` runs **before** probe detection. The QUIC handshake uses a **conntrack-mark flow-claim** (set via the nfqueue verdict's NFQA_CT facility — `SetVerdictWithOption(..., WithConnMark())`, **not** the deprecated `SetVerdictWithConnMark`) so the multi-RTT handshake stays queued; claimed packets are **ACCEPTed** (not DROPped), since a DROP destroys the unconfirmed conntrack entry and its mark.

**Config storage**: WireGuard state lives in `/etc/amnezia/amneziawg/wg0.json` (structured data) synced to `/etc/amnezia/amneziawg/wg0.conf` (WireGuard format). The responder reads `wg0.conf` once at startup for S/H params.

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
| `I1_COMPAT` | Seed a baked-in legacy DNS-shaped `I1` when no explicit `I1` is set (older-client compat; explicit `I1` wins) | false |
| `IMITATE_PROTOCOL` | Shape obfuscation to a protocol (`none\|quic\|dns\|stun\|sip`); server + client configs | none |
| `RESPONDER` | Enable the Go active-probe responder (needs `IMITATE_PROTOCOL != none`, `NET_RAW`) | false |
| `QUIC_HANDSHAKE` | QUIC responder: full TLS-1.3 handshake (true) vs Version-Negotiation only (false) | true |
| `QUIC_CERT_DOMAIN` | SNI/cert domain for the QUIC handshake's self-signed cert | cloudflare.com |
| `UI_TRAFFIC_STATS` | Enable RX/TX stats | false |
| `UI_CHART_TYPE` | Chart type (0-3) | 0 |

## Deployment

Docker image: `ghcr.io/creatorofuniverses/amnezia-wg-easy` (multi-arch: amd64, arm/v6, arm/v7, arm64/v8). Multi-stage `Dockerfile`: builds `amneziawg-go` (userspace fallback) and `amneziawg-tools` (awg/awg-quick) from source, plus the Go responder (`golang:1.25-alpine` stage), onto an `alpine:3.20` runtime; uses the host kernel module (DKMS) when present. `docker-entrypoint.sh` supervises the Node UI (primary, `exec`'d) and the optional responder (crash-isolated side process under `dumb-init`), rendering/tearing down the NFQUEUE rules. Config path: `/etc/amnezia/amneziawg/`. Production deploys from `production` branch via GitHub Actions. Requires `NET_ADMIN` and `/dev/net/tun`; the responder additionally needs `NET_RAW` (raw-socket reply injection). `SYS_MODULE` no longer needed — kernel module is host-installed via DKMS.
