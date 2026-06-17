# Native AmneziaWG + probe-responder redesign — Design

**Date:** 2026-06-17
**Branch:** `feat/amneziawg-proxy`
**Status:** Design — **ready for `superpowers:writing-plans`** (round-2 review verdict).
Incorporates both review rounds in `2026-06-17-native-awg-responder-redesign-review.md`
(round 1: F1–F6; round 2: R2-1…R2-6). The QUIC-handshake flow-claim (R2-1) is gated
behind a first prototype milestone; the entire non-QUIC scope is plan-ready as-is.
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
   iptables rule queues conntrack-`NEW` inbound UDP (plus probe flows the responder
   explicitly claims via connmark — see Decision 9) to the Go responder; established,
   unclaimed flows bypass userspace entirely (kernel fast path).
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
8. **SIP responder is deferred; SIP *shaping* stays.** SIP is the only stateful
   responder (per-client dialog + timed `100/180/200` follow-ups). The #1 responder
   covers the three stateless protocols (QUIC/DNS/STUN). `IMITATE_PROTOCOL=sip` still
   shapes traffic natively (kernel `imitate_sip_modifier` / go), it just has no
   active-probe reply — the same posture as `RESPONDER=false`. SIP responder is a
   documented future tier. (Review F2.)
9. **QUIC responder ports the full TLS-1.3 handshake, not VN-only.** Chosen for
   strong probe resistance: a realistic prober sends a well-formed v1 Initial and
   expects a `ServerHello`, which VN cannot give. This is the larger-scope choice and
   its cost — multi-RTT flow ownership under NFQUEUE — is designed for below. (Review
   F3, and the new flow-ownership consequence.)

## Architecture

One container, one network namespace, owning `WG_PORT/udp` (published) and the
UI `PORT/tcp` (published).

