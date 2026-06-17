# Native AWG Datapath + Image Restructure — Implementation Plan (Plan 1 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Switch the Docker image to a native AmneziaWG datapath (host kernel module via `awg-quick`, with our `amneziawg-go-proxy` userspace fork as automatic fallback) and wire a global `IMITATE_PROTOCOL` knob through `WireGuard.js` into the generated `wg0.conf` / client configs — collapsing the old plain/proxy dual-compose layout into one configurable image.

**Architecture:** The Node app already drives the interface with `awg-quick up wg0` + `awg syncconf` (`wg`/`wg-quick` are symlinks to `awg`/`awg-quick`). Kernel-vs-userspace selection happens *inside* `awg-quick` (`ip link add type amneziawg`, else `WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go`). The image is rebuilt to carry our `-proxy` forks (built from pinned git refs in multi-stage builds); the kernel module is installed on the **host** (DKMS), never in the container. `ImitateProtocol = <proto>` rides through the existing `[Interface]` block to both datapaths (verified: `config.c:600` → netlink `WGDEVICE_A_IMITATE_PROTOCOL` → kernel `imitate.c` / go `uapi.go`).

**Tech Stack:** Node.js 18 (H3 backend, no test runner — ESLint + node assertions + manual), Docker multi-stage (Go 1.24 + Alpine C toolchain), `amneziawg-tools-proxy` (C), `amneziawg-go-proxy` (Go), `awg-quick` (bash).

## Global Constraints

- **`IMITATE_PROTOCOL`** ∈ `none|quic|dns|stun|sip`, default `none` (lowercased; invalid value → throw at startup). Emitted as the `[Interface]` line `ImitateProtocol = <proto>` **only when ≠ `none`**; when `none`, generated config output is **byte-identical to today**.
- **Forks are built from source at pinned refs** via build ARGs `AWG_GO_REF` / `AWG_TOOLS_REF` (default `master`; pin to a tag/SHA in CI). Repos: `https://github.com/creatorofuniverses/amneziawg-go-proxy`, `https://github.com/creatorofuniverses/amneziawg-tools-proxy`.
- **Kernel module is host-installed (DKMS), not container-built.** Container caps: `NET_ADMIN` + `/dev/net/tun` (go fallback). **Remove `SYS_MODULE`** (no in-container insmod).
- **Single image, single `docker-compose.yml`.** Delete `proxy/`, `Dockerfile.proxy`, `docker-compose.proxy.yml`, the dual `justfile` recipes, and all `PROXY_*` env.
- **`RESPONDER` / `QUIC_*` env are NOT consumed in Plan 1** — they belong to Plans 2/3 (the probe-responder). Do not wire them here.
- **Commits:** conventional-commit prefixes; end every commit message with the trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- **No automated test suite exists** (per `CLAUDE.md`). "Tests" below are runnable `node` assertions, `npm run lint`, and explicit build/run verification commands.

---

## File Structure

- `src/config.js` — **modify**: add validated `IMITATE_PROTOCOL` export.
- `src/lib/WireGuard.js` — **modify**: emit `ImitateProtocol` in the server `[Interface]` (`__saveConfig`) and client `[Interface]` (`getClientConfiguration`).
- `Dockerfile` — **rewrite**: multi-stage build of both forks + Alpine runtime + `WG_QUICK_USERSPACE_IMPLEMENTATION`.
- `docker-compose.yml` — **modify**: `IMITATE_PROTOCOL` env, build ARGs, drop `SYS_MODULE`.
- `.env.example` — **rewrite**: drop `PROXY_*`, add `IMITATE_PROTOCOL`.
- `justfile` — **modify**: remove `*-proxy` recipes.
- `README` — **modify**: native datapath + host-module + `IMITATE_PROTOCOL` section.
- **Delete**: `proxy/`, `Dockerfile.proxy`, `docker-compose.proxy.yml`, `proxy-entrypoint.sh`.

---

## Task 1: `IMITATE_PROTOCOL` config knob

**Files:**
- Modify: `src/config.js`
- Test: inline `node` assertions (run from `src/`)

**Interfaces:**
- Produces: `require('../config').IMITATE_PROTOCOL` → a lowercased string in `{none,quic,dns,stun,sip}`; throws at module load on any other value.

- [ ] **Step 1: Write the failing test**

