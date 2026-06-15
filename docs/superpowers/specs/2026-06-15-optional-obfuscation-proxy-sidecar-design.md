# Optional Obfuscation Proxy (sidecar) — Design

**Date:** 2026-06-15
**Branch:** `feat/amneziawg-proxy`
**Status:** Implemented (Tasks 1–7). Manual E2E (Task 8) pending on a real host.

## Implementation deltas (discovered while building)

These refine, but do not change, the design above:

1. **Backend must be an IP, not a hostname.** The proxy parses `backend` as a
   Rust `SocketAddr`, so the Docker service name `amnezia-wg2-easy` is rejected.
   `proxy-entrypoint.sh` resolves `PROXY_BACKEND_HOST` to an IPv4 via
   `getent ahostsv4` at startup and writes the resolved IP into `backend`. (If
   the AWG container is later recreated with a new IP, restart the proxy.)
2. **`PROXY_QUIC_HANDSHAKE` default is `true`** (not `false`): the binary itself
   defaults it true, and `false` = stateless Version Negotiation (weaker).
3. **Runtime base pinned to `debian:bookworm-slim`** to match the
   `rust:1.75-slim` build stage's glibc. (A `cargo fetch` cache layer was tried
   and reverted — it forced a toolchain bump off the 1.75 pin.)
4. **`.env.example` uses full-line comments only.** Docker Compose does not strip
   an inline comment after an *empty* value, which would have turned an empty
   `PASSWORD=` into the literal comment text (a silent admin password).
5. **`.env` is now untracked** (added to `.gitignore`); the repo's previously
   tracked `.env` was an upstream placeholder template, replaced by
   `.env.example`.

## Summary

## Summary

Add an **optional** UDP obfuscation proxy in front of the Dockerized AmneziaWG
server, so traffic positively resembles a real QUIC / DNS / STUN / SIP service
to DPI — a second layer on top of AWG's own S1–S4 / H1–H4 randomization.

The proxy is the `amneziawg-proxy` Rust crate from
[wiresock/amneziawg-install](https://github.com/wiresock/amneziawg-install)
(`amneziawg-proxy/`), an async tokio UDP proxy that (1) answers DPI probes with
valid protocol responses and (2) rewrites each outgoing packet's S-padding
prefix with protocol-conformant bytes, leaving the encrypted WireGuard payload
untouched.

The user chooses what to run:

- **Plain mode** — AmneziaWG 2.0 only (today's behavior, unchanged).
- **Proxy mode** — AmneziaWG 2.0 + obfuscation proxy sidecar.

Selection is by **which compose file is brought up**, with a `justfile` for
convenience. This is the "Option A — sidecar" integration: purely additive
infrastructure with **zero changes to existing application code**.

### Explicitly out of scope (future "Option C")

- No Web-UI controls for the proxy (protocol toggle lives in env/compose).
- No consumption of the proxy's `sessions.json` status file. This UI only shows
  per-public-key `transferRx/Tx`/handshake, which stay correct behind the proxy;
  it never displays client endpoints, so the status file adds nothing here.

## Architecture

Two deployment modes, selected by compose file. The proxy is a separate
container; the existing image is reused as-is.

**Plain mode** (`just up` → `docker-compose.yml`): one container. AWG publishes
`${WG_PORT}/udp` and the UI `${PORT}/tcp`. Identical to current behavior.

**Proxy mode** (`just up-proxy` → `docker-compose.proxy.yml`): two containers on
the default compose network.

```
                         host :${WG_PORT}/udp  (published by proxy only)
 VPN client ──UDP──►  ┌──────────────────────────┐
 (DPI sees QUIC/DNS/  │ proxy container          │ reads wg0.conf (ro) for
  STUN/SIP)           │  listen 0.0.0.0:${WG_PORT}│  S1–S4 / H1–H4 padding
                      │  backend ───────────────►│ amnezia-wg2-easy:${WG_PORT}/udp
                      └──────────────────────────┘     (NOT host-published)
                      ┌───────────────────────────────────────────┐
 you ──browser──────► │ amnezia-wg2-easy   UI :${PORT}/tcp         │
                      │   AWG wg0 ListenPort ${WG_PORT} (unchanged)│
                      └───────────────────────────────────────────┘
```

### Why no second port and no AWG rebind

The two containers live in **separate network namespaces**, so the public port
and the AWG backend port are *both* simply `${WG_PORT}`. The proxy binds
`:${WG_PORT}` in its own namespace and forwards to `amnezia-wg2-easy:${WG_PORT}`
over the compose network (inter-container traffic does not require `ports:`
publishing). Therefore:

- No second/backend port number is introduced.
- AWG's `ListenPort` is **unchanged** (`WireGuard.js:137` still emits
  `ListenPort = ${WG_PORT}`).
- The client `Endpoint = WG_HOST:${WG_PORT}` (`WireGuard.js:277`) stays correct,
  because the proxy now answers on that port.

### Client-facing port is configurable via `WG_PORT`

`WG_PORT` is the single client-facing port knob and is **not** hardcoded:

- Proxy listens on `0.0.0.0:${WG_PORT}` (what clients dial).
- Client config advertises `WG_HOST:${WG_PORT}` (same variable → always matches).
- AWG's internal backend follows the same value invisibly (separate netns, no
  collision).

