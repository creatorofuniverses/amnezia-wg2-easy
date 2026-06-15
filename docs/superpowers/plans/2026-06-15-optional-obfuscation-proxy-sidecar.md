# Optional Obfuscation Proxy (sidecar) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional UDP obfuscation proxy (vendored `amneziawg-proxy` Rust crate) as a sidecar container in front of the Dockerized AmneziaWG server, selectable via a second compose file and a `justfile`, with zero changes to existing application code.

**Architecture:** Two compose files. `docker-compose.yml` = plain AWG2 (today). `docker-compose.proxy.yml` = AWG2 (UDP not published) + a proxy container that publishes `${WG_PORT}/udp`, forwards to `amnezia-wg2-easy:${WG_PORT}` over the compose network, and reads the AWG config read-only for S1–S4/H1–H4 padding. Both files read shared config from `.env`. A small entrypoint renders `proxy.toml` from `PROXY_*` env vars.

**Tech Stack:** Docker / Docker Compose, a vendored Rust crate (built amd64-only via `Dockerfile.proxy`), POSIX `sh` entrypoint, `just` task runner.

**Spec:** `docs/superpowers/specs/2026-06-15-optional-obfuscation-proxy-sidecar-design.md`

**Source provenance:** the crate is vendored from `wiresock/amneziawg-install` at commit `549bba8ae7548de1cf0264e33e0110462ec18a99` (path `amneziawg-proxy/`), MIT-licensed. The local clone is at `/home/kowalski/projects/vpn/amneziawg-install`.

**No test suite:** this repo has no automated tests (ESLint + manual). Verification steps use `docker build`, `docker compose config`, `sh -n`, and `--version`/log checks instead of unit tests.

---

### Task 1: Vendor the proxy crate

**Files:**
- Create: `proxy/` (copy of `amneziawg-proxy/` crate from the install repo)
- Create: `proxy/LICENSE` (MIT text from the install repo root `LICENSE`)

- [ ] **Step 1: Copy the crate, excluding build artifacts**

The upstream `.gitignore` excludes `/target/`; copy only tracked-style content. Run from the repo root (`/home/kowalski/projects/vpn/amnezia-wg2-easy`):

```bash
SRC=/home/kowalski/projects/vpn/amneziawg-install/amneziawg-proxy
rsync -a --exclude='/target/' --exclude='.idea/' --exclude='.vscode/' "$SRC"/ proxy/
```

- [ ] **Step 2: Add the MIT LICENSE to the vendored crate**

The crate dir has no `LICENSE` of its own; the MIT license lives at the install repo root.

```bash
cp /home/kowalski/projects/vpn/amneziawg-install/LICENSE proxy/LICENSE
```

- [ ] **Step 3: Verify the expected files are present**

Run:
```bash
ls proxy/Cargo.toml proxy/Cargo.lock proxy/LICENSE proxy/src/main.rs proxy/src/config.rs && test ! -d proxy/target && echo OK
```
Expected: lists the four files and prints `OK` (no vendored `target/`).

- [ ] **Step 4: Commit**

```bash
git add proxy/
git commit -m "feat(proxy): vendor amneziawg-proxy crate (wiresock @ 549bba8, MIT)"
```

---

### Task 2: Proxy entrypoint script

Renders `/etc/amneziawg-proxy/proxy.toml` from `PROXY_*` env vars, then execs the binary. Lives at repo root (keeps the vendored `proxy/` a pristine upstream copy).

**Files:**
- Create: `proxy-entrypoint.sh`

- [ ] **Step 1: Write the entrypoint**

