# Design: custom AllowedIPs / site-relay peers

Status: **approved design, not built — rev 2 (review-integrated).** Created
2026-06-23. Round 2 of the fork-notes roadmap
(`docs/superpowers/plans/2026-06-23-fork-notes-roadmap.md`). Supersedes the problem
framing in `docs/custom-allowedips-site-peer.md` (kept as the original field
report); this spec is the buildable design.

**Rev 2** integrates the code review at
`docs/superpowers/specs/2026-06-23-custom-allowedips-site-peer-design.review.md`:
B1 (masq leak on non-bounce lifecycle paths), B2 (overlap enforced everywhere, not
just the site-peer route), S1 (import the existing CIDR validators; resolve v6),
S2 (replace-semantics decided + UI help text). All four were verified against
source before integration.

## Goal

Let a client peer be given **custom AllowedIPs** (a foreign subnet, or specific
CIDRs) instead of only its single `/32` in the server subnet, so relay /
site-to-site topologies (an entry node tunneling to this box as an exit, a peer
routing a LAN) work **from the web UI** without a separate `awg-peers.service` +
hand-written `iptables`/`ip route`.

## Primary use case

The triple-awg-xray RU-exit migration: an entry node is a peer of this exit; its
client subnet (e.g. `10.20.0.0/24`) must be carried as that peer's AllowedIPs and
masqueraded for internet egress. Aligning a peer *into* the server's own subnet (a
normal `/32`) already works today via `updateClientAddress` — this feature is for
**genuine foreign-subnet / subnet-carrying peers**.

## Decisions (locked during brainstorming)

- **Scope:** full feature in one pass — data model + render + MASQUERADE + overlap
  validation + web UI.
- **Masquerade:** explicit **per-client toggle** (`siteMasquerade`), default
  `false`. A LAN-only peer that shouldn't egress stays un-NATed; an exit-relay peer
  opts in.
- **Apply path:** **bounce** (`wg-quick down/up` with rollback), because both the
  `Table=auto` route and the PostUp masq rule only materialize at `wg-quick up`.
  Normal client CRUD stays on the no-bounce `wg syncconf` path; only site-peer
  changes bounce.
- **"Site peer" is defined by data, not a flag:** the *presence* of a non-empty
  `allowedIPs` makes a client a site peer. No separate boolean.
- **Overlap = hard reject** (HTTP 400), not warn. Overlapping AllowedIPs is never
  valid within one interface — `wg` silently reassigns the IP to the last peer
  (the exact bug that bit the RU-exit migration: three entry peers all claiming
  `10.20.0.0/24`, only one worked).
- **Masq rules render into PostUp/PostDown** (not a separate runtime iptables
  call), consistent with the existing hook model.
- **Site-peer lifecycle is bounce-aware (B1):** because masq rules live in
  PostUp/PostDown and only run on `wg-quick up`/`down`, *every* path that adds or
  removes a site peer from the live config must bounce — not just
  `setClientSitePeer`, but also enable/disable/delete of a site peer. Otherwise a
  disabled/deleted site peer's MASQUERADE rule orphans in the kernel (syncconf
  never runs PostDown, and the re-rendered conf no longer contains the matching
  `-D`). Normal clients keep the fast `syncconf` path.
- **Overlap is enforced on every mutation (B2)**, not only the site-peer route:
  `updateClientAddress` and `createClient`'s auto-assigned `/32` must pass the same
  cross-peer overlap check. Otherwise a normal client's `/32` can be pointed inside
  a site peer's subnet, re-creating the silent cryptokey-reassignment bug.
- **Reuse the existing CIDR validators (S1):** `isValidCIDR` / `isValidCIDRList`
  already exist and are exported from `serverSettings.js` (v4+v6 via `node:net`).
  Import them; the only new validation logic is **overlap**. Frontend reuses the
  existing `svIsCIDR` helper + `fieldErr()` plumbing + the `index.html` AllowedIPs
  input/error pattern.
- **Overlap is computed for v4 AND v6 uniformly (S1):** via integer (BigInt) network
  ranges — no v6 hole. (`isValidCIDR` accepts v6 as well-formed, so a v4-only
  overlap check would silently under-enforce.)
- **AllowedIPs render is REPLACE, not merge (S2):** the field is the peer's
  authoritative AllowedIPs list (WireGuard semantics). The peer's own `/32` is
  **not** auto-merged; the UI help text says "this replaces the peer's /32 —
  include it in the list if you still need it." Chosen over merge because merge
  self-overlaps the common case (tunnel IP inside the carried subnet) and the
  audience is advanced (gated behind the expander).
- **UI:** an "Advanced / site peer" expander in the client row, collapsed by
  default so the simple-client UX is unchanged.

## Architecture

### 1. Data model (`wg0.json`, per client)

Two new optional fields on a client object:

