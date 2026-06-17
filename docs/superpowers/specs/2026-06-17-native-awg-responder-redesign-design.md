# Native AmneziaWG + probe-responder redesign — Design

**Date:** 2026-06-17
**Branch:** `feat/amneziawg-proxy`
**Status:** Design — approved in brainstorm, not yet planned. Run `superpowers:writing-plans` against this doc next.
**Supersedes:** `docs/superpowers/specs/2026-06-15-optional-obfuscation-proxy-sidecar-design.md`
(the inline Rust sidecar). That design's `transform.rs` rewrite + UDP relay are
removed here; only its `responder.rs` probe-response role survives, rewritten in Go.

## Scope

This is subsystem **#1** of a four-part effort. The other three have their own specs:

- **#2** Web-UI restyle to match `amneziawg-refresh-assets`.
- **#3** `awg://v1/` config share-string (`2026-06-17-config-share-string-design.md`, cross-repo).
- **#4** Dual old/new client config export (strips imitation keys for legacy clients).

#4 depends on the `imitate_protocol`-in-config work landed here. #2 and #3 are independent.

## Problem / motivation

The 2026-06-15 sidecar shipped a vendored Rust `amneziawg-proxy` that did **two**
jobs inline on the data path:

1. `transform.rs` — rewrite outgoing S-padding into protocol-conformant filler
   (QUIC/DNS/STUN/SIP), shaping the tunnel against protocol-positive DPI (layer 2/3).
2. `responder.rs` — answer active DPI probes with protocol-valid responses (layer 5).

Since then, job #1 became **native** in our own forks:

- `amneziawg-proxy-linux-kernel-module` — imitation in the kernel module (`src/imitate.c`),
  bidirectional, sender-only, length-invariant, interops with a vanilla peer.
- `amneziawg-go-proxy` — same imitation in Go (`device/obf_imitate.go`,
  `imitate_protocol=none|quic|dns|stun|sip`), plus I-packet builders and a Tier-4
  fake QUIC Initial with SNI (`device/obf_imitate_quic.go`).
- `amneziawg-tools-proxy` — `awg`/`awg-quick` that drive the above.

So `transform.rs` is now redundant in our stack. The inline relay is also
**actively harmful** to the new goal: routing every VPN packet through a userspace
hop throws away the kernel module's throughput, which is the entire reason to go
native.

What the native forks do **not** do is answer active probes — a patched module
still silently drops unauthenticated packets, and that silence is itself a
fingerprint. So `responder.rs` is the one piece worth keeping.

**This redesign:** the Docker image runs **native** AmneziaWG (kernel module via
`awg-quick`, with go userspace as automatic fallback) for the data path, and the
proxy shrinks to a **Go probe-responder** that sits as an **NFQUEUE ingress
filter** — off the data fast-path, answering scanners without relaying real traffic.

## Decisions (settled in brainstorm)

1. **Deployment = `awg-quick` auto-select.** Ship both datapaths in one image;
   `awg-quick` uses the host kernel module if loadable, else falls back to
   `amneziawg-go`. The go fork ("just for case") becomes the automatic safety net.
2. **Kernel module is host-installed, not container-built.** The module loads into
   the *host* kernel (shared by all containers). The image carries only the
   userspace tools; the host installs the module via DKMS (auto-rebuilds across
   host-kernel upgrades). Container needs only `CAP_NET_ADMIN` to create/configure
   `awg0`. No `SYS_MODULE`, no `/lib/modules` mount, no in-container module build.
   (A container-`insmod` path exists but is the fragile fallback we avoid.)
3. **Responder rewritten in Go, behavior-port (not byte-exact).** Consolidates the
   stack to C/Go/Node and drops the Rust+cargo+tokio toolchain. The responder has
   no peer that must reproduce its bytes, so it only needs to emit *plausible*
   protocol responses — a behavior port, not a bit-for-bit one. Go also has the
   mature `go-nfqueue` library the topology needs.
4. **Topology = NFQUEUE, first-contact only.** Native AWG owns `WG_PORT`. An
   iptables rule queues only conntrack-`NEW` inbound UDP to the Go responder;
   `ESTABLISHED` flows bypass userspace entirely (kernel fast path).
5. **Single container, one image.** UI (Node) + `awg-quick` + Go responder run in
   one container under the entrypoint. The responder is an env **toggle** (off by
   default). The old plain/proxy dual-compose split + justfile dance collapse into
   one configurable image.
6. **Config surface for #1 = one global env knob.** `IMITATE_PROTOCOL` applied
   uniformly to the server interface and all client configs; per-client UI control
   is deferred to #2/#4.
7. **Delete the Rust crate outright.** Git history preserves it; keeping a dead
   `proxy/` as "reference" is clutter. The behavior reference is the design + the
   git blob.

## Architecture

