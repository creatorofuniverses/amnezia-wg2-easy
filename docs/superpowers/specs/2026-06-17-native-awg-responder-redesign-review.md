# Review — Native AmneziaWG + probe-responder redesign

**Reviews:** `2026-06-17-native-awg-responder-redesign-design.md`
**Date:** 2026-06-17
**Reviewer:** Claude (grounded against the actual code in `amneziawg-go-proxy`,
`amneziawg-tools-proxy`, and the current `amnezia-wg2-easy` tree)
> **Round 2 (against a759fe2) added at the bottom of this file — read it for the
> current verdict.** Round 1 below is retained as history.

**Verdict:** **Approve the direction; do not start planning yet.** The core
move — native datapath + side-channel responder instead of an inline relay — is
right and well-motivated. But three load-bearing mechanisms are described at a
level that hides real work (how responses are *transmitted*, SIP's statefulness,
and the QUIC answer's strength), and one primary-datapath assumption is
unverified. These should be resolved in the design before `writing-plans`, or the
plan will inherit under-specified steps.

---

## What the review is grounded in

Unlike the spec's "verify during implementation" notes, several of these were
checked against source now:

- **Go UAPI parser** — `imitate_protocol` is real, device-level, lowercase, values
  `none|quic|dns|stun|sip` (`amneziawg-go-proxy/device/uapi.go:391`,
  `device/obf_imitate.go:31`). Round-trips through `IpcGet` (`uapi.go:129`).
- **conf→config bridge** — `amneziawg-tools-proxy/src/wg-quick/linux.bash`
  parses only a fixed set of `[Interface]` keys (Address/MTU/DNS/Table/Pre*/Post*/
  SaveConfig) and **passes every other line straight to `awg setconf`** (linux.bash
  ~L40–73, L258). So `imitate_protocol=quic` in `[Interface]` *does* reach the go
  backend — by pass-through, not by explicit support.
- **Current responder** — `amnezia-wg2-easy/proxy/src/responder.rs` +
  `proxy.rs`: it is a **UDP relay** today, classifies AWG first then detects
  protocol, QUIC/DNS/STUN are stateless, **SIP is a stateful per-client dialog with
  timed deferred responses**, and QUIC optionally completes a full TLS handshake.

The spec's prose matches reality on QUIC-GREASE, DNS-SERVFAIL-with-validation,
STUN-XOR-MAPPED-ADDRESS, and classify-before-detect. Those parts are accurate.

---

## Strengths (keep these)

- **Killing the inline relay is the correct call.** The motivation (don't funnel
  kernel-module throughput through a userspace hop) is sound, and "responder is the
  only surviving half" is the right scope cut.
- **classify-AWG-before-detect-protocol ordering** is faithful to the Rust code and
  correctly justified for the `IMITATE=dns`-shaped-handshake adversarial case
  (design §"Per-packet verdict" / §"Why the responder must read S1–S4").
- **NEW-only NFQUEUE so ESTABLISHED bypasses userspace** is the right shape for
  keeping bulk throughput in the kernel.
- **Single env var feeding both shaping and responder** (Risk bullet 4) genuinely
  prevents drift and is worth the explicit constraint.
- **Deleting the Rust crate** rather than keeping dead "reference" code is the right
  hygiene call given git preserves it.

---

## Findings

### F1 — [High] The response *transmission* path is undefined, and it collides with `WG_PORT` ownership

The verdict step "2. probe? → **send the crafted response, DROP**" hides the
hardest mechanical problem in the whole redesign.

In the old design the proxy *owned* `WG_PORT` (it was the listener; the backend was
elsewhere), so replying was just `socket.send_to(client)`. In **this** design
`awg0` owns `WG_PORT`. The responder is a side filter — it **cannot** bind a UDP
socket to `WG_PORT` to emit a reply, because the kernel module / go-socket already
holds it. NFQUEUE's verdict API only lets you ACCEPT/DROP/mangle the *queued*
packet; it cannot *originate* a reply datagram.

So the responder must inject packets with a **raw socket** (forging
source-port = `WG_PORT`, dest = observed client). That implies:

- `CAP_NET_RAW` (or `CAP_NET_ADMIN` covers raw on most setups, but state it), not
  just the `CAP_NET_ADMIN` the spec lists for nfqueue.
- Hand-building IP+UDP headers + checksums for v4 **and** v6.
- A decision about egress: a forged-source raw send may itself hit the NFQUEUE/
  conntrack rules — confirm the reply doesn't loop back into the queue.

This is real, non-trivial work that the one-line "send the crafted response"
elides. **Add a "Response egress" subsection** specifying the raw-socket injection
mechanism, the capability it needs, and the v4/v6 header construction, before
planning.

### F2 — [High] SIP is stateful; the "stateless 3-step verdict" framing will produce a detectably-wrong SIP responder

The design frames the responder as a per-packet `classify → detect → respond`
function. That is true for QUIC/DNS/STUN. It is **false for SIP**, which in the
current code (`responder.rs:680–1207`, `proxy.rs:857–1089`) is:

- a per-client dialog state machine (`Idle→Invited→Ringing→Established→Terminated`),
  stored in a `DashMap<SocketAddr, SipDialog>`, and
- **timed deferred responses**: `100 Trying` immediately, `180 Ringing` after
  ~200 ms, `200 OK` after ~1 s — scheduled as separate tasks that re-check dialog
  state before firing.

Two consequences:

1. The deferred sends are *unsolicited* follow-up datagrams — they depend on F1's
   raw-socket egress, **not** on any queued packet's verdict. A pure NFQUEUE-verdict
   responder cannot emit them at all.
2. A behavior port that collapses SIP into a single synchronous reply (no Ringing
   delay, no dialog) is *itself a fingerprint* — the opposite of the goal.

The good news the spec under-states: `go-nfqueue` runs the responder in
**userspace**, so holding a `map[client]*sipDialog` + `time.AfterFunc` goroutines
is entirely feasible — this is not a kernel filter. But that means the design's
"off the data-path stateless filter" mental model is wrong for SIP, and the plan
must explicitly include a per-client SIP state store + timer-driven egress.
**Either** scope SIP down to a documented-weaker single-response form (and say so),
**or** keep the dialog machine and budget for the state+timer port. Pick one in the
design.

### F3 — [High] QUIC = Version-Negotiation-only is weaker than what's being replaced

The current responder has **two** QUIC behaviors: VN by default *and* an optional
full TLS-1.3 handshake (`quic_handshake_enabled`, `quic_handshake.rs`) that decrypts
the Initial, parses the ClientHello, and emits a server Certificate flight.

The redesign keeps only VN. But a **Version-Negotiation packet is only a valid
answer to an *unsupported* version**. An active prober that sends a well-formed
QUIC v1 Initial (the obvious probe) expects a `ServerHello`/Retry, *not* VN — a
VN reply to a v1 Initial is arguably itself anomalous (and the code already refuses
to advertise v1 for exactly this reason). So VN-only answers the weakest probe and
stays silent (or odd) against the realistic one.

This may be an acceptable scope cut for #1, but it is a **regression in probe
resistance** that the spec presents as a like-for-like "behavior port." Call it out
explicitly: state that full QUIC-handshake continuation is dropped, why VN-only is
sufficient for the threat model you care about now, and that `quic_handshake` is
deferred (alongside the `qinit`/I-packet deferral you already note).

### F4 — [Med] The *primary* datapath's `imitate_protocol` support is the least-verified link

The spec's risk bullet "verify the config key against the parsers" is half-done by
this review:

- **go-userspace fallback**: verified end-to-end (wg-quick pass-through →
  `awg setconf` → UAPI → `device/uapi.go:391`). ✅
- **kernel module (the primary path!)**: **unverified.** `awg setconf` must
  translate `imitate_protocol` into a *netlink* attribute, and the host kernel
  module (`amneziawg-proxy-linux-kernel-module`, `src/imitate.c`) must accept that
  attribute. wg-quick pass-through only gets the line as far as `awg setconf`; it
  says nothing about whether the netlink/kernel side understands it.

So the ironic gap: the datapath the design promotes to *primary* is the one whose
shaping plumbing is unconfirmed, while the "just in case" fallback is the proven
one. The plan's verification step should explicitly test `imitate_protocol` on a
loaded kernel module (e.g. `awg showconf` round-trip + on-wire `tcpdump`), not just
the go path. Until then, treat kernel-path shaping as an assumption, not a fact.

(Secondary, lower: wg-quick passing the key through *by virtue of not recognizing
it* is fragile — if upstream tools ever add explicit `[Interface]` handling it could
change. Worth a one-line note; not a blocker.)

### F5 — [Med] Silently-dropped responder features should be enumerated

Beyond SIP (F2) and QUIC-handshake (F3), the current responder has behaviors the
redesign drops without listing them. For an honest scope:

- **`auto` protocol** — the old `PROXY_PROTOCOL` accepted `auto` (per-client
  protocol locking on first detection). `IMITATE_PROTOCOL` drops it. This is
  *correct* (sender shaping must pick one shape; answering many protocols is itself
  a fingerprint, which the design rightly argues), but it's an uncalled-out
  behavior change from today's `.env.example`.
- **DNS forward-to-upstream** (`PROXY_DNS_FORWARD`/`PROXY_DNS_UPSTREAM`) — gone.
  Synthetic SERVFAIL only. Probably fine; say so.
- **Per-client app-level rate limiting** — replaced by an iptables rate limit on the
  NFQUEUE rule (coarser, per-rule/per-source-hash vs per-client token bucket).
  Acceptable for flood protection; note the semantic change.

A short "Explicitly dropped from `responder.rs`" subsection would make the scope
defensible and stop these from resurfacing as "regressions" later.

### F6 — [Low] go-fallback conntrack interaction is lower-risk than the open item implies

The design flags (open item 2) that the go-TUN fallback "may interact with conntrack
differently." In practice both consumers — kernel `awg0` and the go UDP socket —
receive locally-terminated UDP, so both traverse the **INPUT** chain *before* local
delivery; the NEW/ESTABLISHED 5-tuple bookkeeping is the same for both. The genuine
edge to test is idle-expiry: after the UDP conntrack timeout a mid-stream transport
packet arrives as `NEW` and must be ACCEPTed by `classifyAwgPacket` matching the
**S4/H4 transport** shape (not just handshake) — the Rust classifier does try all
four S/H pairs, so the Go port must too. Keep the test, downgrade the worry.

---

## Smaller notes

- **INPUT vs PREROUTING** (open item): INPUT is correct here — traffic is
  locally terminated (awg0 / go socket), not forwarded. PREROUTING would be for a
  routing box. Low risk; the existing test plan covers it.
- **Privileged port** (open item): root + `CAP_NET_ADMIN` can bind <1024; the go
  fork already does this today in the current compose. Likely a non-issue; quick to
  confirm.
- **Responder requires `IMITATE_PROTOCOL != none`** (entrypoint hard-error) is a
  good guard — keep it.
- **Process supervision**: three things in one container (awg-quick, Node UI, Go
  responder) under dumb-init — specify what happens if the responder crashes (does
  the tunnel keep serving? it should — it's a side filter). One line in the
  entrypoint section.

---

## Recommended changes before `writing-plans`

1. **Add a "Response egress" subsection** (F1): raw-socket injection, the capability
   it needs (`CAP_NET_RAW`/`NET_ADMIN`), v4+v6 header build, and loop-back avoidance.
2. **Resolve the SIP model** (F2): keep the stateful dialog (state map + timers in
   the userspace responder) *or* explicitly scope SIP to a weaker single-shot reply.
   Decide and write it down.
3. **Reframe QUIC** (F3): state that VN-only is the chosen scope, that full handshake
   continuation is deferred, and acknowledge the probe-resistance trade-off.
4. **Move kernel-path `imitate_protocol` from "verify the key" to "verify the
   netlink/kernel-module path"** (F4) — it's the primary datapath and currently the
   weakest-verified link.
5. **Add an "Explicitly dropped from responder.rs" list** (F5): `auto`, DNS upstream
   forward, per-client rate-limit, QUIC handshake — each with a one-line rationale.

None of these change the architecture; they close gaps that would otherwise become
vague or wrong steps in the plan.

---

# Round 2 — re-review against `a759fe2`

**Date:** 2026-06-17
**Verdict:** **Ready for `superpowers:writing-plans`.** All six round-1 findings are
resolved — and F4 I re-verified against the actual source (citations are accurate).
The revision is materially stronger than round 1. One *new* mechanism introduced by
the F3 decision — the connmark flow-claim for the multi-RTT QUIC handshake — carries
a non-obvious correctness gap. It does **not** block planning, but it must be the
**first** thing the plan prototypes, and the precise failure mode below should be
named in that prototype's goal. Gate the rest of the QUIC-handshake work on it.

## Round-1 findings — all resolved

| # | Resolution in `a759fe2` | Assessment |
|---|---|---|
| F1 | "Response egress" section: `SOCK_RAW`/`IP_HDRINCL`, forged `sport=WG_PORT`, v4+v6, `CAP_NET_RAW`, no-loop reasoning | Correct and complete enough to plan against. |
| F2 | Decision 8 — defer SIP responder, keep SIP shaping; entrypoint warns on `sip`+`RESPONDER=true` | Clean. See minor note R2-5. |
| F3 | Decision 9 — full TLS-1.3 handshake via `quic-go`, VN retained for unsupported-version probes | Owns the consequence (connmark) instead of hand-waving it. Good. |
| F4 | Recast from assumption to **verified**, with citations | **Independently re-verified — see below.** |
| F5 | "Explicitly dropped from responder.rs" section (`auto`, DNS-forward, per-client rate-limit, VN-only) | Present and honest. |
| F6 | Verdict step 1 now classifies **transport** (S4/H4), not just handshakes; Testing step 6 covers the re-`NEW` idle case | Correct. |

### F4 independently re-verified

I checked every citation against the checked-out repos; all are accurate:

- `amneziawg-tools-proxy/src/config.c:600` — `key_match("ImitateProtocol")` (conf
  key) ✓; `:912` — `imitate_protocol` setconf arg ✓.
- `ipc-linux.h:227–228` — `mnl_attr_put_strz(..., WGDEVICE_A_IMITATE_PROTOCOL, ...)` ✓.
- kernel module `netlink.c:62` (policy `NLA_NUL_STRING`), `:886–893` (parse → 
  `wg->imitate_proto`), and **`:467–469`** (emits it back in showconf) ✓;
  `imitate.c:437–448` (applies it on the fill path) ✓.

So "reaches both backends" is true, and the `awg showconf` round-trip the smoke test
relies on is real (netlink.c:467). The downgrade from "top risk" to "smoke test" is
justified.

## New findings (all on the connmark mechanism from Decision 9)

### R2-1 — [High, prototype-blocking] A DROPped probe packet cannot persist a connmark; the claim mechanism as drawn won't work

This is the one thing to resolve before committing the QUIC-handshake path. The rule
sketch shows `CONNMARK --restore-mark` but the matching `--save-mark` only in prose —
and the QUIC verdict is *"set mark `0x1` … **DROP**."* That combination has three
problems that compound:

1. **DROP ends chain traversal**, so a later `--save-mark` rule in INPUT never sees
   the packet — the mark is set on a packet that's about to be discarded.
2. **A dropped packet's conntrack entry is never *confirmed*.** Confirmation happens
   at the end of `LOCAL_IN`, *after* the NFQUEUE verdict; a DROP short-circuits it, so
   the entry is freed and there is no conntrack mark to restore later.
3. **The raw-socket reply is what conntrack actually sees complete.** It leaves via
   OUTPUT with `sport=WG_PORT`, so conntrack tends to create/confirm the flow in the
   *reverse-as-original* direction — which is precisely what flips the prober's 2nd
   packet to `ESTABLISHED` and routes it to `awg0`. That is the exact stall the
   connmark was meant to prevent.

So the instinct is right (the flow *will* otherwise establish and stall — the
connmark is genuinely needed), but the persistence path is unsolved. Viable options
for the prototype:

- **(i)** set the *conntrack* mark directly through the nfqueue verdict's CT facility
  (`go-nfqueue` NFQA_CT / connmark on verdict) rather than via an iptables
  `--save-mark` rule — bypasses the DROP-vs-save ordering entirely; **or**
- **(ii)** have the responder program the conntrack entry for the claimed 5-tuple via
  `libnetfilter_conntrack` when it decides to claim.

**Action:** make this the first prototype milestone, and state the failure mode (DROP
⇒ no confirm ⇒ no mark; reply confirms the reverse tuple) explicitly in its goal. The
spec's "prototype it early" currently under-specifies *what* could go wrong.

### R2-2 — [Med] Mark must be masked and checked against the datapath fwmark

`awg-quick` sets an fwmark for policy routing when `Table` is in play. Rule 1
(`--restore-mark`) runs **unconditionally** on every `WG_PORT` packet, and a bare
`--set-mark 0x1` / full-word restore can clobber or collide with that fwmark on the
bulk path. Use a dedicated bit with masks (`--set-xmark 0x1/0x1`,
`--restore-mark --nfmask 0x1 --ctmask 0x1`) and confirm the chosen bit is disjoint
from whatever fwmark `awg-quick`/`wg0.conf` uses. Fold this into the R2-1 prototype.

### R2-3 — [Low] `quic-go` over a custom `PacketConn` is feasible — pin it, expect graceful degradation

The spec's "main unknown" is lower-risk than billed. `quic.Transport{Conn: pc}`
accepts a custom `net.PacketConn`; `ReadFrom` returns `(n, clientAddr)`, `WriteTo`
routes to the raw injector, and `tls.Config.GetCertificate` is the standard SNI hook.
quic-go logs a one-time warning when the conn isn't an `OOBCapablePacketConn`
(no ECN/GSO) and falls back cleanly — harmless here. Pin the quic-go version; its
internal retransmit/idle timers are fine and in fact reinforce keeping the flow
claimed for the handshake's duration.

### R2-4 — [Low] Egress detail: IPv6 UDP checksum is mandatory

`IP_HDRINCL` is v4-only; the v6 path differs (no `IP_HDRINCL` — either `IPV6_HDRINCL`
on newer kernels or let the kernel build the header). Whichever way, the **IPv6 UDP
checksum is mandatory** (it's optional in v4), computed over the v6 pseudo-header.
Make sure the hand-built v6 path does it, or v6 replies silently drop.

### R2-5 — [Low] `sip` + `RESPONDER=true` is the weakest combination — note it

With SIP shaping on but no SIP responder (Decision 8), a SIP probe is silently dropped
by `awg0` — which is the "silence is a fingerprint" problem the responder exists to
solve. So `IMITATE_PROTOCOL=sip` is strictly the least-protected setting when probing
is a concern. The entrypoint warning already surfaces this; just make sure the README
frames it as "SIP = shaping only, no active-probe defense yet," not as a peer of the
other three.

### R2-6 — [Low, inherited] Self-signed cert is itself a (weaker) fingerprint

The full handshake defeats a *cheap* prober ("does it speak QUIC/TLS at all"), but a
prober that validates the chain sees a self-signed cert for `QUIC_CERT_DOMAIN`. This
is inherited from the Rust original (rcgen self-signed) and out of scope to fix — one
line in the design noting it as a known limit keeps it from reading as a surprise
later.

## Bottom line

Plan it. Sequence the QUIC-handshake work behind a connmark prototype whose explicit
goal is R2-1 (with R2-2 folded in); everything else is low-risk or already correct.
The non-QUIC scope (DNS/STUN single-shot, classify-first, raw egress, the
`ImitateProtocol` wiring, datapath auto-select) is ready to plan as-is.