Run from `src/`:
```bash
cd src
IMITATE_PROTOCOL=quic node -e "if(require('./config').IMITATE_PROTOCOL!=='quic'){process.exit(1)};console.log('set ok')" \
&& node -e "if(require('./config').IMITATE_PROTOCOL!=='none'){process.exit(1)};console.log('default ok')" \
&& IMITATE_PROTOCOL=QUIC node -e "if(require('./config').IMITATE_PROTOCOL!=='quic'){process.exit(1)};console.log('lowercase ok')" \
&& IMITATE_PROTOCOL=bogus node -e "try{require('./config');console.error('did not throw');process.exit(1)}catch(e){console.log('reject ok')}"
```

- [ ] **Step 2: Run it to verify it fails**

Expected: FAIL — first line errors because `IMITATE_PROTOCOL` is `undefined` (not `'quic'`).

- [ ] **Step 3: Implement the knob**

Add to `src/config.js` (after the existing `WG_*` exports, before the obfuscation params is fine):
```js
const IMITATE_ALLOWED = ['none', 'quic', 'dns', 'stun', 'sip'];
const imitateProtocol = (process.env.IMITATE_PROTOCOL || 'none').toLowerCase();
if (!IMITATE_ALLOWED.includes(imitateProtocol)) {
  throw new Error(
    `IMITATE_PROTOCOL must be one of ${IMITATE_ALLOWED.join(', ')} (got: ${process.env.IMITATE_PROTOCOL})`
  );
}
module.exports.IMITATE_PROTOCOL = imitateProtocol;
```

- [ ] **Step 4: Run the test to verify it passes**

Run the Step 1 command. Expected: `set ok` / `default ok` / `lowercase ok` / `reject ok`.

- [ ] **Step 5: Lint**

Run: `cd src && npm run lint`
Expected: no errors in `config.js`.

- [ ] **Step 6: Commit**

```bash
git add src/config.js
git commit -m "feat(config): add validated IMITATE_PROTOCOL knob

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Emit `ImitateProtocol` in server + client configs

**Files:**
- Modify: `src/lib/WireGuard.js` (import block ~L12-41; `__saveConfig` server `[Interface]` ~L134-153; `getClientConfiguration` client `[Interface]` ~L249-266)
- Test: `src/test-imitate-emit.js` (temporary assertion script, deleted in Step 6)

**Interfaces:**
- Consumes: `IMITATE_PROTOCOL` from Task 1.
- Produces: `wg0.conf` server `[Interface]` and `getClientConfiguration()` output each contain a line `ImitateProtocol = <proto>` iff `IMITATE_PROTOCOL !== 'none'`.

- [ ] **Step 1: Write the failing test**

Create `src/test-imitate-emit.js`:
```js
'use strict';
const assert = require('node:assert');

(async () => {
  // Stub Util.exec so no real `wg` binary is needed.
  const Util = require('./lib/Util');
  Util.exec = async () => 'stub';

  const WireGuard = require('./lib/WireGuard');
  const wg = new WireGuard();

  // Minimal fake config + client (bypass key generation / disk).
  const fakeServer = {
    privateKey: 'priv', publicKey: 'pub', address: '10.8.0.1',
    jc: 5, jmin: 50, jmax: 1000, s1: 100, s2: 100, s3: 100, s4: 100,
    h1: { min: 1, max: 2 }, h2: { min: 3, max: 4 },
    h3: { min: 5, max: 6 }, h4: { min: 7, max: 8 },
  };
  wg.getConfig = async () => ({ server: fakeServer, clients: {} });
  wg.getClient = async () => ({
    privateKey: 'cpriv', address: '10.8.0.2', publicKey: 'pub',
    preSharedKey: null, enabled: true,
  });

  const clientConf = await wg.getClientConfiguration({ clientId: 'x' });

  if (process.env.IMITATE_PROTOCOL === 'quic') {
    assert.match(clientConf, /^ImitateProtocol = quic$/m, 'client must carry ImitateProtocol');
    console.log('client emit ok');
  } else {
    assert.doesNotMatch(clientConf, /ImitateProtocol/, 'none must omit ImitateProtocol');
    console.log('client none ok');
  }
})().catch((e) => { console.error(e.message); process.exit(1); });
```

Run:
```bash
cd src
IMITATE_PROTOCOL=quic node test-imitate-emit.js && node test-imitate-emit.js
```

- [ ] **Step 2: Run it to verify it fails**

Expected: FAIL on the `quic` run — `client must carry ImitateProtocol` (line not emitted yet).

- [ ] **Step 3: Add the import**

In `src/lib/WireGuard.js`, add `IMITATE_PROTOCOL,` to the destructured `require('../config')` block (alongside `I5,`):
```js
  I5,
  IMITATE_PROTOCOL,
} = require('../config');
```

- [ ] **Step 4: Emit in the server `[Interface]`**

In `__saveConfig`, the server template ends with the `H4 = ...` line. Insert the conditional line immediately after it (before the closing backtick), so `none` leaves output byte-identical:
```js
H4 = ${config.server.h4.min}-${config.server.h4.max}
${IMITATE_PROTOCOL !== 'none' ? `ImitateProtocol = ${IMITATE_PROTOCOL}\n` : ''}`;
```