- `allowedIPs: string | null` — comma-separated CIDRs (e.g. `"10.20.0.0/24"` or
  `"10.20.0.0/24, 192.168.50.0/24"`). Empty/absent → normal client.
- `siteMasquerade: boolean` — default `false`; only meaningful when `allowedIPs`
  is set.

**No migration:** both fields absent on existing clients reads as
"normal client" (`allowedIPs` null, `siteMasquerade` false). `createClient` seeds
`allowedIPs: null, siteMasquerade: false` for new clients (alongside the existing
`legacy: false`). `getClients()` already returns `allowedIPs: client.allowedIPs`
(`WireGuard.js:302`) — extend that map to also return `siteMasquerade`.

### 2. Config render (`configRender.js`)

- **Server conf** (`:64`): replace the hardcoded
  `AllowedIPs = ${client.address}/32` with
  `AllowedIPs = ${client.allowedIPs && client.allowedIPs.trim() ? client.allowedIPs : `${client.address}/32`}`.
  This is **replace** semantics (S2): when set, `allowedIPs` is the peer's full
  list and its own `/32` is not auto-included. The `Table=auto` route is added
  automatically at `wg-quick up`.
- **PostUp** (`defaultPostUp`): after the existing default-subnet masq rule, append
  one `iptables -t nat -A POSTROUTING -s <cidr> -o <device> -j MASQUERADE` per CIDR,
  for every **enabled** client with `siteMasquerade === true`. **PostDown**
  (`defaultPostDown`) mirrors each with `-D`. To do this, `defaultPostUp`/
  `defaultPostDown` must receive the **clients** map (today they take only
  `server, device`) — change their signatures and the `renderDefaultHooks` call
  site to pass clients.

  *Note on env-overridden hooks:* `renderDefaultHooks` uses `pick(env.postUp,
  default…)` — if the operator set `WG_POST_UP`, the default (and therefore the
  site-masq rules) is **not** rendered. Document this: site-peer masquerade requires
  the default hooks (no `WG_POST_UP`/`WG_POST_DOWN` override). Acceptable — custom
  hooks are an expert escape hatch and out of scope here.

### 3. Validation (new `src/lib/clientValidation.js`, pure module)

Depends on `serverSettings.js`'s exported `isValidCIDR` / `isValidCIDRList` (no new
format code, no duplication — S1). Adds only the genuinely new logic:

- `parseAllowedIPs(str): string[]` — split on comma, trim, drop empties.
- Format: each CIDR passes the imported `isValidCIDR` (v4+v6).
- `cidrRange(cidr): { lo: BigInt, hi: BigInt, v: 4|6 }` — network range as integers
  (v4 and v6 both via `BigInt`, so overlap is computed uniformly — no v6 hole).