Examples: `WG_PORT=443` → proxy fronts QUIC on 443; `WG_PORT=53` → DNS mode on
53. Proxy-listen and client-Endpoint are deliberately tied to the one variable;
splitting them would let clients dial a port the config does not advertise. AWG's
internal port has no user-visible reason to be independent, so no separate knob
is added (this preserves the zero-app-change property).

## Components / new files

No edits to existing application code. `docker-compose.yml` is unchanged.

| File | Purpose |
|---|---|
| `proxy/` | **Vendored** `amneziawg-proxy` Rust crate, including its MIT `LICENSE`, pinned to a known upstream commit. |
| `proxy/entrypoint.sh` | ~15-line shell: renders `/etc/amneziawg-proxy/proxy.toml` from `PROXY_*` env, then `exec`s the proxy binary. |
| `Dockerfile.proxy` | amd64 build. Build stage (`rust:slim` or similar) compiles the vendored crate; minimal runtime stage carries the binary + entrypoint. |
| `docker-compose.proxy.yml` | Proxy-mode stack: AWG service (UDP **not** published, UI published) + proxy service (publishes `${WG_PORT}/udp`, mounts AWG config dir ro, `cap_add: NET_BIND_SERVICE`, `depends_on` AWG healthy). |
| `.env.example` | Shared config consumed by both compose files: `WG_HOST`, `PASSWORD`, `PORT`, `WG_PORT`, and the `PROXY_*` knobs. |
| `justfile` | Convenience recipes (below). |
| `README` update | "Optional obfuscation proxy" section + vendoring/attribution note for wiresock/amneziawg-install. |

### Vendoring & attribution

The `amneziawg-proxy` crate is MIT-licensed. Vendoring requirements:

- Copy the crate into `proxy/` including its `LICENSE` (MIT text retained).
- Add a note in the README: "The proxy under `proxy/` is vendored from
  wiresock/amneziawg-install (`amneziawg-proxy`), MIT-licensed, at commit
  `<sha>`," documenting provenance and the manual update path.
- A respectful credit to the original authors in the README.

Vendoring (vs submodule / build-time fetch) is chosen because the upstream repo
is not controlled by us; vendoring makes builds reproducible and independent of
upstream availability/retags.

## Proxy configuration (env-driven)

The proxy binary reads a `proxy.toml`. `entrypoint.sh` renders it from env at
container start. Minimal surface — protocol plus its paired options; proxy
defaults for everything else (session TTL, rate limit, buffers).