- [ ] **Step 5: Emit in the client `[Interface]`**

In `getClientConfiguration`, insert the same conditional right after the client `H4 = ...` line and before the `${I1 ? ...}` block:
```js
H4 = ${config.server.h4.min}-${config.server.h4.max}
${IMITATE_PROTOCOL !== 'none' ? `ImitateProtocol = ${IMITATE_PROTOCOL}\n` : ''}\
${I1 ? `I1 = ${I1}\n` : ''}\
```

- [ ] **Step 6: Run the test to verify it passes, then lint and clean up**

```bash
cd src
IMITATE_PROTOCOL=quic node test-imitate-emit.js && node test-imitate-emit.js
npm run lint
rm test-imitate-emit.js
```
Expected: `client emit ok` then `client none ok`; lint clean.

- [ ] **Step 7: Commit**

```bash
git add src/lib/WireGuard.js
git commit -m "feat(wireguard): emit ImitateProtocol in server and client configs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Rebuild the Docker image around the native datapath

**Files:**
- Rewrite: `Dockerfile`

**Interfaces:**
- Produces: an image whose `awg`/`awg-quick`/`amneziawg-go` are our `-proxy` forks, with `WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go` set so `awg-quick` falls back to userspace when the host kernel module is absent.

- [ ] **Step 1: Rewrite `Dockerfile`**

Replace the entire file with:
```dockerfile
# ── Web UI node_modules (node 18: node 20 hangs on armv6/v7 builds) ──
FROM docker.io/library/node:18-alpine AS build_node_modules
COPY src /app
WORKDIR /app
RUN npm ci --omit=dev && mv node_modules /node_modules

# ── amneziawg-go-proxy (userspace datapath fallback) ──
FROM golang:1.24-alpine AS build_awg_go
ARG AWG_GO_REF=master
RUN apk add --no-cache git make
RUN git clone https://github.com/creatorofuniverses/amneziawg-go-proxy.git /src \
 && cd /src && git checkout "${AWG_GO_REF}" && make
# binary: /src/amneziawg-go

# ── amneziawg-tools-proxy (awg + awg-quick) ──
FROM alpine:3.20 AS build_awg_tools
ARG AWG_TOOLS_REF=master
RUN apk add --no-cache git build-base linux-headers bash
RUN git clone https://github.com/creatorofuniverses/amneziawg-tools-proxy.git /src \
 && cd /src && git checkout "${AWG_TOOLS_REF}" \
 && make -C src WITH_WGQUICK=yes WITH_BASHCOMPLETION=no WITH_SYSTEMDUNITS=no \
 && make -C src install DESTDIR=/out PREFIX=/usr WITH_WGQUICK=yes WITH_BASHCOMPLETION=no WITH_SYSTEMDUNITS=no
# installs: /out/usr/bin/awg, /out/usr/bin/awg-quick

# ── Runtime ──
FROM alpine:3.20
RUN apk add --no-cache nodejs npm bash iproute2 iptables dumb-init
COPY --from=build_awg_go    /src/amneziawg-go      /usr/bin/amneziawg-go
COPY --from=build_awg_tools /out/usr/bin/awg       /usr/bin/awg
COPY --from=build_awg_tools /out/usr/bin/awg-quick /usr/bin/awg-quick
RUN ln -sf /usr/bin/awg /usr/bin/wg \
 && ln -sf /usr/bin/awg-quick /usr/bin/wg-quick \
 && chmod +x /usr/bin/awg-quick