One container, one network namespace, owning `WG_PORT/udp` (published) and the
UI `PORT/tcp` (published).

```
                       WG_PORT/udp (published)
 client / scanner ─►  ┌─ single container netns ───────────────────────┐
                      │ iptables: -p udp --dport WG_PORT -m conntrack    │
                      │   --ctstate NEW -j NFQUEUE --queue-num 0         │
                      │   (ESTABLISHED bypasses → kernel awg0, fast path)│
                      │        │                                         │
                      │        ▼  Go responder (go-nfqueue)              │
                      │   1 classify_awg? → ACCEPT                       │
                      │   2 probe(IMITATE)? → reply + DROP               │
                      │   3 else → ACCEPT                                │
                      │                                                  │
                      │ awg0  (kernel module via awg-quick │ go fallback)│
                      │ Node UI :PORT/tcp                                │
                      └──────────────────────────────────────────────────┘
```

### Datapath: `awg-quick` auto-select

```
awg-quick up:
  ├─ host kernel has amneziawg module?  → use it       (kernel fast path; CAP_NET_ADMIN only)
  └─ no module?                          → amneziawg-go  (userspace TUN; needs /dev/net/tun)
```

The same image and the same `wg0.conf`/`wg0.json` work for both datapaths.
`WireGuard.js` continues to write `wg0.conf`; `awg-quick` consumes it unchanged.

### The Go responder (`responder/`, new Go module)

A behavior port of the surviving half of `proxy/src/responder.rs`. It keeps the
rigor of the original:

- **QUIC** — Version-Negotiation response. Preserve the RFC 9000 §6.2 rule: the
  VN response advertises a GREASE value (`0x0a0a0a0a`), **never** `0x00000001`,
  so it doesn't claim v1 support / become a fingerprint.
- **DNS** — SERVFAIL echoing the transaction ID and question section (RFC 1035
  §4.1.1). Keep the strict end-to-end query validation (QR=0, single question,
  valid uncompressed QNAME, QCLASS ∈ {IN, CH, HS, ANY}) so random AWG junk does
  not get misclassified as DNS.
- **STUN** — Binding-Success with `XOR-MAPPED-ADDRESS` for the observed client.
- **SIP** — the dialog state machine (`Idle→Invited→Ringing→Established→Terminated`)
  with reflected `Via/From/To/Call-ID/CSeq` headers.

**Ingress integration (`go-nfqueue`):**

- iptables (rendered by the entrypoint when `RESPONDER=true`):
  `-A INPUT -p udp --dport ${WG_PORT} -m conntrack --ctstate NEW -j NFQUEUE --queue-num 0`
- Only **new/unestablished** datagrams reach userspace. The first packet of every
  real client (its handshake-init) is `NEW` and gets ACCEPTed; conntrack then marks
  the flow `ESTABLISHED`, so all subsequent packets bypass the queue and stay in
  the kernel datapath. Bulk VPN throughput never traverses Go.
- A rate limit on the NFQUEUE rule caps probe/junk flood cost.

**Per-packet verdict (order is correctness-critical):**

1. `classifyAwgPacket(pkt, S/H)` — does the datagram match a real AWG
   handshake/transport (exact S-offset + size + H-range header)? → **ACCEPT**.
2. else `detectProtocol(pkt) == IMITATE_PROTOCOL`? → it is a probe →
   **send the crafted response, DROP**.
3. else → **ACCEPT** (let the kernel `awg0` silently drop genuine junk).

**Why the responder must read S1–S4 / H1–H4:** client→server shaping can make a
real handshake-init *resemble the very protocol we answer* (e.g. `IMITATE=dns`
shapes outgoing padding to look like a DNS query, which `detectProtocol`'s DNS arm
would match). Running `classifyAwgPacket` **first** guarantees a real handshake is
ACCEPTed before any probe test, so the responder never drops a legitimate client.
The responder reads `wg0.conf` once at startup for S/H (params are generated once
and stable across client add/remove) — the same read the old proxy did.

**Responder answers only as `IMITATE_PROTOCOL`** and ignores probes of other
protocols, the way a real single-protocol server does. Answering every protocol
on one port would itself be a fingerprint.

## Config surface (env)

| Var | Default | Effect |
|---|---|---|
| `IMITATE_PROTOCOL` | `none` | `none\|quic\|dns\|stun\|sip`. Drives native sender shaping (server interface **and** every generated client config, via `WireGuard.js`) **and** the responder's answer protocol. |
| `RESPONDER` | `false` | Enables the NFQUEUE active-probe responder. Requires `IMITATE_PROTOCOL != none` and `CAP_NET_ADMIN`; the entrypoint errors out clearly if `RESPONDER=true` with `IMITATE_PROTOCOL=none`. |