- `overlaps(a, b)` — same family and `a.lo <= b.hi && b.lo <= a.hi`.
- **No overlap** across the effective AllowedIPs of **all** peers (a normal client's
  effective set is its `${address}/32`; a site peer's is its parsed list). The
  checker takes the **candidate** peer's set and **all other** peers' effective sets
  and rejects on any cross-peer overlap. (Within-peer overlap is not policed — a
  single peer may legitimately list nested ranges; low value to reject.)
- Returns a field-keyed errors object like `validateServerSettings`; callers throw
  400 on non-empty.

This module is the **single overlap authority** used by `setClientSitePeer`,
`updateClientAddress`, and `createClient` (B2).

### 4. Apply path (`WireGuard.js`)

**Shared apply helper (B1).** Factor the two reload strategies into one place:

- `__isSitePeer(client)` → `!!(client.allowedIPs && client.allowedIPs.trim())`.
- `__reload({ bounce })` → if `bounce`, do `wg-quick down wg0` (on-disk conf) →
  `saveConfig`-equivalent write → `wg-quick up wg0`; else `__saveConfig` +
  `__syncConfig` (today's fast path). Rollback-on-failure is the caller's
  responsibility (it holds the prev state), mirroring `updateServerSettings`.

**`setClientSitePeer({ clientId, allowedIPs, siteMasquerade })`** (new):
1. Resolve client (own-property guard, like `getClient`).
2. Normalize: empty/whitespace `allowedIPs` → `null` and force
   `siteMasquerade: false` (masq only meaningful with a subnet).
3. Validate the candidate set via `clientValidation` against **all other** peers'
   effective AllowedIPs. Throw 400 with `errors` on overlap/format failure.
4. Assign + **bounce with rollback** (`__reload({ bounce: true })`); on failure
   restore prev client state, `__reload({ bounce: true })` again, throw 500.
5. Return `{ client, mustReimport: false }` (server-side only; the entry node is
   configured out-of-band, so no client-facing reimport signal — confirm in plan).

**Bounce-aware lifecycle (B1).** `enableClient`, `disableClient`, `deleteClient`
must bounce when the affected client **is or was** a site peer (its `allowedIPs` is
non-empty), so its masq rule is added/removed by PostUp/PostDown rather than
orphaned. Implementation: compute `bounce = __isSitePeer(client)` before the
mutation, then `__reload({ bounce })`. Normal clients → `bounce: false`, unchanged
fast path.

**Overlap on address change (B2).** `updateClientAddress` runs the same
`clientValidation` overlap check (effective set = `${address}/32`) against all
other peers before assigning; rejects 400 on overlap. `createClient`'s
auto-assigned `/32` is likewise checked against existing site-peer CIDRs (it
already scans the server `/24` for a free host; extend that to also skip addresses
that fall inside a site peer's subnet).

`updateClientName` and the legacy toggle are untouched (no routing/masq impact).

### 5. Routes (`Server.js`)

One route, `POST /api/wireguard/client/:clientId/allowedips`, with the existing
prototype-pollution param guard and session auth. Body: `{ allowedIPs, siteMasquerade }`.
Calls `setClientSitePeer`. On thrown `{ statusCode, errors }`, return that status +
errors (same shape the server-settings route already uses).

### 6. Web UI (`index.html`, `app.js`, `api.js`, `i18n.js`)

- An **"Advanced / site peer"** expander per client row, collapsed by default
  (simple-client UX untouched). Reveals:
  - AllowedIPs text input (placeholder `10.20.0.0/24, …`), comma-separated CIDRs,
    with **help text**: "Replaces the peer's /32 — include it in the list if you
    still need it." (S2). Reuse the existing `svIsCIDR` client-side check + the
    `index.html` AllowedIPs input + `fieldErr()` inline-error pattern already used
    for the server-settings AllowedIPs field (S1) — minimal new plumbing.
  - "Masquerade this peer's traffic" checkbox (disabled/ignored when AllowedIPs is
    empty).
  - A Save button for the advanced fields → `api.setClientSitePeer(...)`.
- On 400, surface the field error inline via the same `fieldErr()` plumbing.
- A small **chip/icon marker** on rows that are site peers (AllowedIPs non-empty),
  so they're visually distinct from normal clients.
- `i18n.js`: keys for the expander label, the masq checkbox, the placeholder, the
  help text, and the overlap/format error messages (en first; other locales fall back).

## Components & boundaries

- `clientValidation.js` — **pure**, no I/O: format (imported validators) + overlap
  (BigInt ranges). The single overlap authority. Independently testable.
- `configRender.js` — **pure** string render; gains client-aware masq lines.
- `WireGuard.js#__reload({ bounce })` — the one place that chooses syncconf vs
  down/up; consumed by `setClientSitePeer` and the bounce-aware lifecycle methods.
- `WireGuard.js#setClientSitePeer` — orchestration + bounce/rollback (I/O).
- `Server.js` route — HTTP glue.
- UI — presentation; one new API call.

## Testing (`node:test`, per app convention)

- `clientValidation`: well-formed/malformed CIDR; single vs multi CIDR; overlap
  detection (exact dup, containment, partial intersection, `/32` vs subnet, no-overlap
  pass); **v6 CIDR accepted AND v6 overlap actually computed** (S1 — assert, don't
  assume); empty → normal.
- `configRender`: server-conf AllowedIPs override present vs fallback `/32` (replace,
  not merge — S2); PostUp emits one masq rule per CIDR only for `siteMasquerade`
  clients, none otherwise; PostDown mirrors; disabled clients excluded.
- **B1 — lifecycle bounce:** disabling/deleting a site peer takes the bounce path
  (assert `__isSitePeer`-driven `bounce: true`), so its masq rule is removed via
  PostDown rather than orphaned; a normal client stays `bounce: false`.
- **B2 — overlap on address change:** `updateClientAddress` rejects an address that
  lands inside an existing site peer's subnet; `createClient` skips such addresses.
- Bounce/rollback, route materialization, and UI: manual verification (app
  convention), with an acceptance walk-through (below).

## Acceptance

A peer can be given custom AllowedIPs + masq toggle in the web UI → written to
`wg0.json`/`wg0.conf` → tunnel bounces → `ip route` shows the subnet route on `wg0`
→ `iptables -t nat -L POSTROUTING` shows the peer's masq rule (only if toggled) →
relay/site traffic works **without** `awg-peers.service` or hand-written iptables/
ip-route. Editing or clearing it in the UI re-applies (bounce) and removes the
route+rule. Overlapping AllowedIPs are rejected with an inline error.

## Out of scope

- No-bounce live delta apply (rejected; bounce chosen).
- Custom `WG_POST_UP`/`WG_POST_DOWN` override + site masq (mutually exclusive;
  documented).
- Per-peer DNS / routes / keepalive overrides — only AllowedIPs + masq.
- Styled QR / share-string changes for site peers (the entry node is configured
  out-of-band).