COPY --from=build_node_modules /app /app
COPY --from=build_node_modules /node_modules /node_modules
ENV DEBUG=Server,WireGuard
# awg-quick uses the host kernel module if present, else this userspace impl:
ENV WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go
WORKDIR /app
HEALTHCHECK CMD /usr/bin/timeout 5s /bin/sh -c "/usr/bin/wg show | /bin/grep -q interface || exit 1" --interval=1m --timeout=5s --retries=3
CMD ["/usr/bin/dumb-init", "node", "server.js"]
```

- [ ] **Step 2: Build the image**

Run: `docker build --tag amnezia-wg-easy:plan1 .`
Expected: build succeeds through all four stages. If a fork build target/path differs, fix the `make`/`COPY` line and rebuild (the binaries must land at `/usr/bin/{awg,awg-quick,amneziawg-go}`).

- [ ] **Step 3: Verify the forks are the imitate-capable builds**

Run:
```bash
docker run --rm amnezia-wg-easy:plan1 sh -c "awg --version; awg-quick --version 2>&1 | head -1; amneziawg-go --version 2>&1 | head -1; awg setconf --help 2>&1 | grep -qi imitate && echo 'imitate-aware' || awg --help 2>&1 | head -1"
```
Expected: `awg`/`awg-quick`/`amneziawg-go` all present and runnable; confirms our forks installed.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile
git commit -m "build: native AWG datapath image (awg-quick auto-select, forks from source)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Single-stack compose, env, justfile, README

**Files:**
- Modify: `docker-compose.yml`
- Rewrite: `.env.example`
- Modify: `justfile`
- Modify: `README.md`

**Interfaces:**
- Consumes: the `amnezia-wg-easy:plan1` image and `IMITATE_PROTOCOL` (Tasks 1–3).

- [ ] **Step 1: Update `docker-compose.yml`**

In `docker-compose.yml`: under `build:` add the fork-ref ARGs; add `IMITATE_PROTOCOL` to `environment`; **remove `SYS_MODULE`** from `cap_add`. The `build` and relevant blocks become:
```yaml
    build:
      context: .
      args:
        AWG_GO_REF: ${AWG_GO_REF:-master}
        AWG_TOOLS_REF: ${AWG_TOOLS_REF:-master}
    environment:
      - LANGUAGE=${LANGUAGE:-en}
      - WG_HOST=${WG_HOST}
      - PASSWORD=${PASSWORD}
      - PORT=${PORT:-51821}
      - WG_PORT=${WG_PORT:-51820}
      # Native obfuscation imitation (none|quic|dns|stun|sip):
      - IMITATE_PROTOCOL=${IMITATE_PROTOCOL:-none}
    # ...
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
```
(Leave the other commented obfuscation hints as-is.)

- [ ] **Step 2: Rewrite `.env.example`**

Replace the whole file with:
```bash
# Copy to .env and fill in. Consumed by docker-compose.yml.

# ── Core (required) ───────────────────────────────────────────────
# Your server's public IP or hostname (REQUIRED).
WG_HOST=
# Web UI admin password (leave empty to disable authentication).
PASSWORD=
# UI language code (e.g. en).
LANGUAGE=en
# Web UI port (tcp).
PORT=51821
# Client-facing VPN port (udp). Clients dial WG_HOST:WG_PORT.
WG_PORT=51820

# ── Native traffic imitation ──────────────────────────────────────
# Shape obfuscation padding/junk to resemble a real protocol, on BOTH
# the server interface and every generated client config.
# One of: none | quic | dns | stun | sip   (default: none)
IMITATE_PROTOCOL=none

# ── Datapath fork refs (pin in CI for reproducible builds) ────────
# AWG_GO_REF=master
# AWG_TOOLS_REF=master

# NOTE: the active-probe responder (RESPONDER / QUIC_* env) ships in a
# later phase; it is not consumed by this image yet.
```

- [ ] **Step 3: Trim `justfile`**

Replace the whole file with:
```
# AmneziaWG Easy — task runner. Single native image; IMITATE_PROTOCOL toggles imitation.

set dotenv-load := true

# List recipes.
default:
    @just --list

# Bring the stack up.
up:
    docker compose up -d --build

# Stop the stack.
down:
    docker compose down

# Follow logs.
logs:
    docker compose logs -f

# Show container status.
ps:
    docker compose ps
