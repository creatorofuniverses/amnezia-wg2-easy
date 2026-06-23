# Roadmap: fork hardening / feature / bug notes (triage & split)

Status: **scoping doc.** Created 2026-06-23 to triage the four items surfaced in
commit `cf1412d` ("docs: add fork hardening/feature/bug notes") plus the
legacy-I1 toil item. This file is the *split & sequencing*; each round gets its
own plan/spec under `docs/superpowers/`.

## The four items

| # | Item | Source doc | Type | Effort | Workaround exists? |
|---|------|-----------|------|--------|--------------------|
| 1 | Server MTU not applied to live wg0 | `docs/server-mtu-not-applied.md` | Confirmed bug | Tiny | No — real breakage |
| 2 | Legacy/compat I1 default | (this roadmap; new) | Small feature | Tiny | Manual paste each deploy |
| 3 | Custom AllowedIPs / site-relay peers | `docs/custom-allowedips-site-peer.md` | Feature | Medium | Yes — `awg-peers.service` + manual iptables |
| 4 | Responder hardening (P1–P5) | `responder/HARDENING.md` | Investigation + hardening | Large / uncertain | Yes — `RESPONDER=false` |

## Rounds

### Round 1 — quick wins (✅ DONE 2026-06-23)
**Goal:** ship the two tiny, high-friction items.

- **#1 MTU bug.** Add `mtu` (and likely `address` — same interface-level class) to
  `RESTART_FIELDS` in `serverSettings.js:99` so an MTU change yields
  `needsRestart: true` and a full `wg-quick down/up` re-applies the interface MTU.
  (Alt no-bounce path: also run `ip link set dev wg0 mtu <n>` live.) Add a
  `classify()` test asserting `mtu`/`address` force restart.
  Acceptance: after a UI MTU change, `ip link show wg0` reflects the new MTU.
- **#2 legacy I1.** New env flag (decided) — `IMITATION_COMPAT` (name TBD) that,
  when set and `I1` is empty, injects a baked-in legacy I1 value into the seeded
  server config. Does **not** change current `I1` defaults (stays `null` when the
  flag is off). Surfaces through the same seed path as `I1`
  (`WireGuard.js:114` → `serverSettings.seedServerDefaults`).
  - **Baked-in value (resolved):** `<r 2><b 0x858000010001000000000669636c6f756403636f6d0000010001c00c000100010000105a00044d583737>`
    — a DNS-shaped CPS signature (iCloud.com query/response, repeated). Lives as
    `LEGACY_I1_VALUE` in `config.js`.

**Shipped:** `mtu` added to `RESTART_FIELDS` + `classify()` test (`serverSettings.js`,
`serverSettings.test.js`); `I1_COMPAT` flag in `config.js`; env docs in
`CLAUDE.md` + `README.md`. 31 tests pass, lint clean.

→ Detailed plan: `docs/superpowers/plans/2026-06-23-round1-mtu-and-legacy-i1.md`

### Round 2 — feature (later)
**#3 custom AllowedIPs / site-relay peers.** Doc already scopes it well:
- Data model: optional per-client `allowedIps` in `wg0.json` (vestigial var at
  `WireGuard.js:323`).
- Render: override at `configRender.js:64`, else fall back to `${client.address}/32`.
- Route: **free** — `wg-quick`/`Table=auto` adds it; no route code.
- Real work: **MASQUERADE** for sources outside `server.defaultAddress/24`, and
  **overlap validation** (non-unique AllowedIPs silently steal return traffic).
- Gate behind an "advanced / site peer" flag so simple-client UX is unchanged.

Not urgent — workaround exists. Write a full plan/spec when picked up.

### Round 3 — investigation, not a fix (later)
**#4 responder hardening.** The doc's own conclusion: **causality is unknown /
likely reversed** — responder is isolated by design and *should not* be able to
flap the tunnel. So this is an **investigation first**, not a code change:
- **P1 (blocking): pin causality** with timestamped logging on both sides — did
  Node's `wg-quick down` fire *before* the `netlink i/o timeout` or after? Find
  why Node cycles the tunnel (`lib/WireGuard.js`).
- Only after P1: P2 (re-open queue vs `log.Fatal`), P3 (rcvbuf / `MaxQueueLen`),
  P4 (isolation-invariant test), P5 (silence benign quic-go rcvbuf warning).
- Workaround holds: `RESPONDER=false` keeps passive imitation; loses active-probe
  answering only.

**Cross-link to watch:** the MTU mismatch in #1 and the tunnel flap in #4 come from
the *same* field report (triple-awg-xray RU-exit). The MTU mismatch is a candidate
explanation for *why Node bounced the tunnel* in #4 — fixing #1 may inform P1.

## Decisions (locked)
- I1 legacy mode: **new env flag**, off by default, does not alter existing I1 defaults.
- Implement now: **Round 1 only** (this roadmap + the Round 1 plan).