Existing `WG_HOST`, `WG_PORT`, `PORT`, `PASSWORD`, `WG_DEFAULT_DNS`,
`WG_DEFAULT_ADDRESS`, `JC/JMIN/JMAX/S1–S4/H1–H4/I1–I5` are unchanged. The old
`PROXY_*` vars are **removed**.

Tier-4 `qinit` (fake QUIC Initial + SNI) and I-packet special-junk builders are
**out of scope for #1** — noted as a future enhancement once the basic
`imitate_protocol` plumbing exists.

## `WireGuard.js` change

When `IMITATE_PROTOCOL != none`, emit the `imitate_protocol` line:

- into the **server `[Interface]`** block of `wg0.conf` (so server→client traffic
  is shaped), and
- into every **generated client `[Interface]`** config (so client→server traffic
  is shaped — closing the layer-3 flow-consistency asymmetry).

This is additive; when `IMITATE_PROTOCOL=none`, output is byte-identical to today.
The exact key name/format must match what `amneziawg-tools-proxy`/`amneziawg-go-proxy`
parse (verify against their config parser during implementation).

## Migration / file-level changes

**Delete:**
- `proxy/` (entire vendored Rust crate)
- `Dockerfile.proxy`
- `docker-compose.proxy.yml`
- old `PROXY_*` env vars (from `.env.example`, README, compose)
- dual-mode `justfile` recipes (`up-proxy`/`down-proxy`/`logs-proxy`)

**Add:**
- `responder/` — Go module (responder + go-nfqueue glue + protocol response builders)
- a Go build stage in the single `Dockerfile`

**Replace / modify:**
- `Dockerfile` — toolchain for `awg-quick` auto-select datapath + Go responder
  build stage; runtime carries `awg`/`awg-quick` (from tools-proxy), `amneziawg-go`
  (fallback), the responder binary, the Node UI.
- `docker-compose.yml` — single stack; `IMITATE_PROTOCOL` + `RESPONDER` env;
  `cap_add: [NET_ADMIN]`; `/dev/net/tun`; document host module + nfqueue/conntrack.
- entrypoint/startup — `awg-quick` auto-select; when `RESPONDER=true`, render the
  NFQUEUE iptables rule and launch the responder alongside the UI under `dumb-init`.
- `src/lib/WireGuard.js` — emit `imitate_protocol` (above).
- `.env.example`, `README`, `justfile` — reflect the single-image model.

## Host requirements (documented)

- For the kernel datapath: the `amneziawg` module installed/loaded on the **host**
  (DKMS). Otherwise the image silently uses the go userspace fallback.
- For the responder: `nfnetlink_queue` and `nf_conntrack` kernel modules
  (standard on mainstream distros). `CAP_NET_ADMIN` on the container.

## Testing (manual — no automated suite per project norms)

1. **Regression:** `IMITATE_PROTOCOL=none`, `RESPONDER=false` → behavior identical
   to current master (client connects on `WG_PORT`, UI on `PORT`, traffic flows).
2. **Datapath auto-select:** module loaded on host → `awg show` confirms the kernel
   interface; `rmmod` the host module + restart → the go fallback brings up the same
   tunnel from the same `wg0.conf`.
3. **Shaping:** `IMITATE_PROTOCOL=quic` → `tcpdump`/Wireshark on `WG_PORT` shows
   QUIC-shaped frames in **both** directions, no WireGuard/malformed markers.
4. **Responder:** `RESPONDER=true` →
   - a QUIC Initial probe gets a valid Version-Negotiation reply;
   - a DNS / STUN probe is **ignored** (consistent with a QUIC-only server);
   - a **real client still connects** — its handshake is ACCEPTed (classify wins
     before probe-detection), not dropped. Re-test with `IMITATE_PROTOCOL=dns` (the
     adversarial case where shaped handshakes resemble the answered protocol).
5. **Fast path:** `iperf` over an established tunnel — responder CPU stays ~0 during
   bulk transfer, confirming `ESTABLISHED` flows bypass userspace.

## Risks / open items for the plan

- **Verify the `imitate_protocol` config key/format** against the actual parsers in
  `amneziawg-tools-proxy` and `amneziawg-go-proxy` before wiring `WireGuard.js`.
- **NFQUEUE rule placement** (INPUT vs PREROUTING) relative to where `awg0` receives
  — confirm conntrack `NEW`/`ESTABLISHED` transitions as expected for the kernel
  datapath vs the go-TUN fallback (the fallback's socket may interact with conntrack
  differently; validate both).
- **Privileged port binding** when `WG_PORT < 1024` — native AWG binds it; confirm
  the container (root + `CAP_NET_ADMIN`) can, or add `CAP_NET_BIND_SERVICE`.
- **Responder ↔ datapath protocol agreement:** the responder answers as
  `IMITATE_PROTOCOL`; the datapath shapes as the same value. Keep them sourced from
  one env var so they cannot drift.
