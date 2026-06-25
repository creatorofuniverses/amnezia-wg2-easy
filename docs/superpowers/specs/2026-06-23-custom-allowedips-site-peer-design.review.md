# Review — custom AllowedIPs / site-relay peers design

Reviewer: Claude (Opus 4.8), 2026-06-23. Verified every "how the code works today" claim
against the actual source (`src/lib/WireGuard.js`, `src/lib/configRender.js`,
`src/lib/serverSettings.js`, `src/lib/Server.js`, `src/lib/Util.js`, `src/www/js/{app,api,i18n}.js`).
Verdict first, then findings ordered by severity.

## Verdict

**Right architecture, buildable — but not mergeable as written.** The core calls are correct:
"site peer = presence of `allowedIPs`" (no redundant flag), per-client `siteMasquerade` toggle,
hard-reject on overlap, masq rendered into `PostUp`/`PostDown`, and — importantly — the
apply path bounces in the **right order** (`wg-quick down` → save → `wg-quick up`), which
sidesteps the write-before-down iptables leak that the server-settings review caught in its
round (B1 there). The spec's reading of the existing code is ~95% accurate; line refs check out.

But there is **one real correctness gap** the design doesn't acknowledge (masq rules leak when a
site peer leaves through a *non-bounce* path), **one half-enforced invariant** (overlap is only
checked on the site-peer route, not on `updateClientAddress`/`createClient`), and **one open
question the spec leaves to the plan that is already answered by the codebase** (the CIDR
validators it wants to "extract or duplicate" already exist and are exported). Resolve B1 + B2,
delete the S1 indecision, and this is good to implement.

---

## Blocking

### B1. Site-peer masq rules leak when a site peer leaves via a no-bounce path

§4 routes **only** `setClientSitePeer` through the bounce, and §4's last paragraph explicitly
keeps `enable/disable`, `delete`, name, and legacy on the `saveConfig → wg syncconf` path. But
`wg syncconf` **does not run `PostUp`/`PostDown`** — only `wg-quick up`/`down` do. So:

- Operator disables (or deletes) a site peer via the normal route → `__saveConfig` re-renders
  `wg0.conf` (the peer's `[Peer]` block and its masq lines are now gone, because render only
  emits for **enabled** `siteMasquerade` clients) → `wg syncconf` drops the route. **But the live
  `iptables … -j MASQUERADE` rule added at the last `wg-quick up` is never removed** — no
  `down` ran, and the on-disk `PostDown` no longer contains the matching `-D`, so even a *future*
  legitimate bounce won't clean it. The rule leaks until container restart.
- Re-enabling that peer later (normal enable route, no bounce) does **not** re-add its masq rule
  or route; and if a bounce *does* happen afterward, you now get a **duplicate** rule.

This is the same class of bug as the prior round's B1, just reached from the other side: the
design correctly stopped writing-before-down, but it didn't make the *enable/disable/delete*
lifecycle of a site peer bounce-aware.

**Fix:** make `disableClient`, `enableClient`, and `deleteClient` bounce-aware — if the target
client *is or was* a site peer (`allowedIPs` non-empty), take the same down→save→up path with
rollback instead of `saveConfig → syncconf`. Normal clients stay on the fast path untouched.
Add a test that asserts a disabled/deleted site peer's masq rule is gone (or that the path
bounced).

### B2. Overlap invariant is only enforced on the site-peer route