| Env var | Default | Notes |
|---|---|---|
| `PROXY_PROTOCOL` | `quic` | one of `quic` / `dns` / `stun` / `sip` / `auto` |
| `PROXY_DNS_FORWARD` | `false` | only meaningful for `dns` / `auto` |
| `PROXY_DNS_UPSTREAM` | `1.1.1.1:53` | upstream resolver when forwarding |
| `PROXY_QUIC_HANDSHAKE` | `false` | stateful QUIC/TLS responder |
| `PROXY_QUIC_DOMAIN` | `cloudflare.com` | SNI domain for the handshake cert |

Fixed by the entrypoint (not user-facing):

```
listen     = 0.0.0.0:${WG_PORT}
backend    = amnezia-wg2-easy:${WG_PORT}
awg_config = /etc/amnezia/amneziawg/wg0.conf
```

## justfile recipes

```
just up           # plain AWG2         → docker compose up -d
just down         # stop plain stack
just up-proxy     # AWG2 + proxy       → docker compose -f docker-compose.proxy.yml up -d --build
just down-proxy   # stop proxy stack
just logs         # plain logs
just logs-proxy   # proxy-stack logs
just ps           # status
```

Switching modes = `down` the current mode, then `up`/`up-proxy` the other. The
shared `~/.amnezia-wg-easy:/etc/amnezia/amneziawg` volume preserves client data
across modes.

## Correctness details accounted for

- **Startup ordering:** proxy
  `depends_on: { amnezia-wg2-easy: { condition: service_healthy } }`. The
  existing healthcheck (`wg show | grep interface`) guarantees `wg0.conf` (with
  `S1–S4`/`H1–H4`) exists before the proxy reads it. Padding params are generated
  once and are stable across client add/remove, so a single read at proxy start
  is correct.
- **Return path:** AWG sees the proxy container as each peer's endpoint and
  replies there; the proxy's session table maps replies back to the real client.
  This is the documented "proxy + AWG on a network" deployment.
- **H-param format:** this repo writes `H1 = min-max` (`WireGuard.js:149-152`);
  the proxy parses that range format. Compatible.
- **Stats:** per-public-key `transferRx/Tx`/handshake (`WireGuard.js:202-230`)
  remain correct behind the proxy; the UI does not display endpoints, so the
  shared-loopback endpoint behind the proxy is irrelevant here.
- **Privileged ports:** binding `443`/`53` requires the capability; the proxy
  container gets `cap_add: [NET_BIND_SERVICE]` (or the Dockerfile `setcap`s the
  binary).

## Build / platform

- **amd64 only** (per decision). `Dockerfile.proxy` targets amd64; no
  arm cross-compile.
- Proxy image is **built locally** via `build:` in `docker-compose.proxy.yml`
  (the vendored crate is in-repo). Publishing a proxy image to a registry/CI is
  a possible future addition, not part of this work.

## Follow-up (separate task, agreed)

Research the **WireSock Secure Connect 3.5+** bidirectional-imitation client
requirement: whether enabling client→server imitation needs specific `.conf`
keys (e.g. the `I1–I5` CPS signatures this repo already supports) or is purely
WireSock-internal. This affects client-side **documentation only**, not any of
the server-side work above.

## Testing

No automated test suite exists (ESLint + manual testing per project norms).
Manual verification plan:

1. `just up` — plain mode still works: client connects on `${WG_PORT}`, UI on
   `${PORT}`, traffic flows. (Regression check — confirms zero-change property.)
2. `just up-proxy` — proxy starts only after AWG is healthy; client connects on
   `${WG_PORT}` through the proxy; traffic flows; UI stats update.
3. `tcpdump`/Wireshark on the public port shows frames decoding as the imitated
   protocol (e.g. QUIC) with no WireGuard/malformed markers.
4. Set `WG_PORT=443` and `PROXY_PROTOCOL=quic`; confirm the proxy binds 443 (cap
   works) and a client with `Endpoint = WG_HOST:443` connects.
5. `just down-proxy` then `just up` — client data persists across the mode
   switch (shared volume).