```
                       WG_PORT/udp (published)
 client / scanner ─►  ┌─ single container netns ───────────────────────────┐
                      │ iptables: udp dpt WG_PORT, NEW or connmark 0x1       │
                      │   → NFQUEUE 0   (ESTABLISHED+unmarked → awg0 fast path)│
                      │        │                                             │
                      │        ▼  Go responder (go-nfqueue, userspace)       │
                      │   1 classify_awg (hs OR transport)? → ACCEPT         │
                      │   2 QUIC probe?  → quic-go flight, mark flow, DROP   │
                      │     DNS/STUN probe? → reply, DROP                    │
                      │   3 else → ACCEPT                                    │
                      │        └─ replies injected via raw socket (sport=WG_PORT)
                      │                                                     │
                      │ awg0  (kernel module via awg-quick │ go fallback)   │
                      │ Node UI :PORT/tcp                                   │
                      └──────────────────────────────────────────────────────┘
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

A behavior port of the surviving half of `proxy/src/responder.rs` (not byte-exact;
a probe-responder has no peer that must reproduce its bytes). The #1 responder
handles the three **stateless** protocols plus the **stateful QUIC handshake**;
SIP is deferred (Decision 8). It keeps the rigor of the original:

- **QUIC** — two behaviors, both ported:
  - *Version-Negotiation* (RFC 9000 §17.2.1) for unsupported-version probes,
    preserving the §6.2 rule: advertise a GREASE value (`0x0a0a0a0a`), **never**
    `0x00000001`, so it doesn't claim v1 support / become a fingerprint.
  - *Full TLS-1.3 handshake continuation* (Decision 9) for a well-formed v1
    Initial: decrypt the Initial (RFC 9001 keys), parse the ClientHello, emit a
    `ServerHello`/Certificate flight. The Rust uses `quinn-proto` + `rustls` +
    `rcgen` with a **dynamic SNI resolver** (self-signed cert generated/cached per
    ClientHello SNI). The Go port uses **`quic-go`** as a server over a custom
    `net.PacketConn` (reads queued probe packets, writes via the raw-socket egress
    below) with a `tls.Config.GetCertificate` callback mirroring the SNI resolver.
- **DNS** — SERVFAIL echoing the transaction ID and question section (RFC 1035
  §4.1.1). Keep the strict end-to-end query validation (QR=0, single question,
  valid uncompressed QNAME, QCLASS ∈ {IN, CH, HS, ANY}) so random AWG junk does
  not get misclassified as DNS. Single-shot, stateless.
- **STUN** — Binding-Success with `XOR-MAPPED-ADDRESS` for the observed client.
  Single-shot, stateless.
- **SIP** — *deferred* (Decision 8). The dialog state machine + timed responses
  are not ported in #1; `IMITATE_PROTOCOL=sip` shapes traffic but is not
  actively answered. If `RESPONDER=true` with `IMITATE_PROTOCOL=sip`, the
  entrypoint logs a warning that SIP probes are not answered (shaping still applies)
  and the responder runs only its classify/ACCEPT path. **README framing (Review
  R2-5):** `IMITATE_PROTOCOL=sip` with `RESPONDER=true` is the *least-protected*
  combination — a SIP probe gets `awg0`'s silence, the very fingerprint the responder
  exists to remove. Document SIP as "shaping only, no active-probe defense yet," not
  as a peer of QUIC/DNS/STUN.

**Ingress integration (`go-nfqueue`).** `go-nfqueue` runs the responder in
**userspace** — it is *not* a kernel filter, so per-flow state, timers, and an
embedded QUIC stack are all available to it.

- iptables intent (rendered by the entrypoint when `RESPONDER=true`) — exact rule
  ordering/syntax is for the plan to finalize and test; the *intent* is:
  ```
  # probe-claimed flows: keep the WHOLE flow going to the responder (multi-RTT QUIC).
  # Match the CONNMARK directly (masked bit), so no per-packet restore-mark is needed.
  -A INPUT -p udp --dport ${WG_PORT} -m connmark --mark 0x1/0x1 -j NFQUEUE --queue-num 0
  # otherwise only first-contact packets reach userspace
  -A INPUT -p udp --dport ${WG_PORT} -m conntrack --ctstate NEW -j NFQUEUE --queue-num 0
  ```
- **How the claim mark is persisted (Review R2-1 — the broken-on-paper part):** the
  responder does **not** persist the mark via an iptables `--save-mark` rule. That
  cannot work: the QUIC verdict is DROP, and (a) DROP ends chain traversal so a
  `--save-mark` rule never runs, (b) a DROPped packet's conntrack entry is never
  *confirmed* (confirmation is post-verdict) so there is no ct entry to mark, and
  (c) the raw-socket reply leaving via OUTPUT (`sport=WG_PORT`) is what conntrack
  confirms — in the reverse-as-original direction — which is exactly what flips the
  prober's next packet to `ESTABLISHED` and routes it to `awg0`, the stall we are
  trying to prevent. Instead the responder sets the **conntrack mark on the entry
  directly**, either via the nfqueue verdict's CT facility (`go-nfqueue` `NFQA_CT` /
  conntrack-mark attribute on the verdict) or via `libnetfilter_conntrack`. This is
  the **first prototype milestone** (see Risks); the rest of the QUIC-handshake work
  is gated on it.
- **Mask the bit (Review R2-2):** use a dedicated masked bit (`0x1/0x1`), disjoint
  from any fwmark `awg-quick`/`wg0.conf` sets for policy routing (the `Table`/fwmark
  path), so the claim mark never clobbers or collides with the datapath's fwmark on
  the bulk path.
- **NEW-only by default** so real clients cost one userspace round-trip then run in
  the kernel: a real handshake-init is `NEW` → ACCEPTed → conntrack marks the flow
  `ESTABLISHED` → all later packets bypass the queue. **Bulk VPN throughput never
  traverses Go.**
- **Probe flows are "claimed" via connmark** so the responder keeps seeing them
  across RTTs (needed for the QUIC handshake — see below). Real flows are never
  marked, so they stay on the kernel fast path.
- A rate limit on the NEW rule caps probe/junk flood cost.

**Per-packet verdict (order is correctness-critical):**

1. `classifyAwgPacket(pkt, S/H)` — does the datagram match a real AWG
   handshake **or transport** packet (try all four S/H pairs: exact S-offset +
   size + H-range; transport is variable-size with a min-size check)? → **ACCEPT**.
2. else `detectProtocol(pkt) == IMITATE_PROTOCOL`? → it is a probe:
   - **QUIC** → feed to the embedded `quic-go` endpoint; it emits the server flight
     via raw egress. **Set the conntrack mark `0x1/0x1` on the entry** (via the
     verdict's CT facility, *not* an iptables save-mark — see R2-1 below) so the rest
     of the multi-RTT handshake stays queued to us. **DROP** the queued packet (we
     re-emit, not forward).
   - **DNS / STUN** → build the response, **send via raw egress, DROP**. No mark
     (single-shot).
3. else → **ACCEPT** (let the kernel `awg0` silently drop genuine junk).

**Why the responder must read S1–S4 / H1–H4 (and classify transport, not just
handshakes):** client→server shaping can make a real handshake-init *resemble the
very protocol we answer* (e.g. `IMITATE=dns` shapes outgoing padding to look like a
DNS query, which `detectProtocol`'s DNS arm would match). Running
`classifyAwgPacket` **first** guarantees a real packet is ACCEPTed before any probe
test. It must cover **transport** packets too (S4/H4), because after the UDP
conntrack idle-timeout a mid-stream transport packet re-enters as `NEW` and must be
recognized as real AWG, not mistaken for a probe (Review F6). The Rust classifier
tries all four S/H pairs; the Go port must match. The responder reads `wg0.conf`
once at startup for S/H (params are generated once and stable).

**Responder answers only as `IMITATE_PROTOCOL`** and ignores probes of other
protocols, the way a real single-protocol server does. Answering every protocol on
one port would itself be a fingerprint.

#### Response egress — raw-socket injection (Review F1)

`awg0` owns `WG_PORT`, so the responder **cannot** bind a UDP socket there to reply,
and an NFQUEUE verdict (ACCEPT/DROP/mangle) **cannot originate** a new datagram. All
responder replies are therefore injected with a **raw socket**:

- A `SOCK_RAW` socket forging **source port = `WG_PORT`**, dest = the observed client
  `addr:port`. Both **IPv4 and IPv6** paths are required:
  - **v4:** `IP_HDRINCL` (v4-only), hand-build the IP + UDP headers + checksums.
  - **v6:** `IP_HDRINCL` does **not** apply; use `IPV6_HDRINCL` (newer kernels) or let
    the kernel build the IPv6 header. Either way the **IPv6 UDP checksum is mandatory**
    (it is optional in v4), computed over the v6 pseudo-header — a missing v6 checksum
    means replies are silently dropped (Review R2-4).
- Capability: **`CAP_NET_RAW`** (in addition to `CAP_NET_ADMIN` for nfqueue/iptables).
- **No loop-back:** injected replies are *outbound* with `--sport WG_PORT`; the
  INPUT queueing rules match `--dport WG_PORT`, so replies never re-enter the queue.
  (The plan must still confirm no OUTPUT-side rule interferes.)

#### QUIC handshake continuation under NFQUEUE (consequence of Decision 9)

A full QUIC handshake is **multi-RTT**, which collides with "ESTABLISHED bypasses
userspace": without intervention, the prober's 2nd packet would be `ESTABLISHED`
and get delivered to `awg0` (which drops it), stalling the handshake. The
**connmark claim** above resolves this: once step 2 classifies a flow as a QUIC
probe, that flow's subsequent packets keep being queued to the responder (never to
`awg0`), so the embedded `quic-go` endpoint can complete the exchange. This is the
principal added complexity of choosing the full handshake over VN-only, and it is
intentional: a prober is not a real client, so keeping its flow off the kernel
fast-path is correct. A per-flow idle TTL evicts abandoned probe state and clears
the connmark.

#### Crash isolation

The responder is a **side filter**: if it crashes, the tunnel must keep serving.
The entrypoint runs it as a separate supervised process under `dumb-init`; on
responder exit the NFQUEUE rules are removed (so traffic falls through to `awg0`
normally) and the datapath/UI are unaffected. Restart policy is the responder's
own; a dead responder degrades active-probe defense, never connectivity.

## Config surface (env)

| Var | Default | Effect |
|---|---|---|
| `IMITATE_PROTOCOL` | `none` | `none\|quic\|dns\|stun\|sip`. Drives native sender shaping (server interface **and** every generated client config, via `WireGuard.js`) **and** the responder's answer protocol. `sip` shapes but is not actively answered (Decision 8). |
| `RESPONDER` | `false` | Enables the NFQUEUE active-probe responder. Requires `IMITATE_PROTOCOL != none`, `CAP_NET_ADMIN`, and `CAP_NET_RAW`; the entrypoint errors out clearly if `RESPONDER=true` with `IMITATE_PROTOCOL=none`, and warns (does not fail) if `IMITATE_PROTOCOL=sip`. |
| `QUIC_HANDSHAKE` | `true` | Only meaningful when `IMITATE_PROTOCOL=quic` + `RESPONDER=true`. `true` = full TLS-1.3 handshake continuation; `false` = Version-Negotiation only (weaker). Mirrors the old `PROXY_QUIC_HANDSHAKE` (which also defaulted true). |
| `QUIC_CERT_DOMAIN` | `cloudflare.com` | SNI/cert domain for the QUIC handshake's default self-signed cert (the dynamic SNI resolver still mints per-ClientHello certs). Must be non-empty when `QUIC_HANDSHAKE=true`. Mirrors the old `PROXY_QUIC_DOMAIN`. |

Existing `WG_HOST`, `WG_PORT`, `PORT`, `PASSWORD`, `WG_DEFAULT_DNS`,
`WG_DEFAULT_ADDRESS`, `JC/JMIN/JMAX/S1–S4/H1–H4/I1–I5` are unchanged. The old
`PROXY_*` vars are **removed** (`PROXY_PROTOCOL`, `PROXY_DNS_FORWARD`,
`PROXY_DNS_UPSTREAM`, `PROXY_QUIC_HANDSHAKE` → `QUIC_HANDSHAKE`,
`PROXY_QUIC_DOMAIN` → `QUIC_CERT_DOMAIN`, `PROXY_BACKEND_HOST`).

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

**Config key/format — verified (Review F4):** the `[Interface]` key is
`ImitateProtocol = <quic|dns|stun|sip>`. It is parsed **first-class** by `awg`
(`amneziawg-tools-proxy/src/config.c:600`, also accepts the `imitate_protocol`
setconf arg), carried over netlink as `WGDEVICE_A_IMITATE_PROTOCOL`
(`ipc-linux.h:228`), and round-trips through `awg showconf`. **Both** datapaths
consume it: the kernel module handles the attribute end-to-end
(`netlink.c:62/886–893` → `wg->imitate_proto` → `imitate.c:437–448`) and the go
fork via UAPI (`device/uapi.go:391`). So `WireGuard.js` emitting `ImitateProtocol`
is confirmed to reach both backends — no longer an assumption.

## Explicitly dropped from `responder.rs` (Review F5)

For an honest scope, these `responder.rs`/`config.rs` behaviors are intentionally
**not** carried over:

- **`auto` protocol mode** (`PROXY_PROTOCOL=auto`, per-client protocol-lock on first
  detection) — dropped. Sender shaping must commit to one shape, and answering many
  protocols on one port is itself a fingerprint; `IMITATE_PROTOCOL` is single-valued
  by design.
- **DNS forward-to-upstream** (`PROXY_DNS_FORWARD` / `PROXY_DNS_UPSTREAM`) — dropped.
  The responder emits a synthetic SERVFAIL only; it is not a real resolver.
- **Per-client app-level rate limiting** (token bucket in the proxy) — replaced by the
  coarser **iptables rate-limit on the NEW rule** (per-rule, not per-client). Adequate
  for flood protection; the semantic change is noted.
- **SIP responder** — deferred (Decision 8); shaping retained.
- **QUIC Version-Negotiation-only as the *sole* behavior** — superseded; #1 ports the
  full handshake (Decision 9) with VN retained for the unsupported-version case.

## Migration / file-level changes

**Delete:**
- `proxy/` (entire vendored Rust crate)
- `Dockerfile.proxy`
- `docker-compose.proxy.yml`
- old `PROXY_*` env vars (from `.env.example`, README, compose)
- dual-mode `justfile` recipes (`up-proxy`/`down-proxy`/`logs-proxy`)

**Add:**
- `responder/` — Go module: `detectProtocol`/`classifyAwgPacket`, DNS/STUN single-shot
  builders, the QUIC VN builder, the embedded `quic-go` handshake endpoint + dynamic
  SNI cert resolver, the raw-socket egress (v4/v6), and the `go-nfqueue` loop with
  connmark flow-claiming. Dependencies: `go-nfqueue`, `quic-go`.
- a Go build stage in the single `Dockerfile`

**Replace / modify:**
- `Dockerfile` — toolchain for `awg-quick` auto-select datapath + Go responder
  build stage; runtime carries `awg`/`awg-quick` (from tools-proxy), `amneziawg-go`
  (fallback), the responder binary, the Node UI, and `iptables`.
- `docker-compose.yml` — single stack; `IMITATE_PROTOCOL` + `RESPONDER` +
  `QUIC_HANDSHAKE` + `QUIC_CERT_DOMAIN` env; `cap_add: [NET_ADMIN, NET_RAW]`;
  `/dev/net/tun`; document host module + nfqueue/conntrack.
- entrypoint/startup — `awg-quick` auto-select; when `RESPONDER=true`, render the
  NFQUEUE + connmark iptables rules and launch the responder as a supervised side
  process alongside the UI under `dumb-init` (crash-isolated: on responder exit,
  tear down the rules so traffic falls through to `awg0`).
- `src/lib/WireGuard.js` — emit `ImitateProtocol` (above).
- `.env.example`, `README`, `justfile` — reflect the single-image model.

## Host requirements (documented)

- For the kernel datapath: the `amneziawg` module installed/loaded on the **host**
  (DKMS). Otherwise the image silently uses the go userspace fallback.
- For the responder: `nfnetlink_queue`, `nf_conntrack`, and the `CONNMARK`/`connmark`
  netfilter targets/matches (standard on mainstream distros). `CAP_NET_ADMIN`
  (nfqueue + iptables) **and** `CAP_NET_RAW` (reply injection) on the container.

## Testing (manual — no automated suite per project norms)

1. **Regression:** `IMITATE_PROTOCOL=none`, `RESPONDER=false` → behavior identical
   to current master (client connects on `WG_PORT`, UI on `PORT`, traffic flows).
2. **Datapath auto-select:** module loaded on host → `awg show` confirms the kernel
   interface; `rmmod` the host module + restart → the go fallback brings up the same
   tunnel from the same `wg0.conf`.
3. **Shaping:** `IMITATE_PROTOCOL=quic` → `tcpdump`/Wireshark on `WG_PORT` shows
   QUIC-shaped frames in **both** directions, no WireGuard/malformed markers.
4. **Responder (QUIC):** `IMITATE_PROTOCOL=quic`, `RESPONDER=true` →
   - an **unsupported-version** QUIC Initial → Version-Negotiation reply (GREASE,
     not v1);
   - a **well-formed v1** Initial (e.g. from `quic-go`/`curl --http3` or a probe
     tool) → the embedded endpoint completes a TLS-1.3 `ServerHello`/cert flight,
     i.e. the **multi-RTT handshake progresses** (verifies connmark flow-ownership);
   - a DNS / STUN probe is **ignored** (consistent with a QUIC-only server);
   - a **real client still connects** — its handshake is ACCEPTed (classify wins
     before probe-detection), not dropped. Re-test with `IMITATE_PROTOCOL=dns` (the
     adversarial case where shaped handshakes resemble the answered protocol).
5. **Responder (DNS/STUN/SIP):** with `IMITATE_PROTOCOL=dns` a DNS query probe gets a
   SERVFAIL echoing the question; with `=stun` a Binding-Request gets Binding-Success;
   with `=sip` confirm shaping occurs but probes are **not** answered (Decision 8) and
   the entrypoint logged the warning.
6. **Fast path:** `iperf` over an established tunnel — responder CPU stays ~0 during
   bulk transfer, confirming `ESTABLISHED` flows bypass userspace. Then confirm a
   mid-stream transport packet arriving after the conntrack idle-timeout (re-`NEW`)
   is still ACCEPTed (transport classify, Review F6).
7. **Crash isolation:** kill the responder mid-session → established tunnels keep
   flowing, new clients still connect (rules torn down), only active-probe defense
   is lost.

## Risks / open items for the plan

- **`imitate_protocol` plumbing — VERIFIED (was the top risk).** Confirmed
  end-to-end on both datapaths (see "WireGuard.js change"). Remaining work is a
  **smoke test**, not a verification unknown: load the kernel module on a host, set
  `ImitateProtocol=quic`, `awg showconf` round-trip, and `tcpdump` the wire to
  confirm shaping — and repeat on the go fallback.
- **QUIC handshake flow-claim — FIRST prototype milestone (Review R2-1).** This is
  the riskiest new mechanism and it is **broken as first drawn** (iptables
  `--save-mark`). The prototype's explicit goal is to prove the claim *persists* given
  the named failure mode: **DROP ⇒ the ct entry is never confirmed ⇒ no mark to
  restore; and the raw reply confirms the reverse tuple, flipping the prober's 2nd
  packet to `ESTABLISHED` → `awg0` stall.** The fix to validate: set the conntrack
  mark on the entry directly via the nfqueue verdict's CT facility (`NFQA_CT`) or
  `libnetfilter_conntrack` — not a save-mark rule. Fold in R2-2 (masked bit `0x1/0x1`,
  disjoint from `awg-quick`'s fwmark). Gate the rest of the QUIC-handshake work on
  this prototype passing. Also: a per-flow idle TTL evicts abandoned probe state and
  clears the mark.
- **Raw-egress correctness (v4 + v6):** hand-built IP/UDP headers + checksums, forged
  source port, and confirmation that injected `--sport WG_PORT` replies are not caught
  by any OUTPUT-side rule (no loop). Test on both address families.
- **NFQUEUE placement = INPUT (not PREROUTING):** traffic is locally terminated
  (`awg0` / go socket), so INPUT is correct. Low risk (Review note). For the go-TUN
  fallback the NEW/ESTABLISHED bookkeeping is the same (both traverse INPUT before
  local delivery); the only edge to test is the idle-expiry re-`NEW` transport case
  (Review F6, covered in Testing).
- **Privileged port binding** when `WG_PORT < 1024` — native AWG binds it; the current
  compose already does this. Confirm root + `CAP_NET_ADMIN` suffices, else add
  `CAP_NET_BIND_SERVICE`. Likely a non-issue.
- **Responder ↔ datapath protocol agreement:** both sourced from the one
  `IMITATE_PROTOCOL` env var so they cannot drift.
- **`quic-go` over a custom `net.PacketConn` — lower-risk than first billed (Review
  R2-3).** `quic.Transport{Conn: pc}` accepts a custom `net.PacketConn`: `ReadFrom`
  returns `(n, clientAddr)`, `WriteTo` routes to the raw injector, and
  `tls.Config.GetCertificate` is the standard SNI hook. quic-go logs a one-time
  warning when the conn isn't `OOBCapablePacketConn` (no ECN/GSO) and degrades
  cleanly — harmless here. **Pin the quic-go version**; its retransmit/idle timers
  actually reinforce keeping the flow claimed for the handshake's duration.
- **Self-signed cert is a residual (weaker) fingerprint (Review R2-6, inherited).**
  The full handshake defeats a *cheap* prober ("does it speak QUIC/TLS at all"), but a
  prober that validates the chain sees a self-signed cert for `QUIC_CERT_DOMAIN`.
  Inherited from the Rust original (`rcgen` self-signed); out of scope to fix —
  recorded as a known limit so it doesn't surprise later.