```

- [ ] **Step 4: Update the README**

In `README.md`, replace the "Optional obfuscation proxy" section (and any `PROXY_*` / proxy-compose references) with a "Native traffic imitation" section stating:
- The image runs native AmneziaWG via `awg-quick`, using the **host kernel module** if installed (DKMS) else the bundled `amneziawg-go` userspace fork — same image either way.
- `IMITATE_PROTOCOL=none|quic|dns|stun|sip` shapes the server interface **and** every client config.
- Host kernel-module install (DKMS) is the recommended datapath; document the `CAP_NET_ADMIN` + `/dev/net/tun` requirement and that `SYS_MODULE` is no longer needed.
- Forks are built from source at `AWG_GO_REF`/`AWG_TOOLS_REF`.

- [ ] **Step 5: Lint the compose/env (syntactic check)**

Run: `docker compose config >/dev/null && echo 'compose ok'`
Expected: `compose ok` (validates `docker-compose.yml` + `.env`/defaults parse).

- [ ] **Step 6: Commit**

```bash
git add docker-compose.yml .env.example justfile README.md
git commit -m "feat: single-stack compose with IMITATE_PROTOCOL; drop SYS_MODULE

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Delete the old proxy artifacts + regression check

**Files:**
- Delete: `proxy/`, `Dockerfile.proxy`, `docker-compose.proxy.yml`, `proxy-entrypoint.sh`

**Interfaces:** none produced; this is the clean cut + the regression gate proving `IMITATE_PROTOCOL=none` is unchanged behavior.

- [ ] **Step 1: Remove the dead Rust sidecar and its compose**

```bash
git rm -r proxy Dockerfile.proxy docker-compose.proxy.yml proxy-entrypoint.sh
```

- [ ] **Step 2: Confirm nothing else references them**

Run:
```bash
grep -rIn "PROXY_\|docker-compose.proxy\|Dockerfile.proxy\|proxy-entrypoint\|/proxy" \
  --exclude-dir=.git --exclude-dir=docs --exclude-dir=node_modules .
```
Expected: no matches (docs excluded — the specs intentionally reference the old design).

- [ ] **Step 3: Regression — `none` config is byte-identical**

Build + run with `IMITATE_PROTOCOL` unset against a temp volume, then confirm the generated `wg0.conf` contains **no** `ImitateProtocol` line and otherwise matches the current format:
```bash
docker build --tag amnezia-wg-easy:plan1 .
docker run --rm -e WG_HOST=test.example -v /tmp/wgreg:/etc/amnezia/amneziawg \
  amnezia-wg-easy:plan1 sh -c "node server.js & sleep 4; grep -c ImitateProtocol /etc/amnezia/amneziawg/wg0.conf || true; grep -q '^ListenPort = ' /etc/amnezia/amneziawg/wg0.conf && echo 'conf ok'"
```
Expected: `0` (no ImitateProtocol line) and `conf ok`. (Tunnel bring-up may warn without the host module — that's the go fallback path; the config-generation regression is what this checks.)

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: remove vendored Rust proxy sidecar and dual-compose layout

Superseded by the native datapath + (forthcoming) Go probe-responder.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Notes for Plans 2 & 3 (do not implement here)

- **Plan 2 (probe-responder, non-QUIC):** new `responder/` Go module — `classifyAwgPacket` (reads S/H from `wg0.conf`), `detectProtocol`, DNS SERVFAIL + STUN Binding-Success builders, QUIC Version-Negotiation, raw-socket egress (v4 + v6, mandatory v6 checksum), NFQUEUE NEW-only loop. New env `RESPONDER` (+ entrypoint guard: error if `RESPONDER=true` & `IMITATE_PROTOCOL=none`, warn on `sip`), caps `NET_ADMIN`+`NET_RAW`, supervised side-process under `dumb-init` with crash-isolation.
- **Plan 3 (QUIC full handshake):** R2-1 connmark-claim prototype **first** (set the conntrack mark via the nfqueue verdict CT facility / `libnetfilter_conntrack`, masked bit `0x1/0x1` disjoint from `awg-quick` fwmark; prove the prober's 2nd packet re-queues instead of stalling at `awg0`), then the embedded `quic-go` endpoint over a custom `net.PacketConn` + dynamic SNI cert resolver. Gate the endpoint work on the prototype passing.