§3/§4 run the no-overlap check inside `setClientSitePeer`. But `updateClientAddress`
(`WireGuard.js:476`) accepts an **arbitrary** IPv4 (`Util.isValidIPv4`, no overlap check) and
`createClient` auto-assigns a `/32`. So after a site peer claims `10.20.0.0/24`, an operator can
point a *normal* client's `/32` at `10.20.0.5` with zero validation — silently re-introducing the
exact cryptokey-routing reassignment bug this feature exists to prevent (§ "Decisions": "the exact
bug that bit the RU-exit migration").

**Fix:** run the same effective-AllowedIPs overlap check in `updateClientAddress` (a normal
client's effective set is its `${address}/32`). `createClient` auto-assign stays inside the server
`/24` so its risk is lower, but the address it picks should also be checked against site-peer CIDRs
if any site peer's subnet overlaps the server subnet. At minimum, if you defer this, **document the
asymmetry** so it's a known gap, not a silent one.

---

## Should-fix

### S1. The CIDR validators the spec wants to "extract or duplicate" already exist and are exported

§3 spends a decision on: *"reuse the `isValidCIDR` logic (extract a shared helper or duplicate the
small function; prefer a shared `src/lib/netValidation.js` if clean, else duplicate — decide in the
plan)."* This is a non-question. `src/lib/serverSettings.js` already implements **and exports**
`isValidCIDR` and `isValidCIDRList` (`:15-30`, exported `:126-127`), built on `node:net`, handling
both v4 and v6. Just `require` them — no new module, no duplication, no plan-time decision. The
new `clientValidation.js` should depend on these and add only the **overlap** logic (the genuinely
new part). Removing this open question also tightens §3's IPv6 note: `isValidCIDR` already accepts
v6 as well-formed, so the "accept v6 but maybe skip overlap" decision must be made **explicit and
logged**, or you ship a validator that *looks* like it covers v6 but doesn't.

Bonus: the frontend already has the mirror — `app.js:60` validates the server-settings AllowedIPs
field with a `svIsCIDR` helper, and `index.html:867-869` renders an AllowedIPs input + inline
error. Reuse that exact pattern (and the `fieldErr()` plumbing) for the per-client field; it cuts
the §6 UI cost substantially.

### S2. AllowedIPs render is full-replace — the peer silently loses its own `/32`

§2/§4 replace `${client.address}/32` **entirely** with the override. For the primary use case (an
entry node whose tunnel address lives inside the carried `10.20.0.0/24`) that's fine. But in the
general case the peer's own tunnel `/32` is now **absent** from its AllowedIPs, so if the exit ever
needs to originate a packet to the entry's tunnel IP and that IP is *not* inside the carried subnet,
there's no cryptokey route to it. Decide this explicitly rather than by omission:

- **(a) Merge:** render `AllowedIPs = ${client.address}/32, ${allowedIPs}` — safe default, the peer
  keeps reachability to its own tunnel IP. Overlap check must then treat the `/32` as part of the
  peer's own set (so it doesn't self-collide).
- **(b) Keep replace** but make the UI placeholder/help text say *"this replaces the peer's /32 —
  include it in the list if you still need it."*

The Goal says "instead of only its single `/32`", which reads like (b) was intended — but the spec
never says the operator must re-add the `/32`, so today it's a silent footgun. Pick one and write
it down.

---

## Nits & confirmations (accuracy is good — recording what I verified)

- **N1 — apply ordering is correct (credit).** §4 does `wg-quick down` (on-disk conf) → assign +
  `saveConfig` → `wg-quick up`, mirroring the fixed `updateServerSettings` restart branch
  (`WireGuard.js:242-259`). This is the right order and avoids the write-before-down leak. Good.
- **N2 — line refs verified.** `getClients` already maps `allowedIPs: client.allowedIPs`
  (`:302`) ✓; `createClient` seeds `legacy: false` (`:421`) — add `allowedIPs: null,
  siteMasquerade: false` alongside ✓; `configRender.js:64` hardcoded `AllowedIPs = ${client.address}/32`
  ✓; `defaultPostUp(server, device)` / `defaultPostDown(server, device)` signatures ✓;
  `renderDefaultHooks` uses `pick(env.postUp, …)` so a `WG_POST_UP` override suppresses the default
  *and* the site-masq lines — §2's caveat is accurate ✓.
- **N3 — drop the "vestigial `allowedIps` var" lead.** The original field note pointed at
  `WireGuard.js:323` as a starting point; that var is just the `wg show wg0 dump` column parse
  (`allowedIps, // eslint-disable-line no-unused-vars`), unrelated to the data model. The spec
  correctly doesn't rely on it — confirming it's a red herring so the plan doesn't chase it.
- **N4 — route + error shape accurate.** Mutating client routes guard `__proto__`/`constructor`/
  `prototype` on `clientId` (`Server.js:182-229`); the `/api/server-settings` route maps a thrown
  `{ statusCode: 400, errors }` to `createError({ status: 400, data: { errors } })` (`:248-249`).
  The new `POST …/allowedips` route should mirror both exactly.
- **N5 — masq `-s <cidr>` semantics are right** for the RU-exit relay (egress traffic *sourced
  from* the carried subnet). Per-CIDR masq granularity (e.g. one AllowedIPs entry you *don't* want
  NAT'd) is correctly out of scope — the coarse per-peer toggle covers the real case.

## Test additions beyond §"Testing"

- **B1 coverage:** disabling/deleting a site peer removes (or bounces to remove) its masq rule —
  not just "render excludes disabled clients."
- **B2 coverage:** `updateClientAddress` rejects an address that lands inside a site peer's subnet.
- **S1 coverage:** v6 CIDR is accepted as well-formed; assert explicitly whether v6 overlap is
  checked or logged-as-skipped (no silent gap).

## Bottom line

Ship-able design with a correct apply path. Before building: (1) make site-peer
**disable/enable/delete** bounce-aware (B1), (2) enforce overlap on `updateClientAddress` or
document the gap (B2), (3) just import the existing `isValidCIDR`/`isValidCIDRList` and decide the
v6-overlap question now (S1), (4) pick merge-vs-replace for the rendered `/32` and write it down
(S2). The rest is accurate.