```sh
#!/bin/sh
# Renders proxy.toml from environment, then runs amneziawg-proxy.
# Booleans (PROXY_DNS_FORWARD, PROXY_QUIC_HANDSHAKE) MUST be literal
# `true`/`false` (unquoted TOML booleans).
set -eu

: "${WG_PORT:=51820}"
: "${PROXY_PROTOCOL:=quic}"
: "${PROXY_BACKEND_HOST:=amnezia-wg2-easy}"
: "${PROXY_DNS_FORWARD:=false}"
: "${PROXY_DNS_UPSTREAM:=1.1.1.1:53}"
: "${PROXY_QUIC_HANDSHAKE:=true}"
: "${PROXY_QUIC_DOMAIN:=cloudflare.com}"
: "${AWG_CONFIG:=/etc/amnezia/amneziawg/wg0.conf}"

CONFIG_DIR=/etc/amneziawg-proxy
CONFIG_FILE="${CONFIG_DIR}/proxy.toml"
mkdir -p "${CONFIG_DIR}" /var/lib/amneziawg-proxy

cat > "${CONFIG_FILE}" <<EOF
listen = "0.0.0.0:${WG_PORT}"
backend = "${PROXY_BACKEND_HOST}:${WG_PORT}"
imitate_protocol = "${PROXY_PROTOCOL}"
awg_config = "${AWG_CONFIG}"
dns_forward_enabled = ${PROXY_DNS_FORWARD}
dns_upstream = "${PROXY_DNS_UPSTREAM}"
quic_handshake_enabled = ${PROXY_QUIC_HANDSHAKE}
quic_certificate_domain = "${PROXY_QUIC_DOMAIN}"
EOF

echo "amneziawg-proxy: rendered ${CONFIG_FILE}:"
cat "${CONFIG_FILE}"

exec amneziawg-proxy "${CONFIG_FILE}"
```

- [ ] **Step 2: Make it executable and syntax-check it**

Run:
```bash
chmod +x proxy-entrypoint.sh && sh -n proxy-entrypoint.sh && echo "syntax OK"
```
Expected: prints `syntax OK` (no syntax errors).

- [ ] **Step 3: Commit**

```bash
git add proxy-entrypoint.sh
git commit -m "feat(proxy): env-driven entrypoint that renders proxy.toml"
```

---

### Task 3: Dockerfile for the proxy image (amd64)

**Files:**
- Create: `Dockerfile.proxy`

- [ ] **Step 1: Write the Dockerfile**

Two stages: build the vendored crate, then a slim glibc runtime carrying the binary + entrypoint. `build-essential`/`perl` are needed because `ring` (via rustls/rcgen) compiles C/asm.

```dockerfile
# Build stage — compiles the vendored amneziawg-proxy crate (amd64).
FROM rust:1.75-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential perl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY proxy/ .
RUN cargo build --release --locked

# Runtime stage — minimal glibc image with the binary and entrypoint.
FROM debian:stable-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /build/target/release/amneziawg-proxy /usr/local/bin/amneziawg-proxy
COPY proxy-entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh \
    && mkdir -p /var/lib/amneziawg-proxy
WORKDIR /var/lib/amneziawg-proxy
ENV RUST_LOG=amneziawg_proxy=info
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
```

- [ ] **Step 2: Build the image (this also compiles the vendored crate)**

Run:
```bash
docker build -f Dockerfile.proxy -t amnezia-wg-proxy:test .
```
Expected: build completes; the `cargo build --release --locked` step succeeds (proves the vendored crate compiles and `Cargo.lock` is consistent). First build is slow (full Rust compile).

- [ ] **Step 3: Verify the binary runs**

The entrypoint execs the proxy with a rendered config, so override it to check `--version` directly:

```bash
docker run --rm --entrypoint amneziawg-proxy amnezia-wg-proxy:test --version
```
Expected: prints `amneziawg-proxy 0.1.7` (or the vendored crate's version).

- [ ] **Step 4: Verify config rendering produces valid TOML the binary accepts**

Render with a privileged port + dns mode, then have the binary load-and-validate it (it will fail to *bind*, but config validation runs first and logs "configuration loaded" before any bind error):

```bash
docker run --rm -e WG_PORT=443 -e PROXY_PROTOCOL=dns -e PROXY_DNS_FORWARD=true \
  amnezia-wg-proxy:test 2>&1 | head -20
```
Expected: output shows the rendered `proxy.toml` (with `listen = "0.0.0.0:443"`, `imitate_protocol = "dns"`, `dns_forward_enabled = true`) followed by a `configuration loaded` log line — i.e. validation passed. (A later bind/connection error is fine; we only care that the config validated.)

- [ ] **Step 5: Commit**

```bash
git add Dockerfile.proxy
git commit -m "feat(proxy): amd64 Dockerfile building the vendored proxy"
```

---

### Task 4: Shared `.env` and base compose interpolation

Resolve the spec's shared-config contract: both compose files read from `.env`. Convert the base file's inline env literals to `${VAR}` interpolation (config only — no app code changes).

**Files:**
- Create: `.env.example`
- Modify: `docker-compose.yml` (env block → `${VAR}` interpolation)
- Modify: `.gitignore` (ignore real `.env`)

- [ ] **Step 1: Write `.env.example`**

```dotenv
# Copy to .env and fill in. Consumed by docker-compose.yml and
# docker-compose.proxy.yml.

# ── Core (required) ───────────────────────────────────────────────
WG_HOST=                 # ⚠️ your server's public IP or hostname
PASSWORD=                # Web UI admin password
LANGUAGE=en
PORT=51821               # Web UI port (tcp)
WG_PORT=51820            # Client-facing VPN port (udp). In proxy mode the
                         # proxy listens here; clients dial WG_HOST:WG_PORT.

# ── Obfuscation proxy (only used by docker-compose.proxy.yml) ─────
PROXY_PROTOCOL=quic          # quic | dns | stun | sip | auto
PROXY_QUIC_HANDSHAKE=true    # stateful QUIC/TLS responder (true = stronger)
PROXY_QUIC_DOMAIN=cloudflare.com
PROXY_DNS_FORWARD=false      # only valid with PROXY_PROTOCOL=dns or auto
PROXY_DNS_UPSTREAM=1.1.1.1:53
```

- [ ] **Step 2: Convert `docker-compose.yml` env block to interpolation**

Replace the four inline values in the `environment:` block with `${VAR}` so the base file reads from `.env`. Change these lines:

```yaml
    environment:
      - LANGUAGE=en
      - WG_HOST= # ⚠️ Required: your server's public IP or hostname
      - PASSWORD= # Admin password for the Web UI
      - PORT=51821
      - WG_PORT=51820
```

to:

```yaml
    environment:
      - LANGUAGE=${LANGUAGE:-en}
      - WG_HOST=${WG_HOST}
      - PASSWORD=${PASSWORD}
      - PORT=${PORT:-51821}
      - WG_PORT=${WG_PORT:-51820}
```

Leave the rest of `docker-compose.yml` (ports, volumes, caps, devices, the commented optional vars) unchanged. The `ports:` block stays as-is (publishes both `51820/udp` and `51821/tcp`) — this is plain mode.

- [ ] **Step 3: Ignore the real `.env`**

Append to `.gitignore` (create the file if absent):

```gitignore
# Local environment
.env
```

- [ ] **Step 4: Verify the base compose still resolves**

Create a throwaway `.env` and render the config:
```bash
cp .env.example .env && sed -i 's/^WG_HOST=/WG_HOST=example.com/' .env
docker compose -f docker-compose.yml config >/dev/null && echo "base compose OK"
```
Expected: prints `base compose OK` with no interpolation warnings about `WG_HOST`/`PORT`/`WG_PORT`.

- [ ] **Step 5: Commit**

```bash
git add .env.example .gitignore docker-compose.yml
git commit -m "feat(proxy): shared .env; base compose reads from it"
```

---

### Task 5: Proxy-mode compose file

**Files:**
- Create: `docker-compose.proxy.yml`

- [ ] **Step 1: Write the proxy-mode stack**

AWG service is the same image but publishes only the UI port (no UDP). The proxy service publishes `${WG_PORT}/udp`, depends on AWG being healthy, and mounts the AWG config read-only.

```yaml
services:
  amnezia-wg2-easy:
    image: ghcr.io/creatorofuniverses/amnezia-wg-easy:devel
    build: .
    container_name: amnezia-wg2-easy
    environment:
      - LANGUAGE=${LANGUAGE:-en}
      - WG_HOST=${WG_HOST}
      - PASSWORD=${PASSWORD}
      - PORT=${PORT:-51821}
      - WG_PORT=${WG_PORT:-51820}
    volumes:
      - ~/.amnezia-wg-easy:/etc/amnezia/amneziawg
    ports:
      # UDP is NOT published here — the proxy fronts it.
      - "${PORT:-51821}:${PORT:-51821}/tcp"
    restart: unless-stopped
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
    devices:
      - /dev/net/tun:/dev/net/tun

  amnezia-wg-proxy:
    build:
      context: .
      dockerfile: Dockerfile.proxy
    container_name: amnezia-wg-proxy
    environment:
      - WG_PORT=${WG_PORT:-51820}
      - PROXY_BACKEND_HOST=amnezia-wg2-easy
      - PROXY_PROTOCOL=${PROXY_PROTOCOL:-quic}
      - PROXY_QUIC_HANDSHAKE=${PROXY_QUIC_HANDSHAKE:-true}
      - PROXY_QUIC_DOMAIN=${PROXY_QUIC_DOMAIN:-cloudflare.com}
      - PROXY_DNS_FORWARD=${PROXY_DNS_FORWARD:-false}
      - PROXY_DNS_UPSTREAM=${PROXY_DNS_UPSTREAM:-1.1.1.1:53}
    volumes:
      # Read-only: proxy reads S1–S4 / H1–H4 from the generated AWG config.
      - ~/.amnezia-wg-easy:/etc/amnezia/amneziawg:ro
    ports:
      - "${WG_PORT:-51820}:${WG_PORT:-51820}/udp"
    cap_add:
      # Lets the proxy bind low ports (443/53) even if run non-root later.
      - NET_BIND_SERVICE
    depends_on:
      amnezia-wg2-easy:
        condition: service_healthy
    restart: unless-stopped
```

- [ ] **Step 2: Validate the compose file**

With the `.env` from Task 4 still present:
```bash
docker compose -f docker-compose.proxy.yml config >/dev/null && echo "proxy compose OK"
```
Expected: prints `proxy compose OK`. No errors about undefined services or bad interpolation.

- [ ] **Step 3: Confirm the AWG service publishes no UDP in proxy mode**

Run:
```bash
docker compose -f docker-compose.proxy.yml config | grep -A3 'published.*51821' ; \
docker compose -f docker-compose.proxy.yml config | grep -c '51820' 
```
Expected: the only `51820` references are under the `amnezia-wg-proxy` service's ports (the AWG service block has no UDP publish). Visually confirm the UDP `published: "51820"` appears under `amnezia-wg-proxy`, not under `amnezia-wg2-easy`.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.proxy.yml
git commit -m "feat(proxy): proxy-mode compose stack (AWG + sidecar)"
```

---

### Task 6: justfile

**Files:**
- Create: `justfile`

- [ ] **Step 1: Write the recipes**

```just
# AmneziaWG Easy — task runner.
# Plain mode = AWG2 only. Proxy mode = AWG2 + obfuscation proxy sidecar.

set dotenv-load := true

# List recipes.
default:
    @just --list

# Plain AWG2 (no proxy).
up:
    docker compose up -d

# Stop the plain stack.
down:
    docker compose down

# AWG2 + obfuscation proxy (builds the proxy image).
up-proxy:
    docker compose -f docker-compose.proxy.yml up -d --build

# Stop the proxy stack.
down-proxy:
    docker compose -f docker-compose.proxy.yml down

# Follow logs (plain stack).
logs:
    docker compose logs -f

# Follow logs (proxy stack).
logs-proxy:
    docker compose -f docker-compose.proxy.yml logs -f

# Show container status for both stacks.
ps:
    docker compose ps
```

- [ ] **Step 2: Verify recipes parse and list**

Run:
```bash
just --list
```
Expected: lists `up`, `down`, `up-proxy`, `down-proxy`, `logs`, `logs-proxy`, `ps` with their doc comments. (If `just` is not installed: `cargo install just` or `apt install just`.)

- [ ] **Step 3: Commit**

```bash
git add justfile
git commit -m "feat(proxy): justfile for plain/proxy mode lifecycle"
```

---

### Task 7: README documentation + attribution

**Files:**
- Modify: `README.md` (add an "Optional obfuscation proxy" section + vendoring/attribution note)

- [ ] **Step 1: Add the proxy section and attribution**

Append a section to `README.md` (place it near the existing deployment/usage docs; adjust the surrounding wording to match the file's style). Use this content:

````markdown
## Optional Obfuscation Proxy

You can run the VPN in two modes:

| Mode | Command | What runs |
|------|---------|-----------|
| Plain | `just up` | AmneziaWG 2.0 only (default) |
| Proxy | `just up-proxy` | AmneziaWG 2.0 + UDP obfuscation proxy sidecar |

In **proxy mode** an async UDP proxy sits in front of AmneziaWG and makes the
traffic positively resemble a real **QUIC / DNS / STUN / SIP** service to Deep
Packet Inspection — a second layer on top of AWG's own S1–S4 / H1–H4
randomization. Clients connect exactly as before (same `WG_HOST:WG_PORT`); the
proxy is transparent. Standard AmneziaWG clients get server→client obfuscation;
bidirectional imitation additionally requires WireSock Secure Connect 3.5+ on
the client.

### Setup

```bash
cp .env.example .env       # then edit WG_HOST, PASSWORD, etc.
just up-proxy              # builds the proxy image and starts both containers
```

The client-facing port is `WG_PORT` (e.g. set `WG_PORT=443` with
`PROXY_PROTOCOL=quic`, or `WG_PORT=53` with `PROXY_PROTOCOL=dns`).

### Proxy configuration (`.env`)

| Variable | Default | Purpose |
|----------|---------|---------|
| `PROXY_PROTOCOL` | `quic` | Protocol to imitate: `quic` / `dns` / `stun` / `sip` / `auto` |
| `PROXY_QUIC_HANDSHAKE` | `true` | Complete a real QUIC/TLS handshake to probes (stronger; `false` = stateless Version Negotiation) |
| `PROXY_QUIC_DOMAIN` | `cloudflare.com` | SNI domain for the QUIC handshake cert |
| `PROXY_DNS_FORWARD` | `false` | Answer real DNS queries upstream (only with `dns`/`auto`) |
| `PROXY_DNS_UPSTREAM` | `1.1.1.1:53` | Upstream resolver when forwarding |

### Switching modes

```bash
just down        # or: just down-proxy
just up-proxy    # or: just up
```

Client data persists across modes (shared `~/.amnezia-wg-easy` volume).

### Credits / vendoring

The proxy under [`proxy/`](proxy/) is vendored from
[wiresock/amneziawg-install](https://github.com/wiresock/amneziawg-install)
(`amneziawg-proxy`), MIT-licensed, at commit `549bba8`. Thanks to its authors.
To update it, re-copy the upstream crate over `proxy/` and bump this note.
````

- [ ] **Step 2: Verify the section renders and links resolve**

Run:
```bash
grep -q "Optional Obfuscation Proxy" README.md && grep -q "549bba8" README.md && echo "README OK"
```
Expected: prints `README OK`.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(proxy): document optional obfuscation proxy mode + attribution"
```

---

### Task 8: End-to-end manual verification

No automation — this is the manual acceptance pass. Requires a host with Docker, `/dev/net/tun`, and `NET_ADMIN`/`SYS_MODULE` (a real/VM server, not necessarily this dev box).

**Files:** none (verification only).

- [ ] **Step 1: Prepare `.env`**

```bash
cp .env.example .env
# Edit .env: set WG_HOST to the server's public IP/hostname and PASSWORD.
```

- [ ] **Step 2: Regression — plain mode still works**

```bash
just up
docker compose ps          # amnezia-wg2-easy is "healthy"
```
Expected: UI reachable on `${PORT}`, a client config from the UI connects on `${WG_PORT}`, traffic flows. Then `just down`.

- [ ] **Step 3: Proxy mode — ordering and connectivity**

```bash
just up-proxy
docker compose -f docker-compose.proxy.yml ps
just logs-proxy
```
Expected: `amnezia-wg-proxy` starts only after `amnezia-wg2-easy` is healthy; proxy logs show `configuration loaded` and `AWG parameters loaded` (S1–S4/H1–H4 read from `wg0.conf`), not the "continuing without padding transformation" warning. A client connects on `${WG_PORT}` and traffic flows; UI stats (`transferRx/Tx`, handshake) update.

- [ ] **Step 4: Confirm the wire looks like the imitated protocol**

On the server:
```bash
sudo tcpdump -i any -c 20 -w /tmp/awg-proxy.pcap udp port ${WG_PORT:-51820}
```
Expected: frames decode as the imitated protocol (e.g. QUIC) in Wireshark, with no WireGuard/"malformed" markers.

- [ ] **Step 5: Privileged-port check**

Set `WG_PORT=443` and `PROXY_PROTOCOL=quic` in `.env`, then:
```bash
just down-proxy && just up-proxy && just logs-proxy
```
Expected: proxy binds `0.0.0.0:443` (no permission error); a client with `Endpoint = WG_HOST:443` connects.

- [ ] **Step 6: Data persists across a mode switch**

```bash
just down-proxy && just up
```
Expected: the clients created earlier are still present in the UI (shared volume preserved). Then `just down`.

- [ ] **Step 7: Record the result**

Note in the PR/commit description which steps passed and on what host. No commit needed for this task.
