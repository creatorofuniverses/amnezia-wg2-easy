# Design: custom AllowedIPs / site-relay peers

Status: **approved design, not built.** Created 2026-06-23. Round 2 of the fork-notes
roadmap (`docs/superpowers/plans/2026-06-23-fork-notes-roadmap.md`). Supersedes the
problem framing in `docs/custom-allowedips-site-peer.md` (kept as the original field
report); this spec is the buildable design.

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
  The `Table=auto` route is added automatically at `wg-quick up`.
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

- `parseAllowedIPs(str): string[]` — split on comma, trim, drop empties.
- Each CIDR well-formed — reuse the `isValidCIDR` logic (extract a shared helper or
  duplicate the small function; prefer a shared `src/lib/netValidation.js` if clean,
  else duplicate — decide in the plan).
- **No overlap** across the effective AllowedIPs of **all** peers (a normal
  client's effective set is its `${address}/32`; a site peer's is its parsed CIDR
  list). Overlap = any two CIDRs where one contains the other or they intersect.
  Compute via integer range comparison on the network (v4 first; v6 handling noted
  below).
- Returns a field-keyed errors object like `validateServerSettings`; the caller
  throws 400 on non-empty.

**IPv6 scope:** v4 is the required case (the RU-exit subnets are v4). v6 CIDRs
should be *accepted as well-formed* but the overlap check may be v4-only in this
pass if v6 range math is costly — **decide in the plan**; if v6 overlap is skipped,
`log`/comment it explicitly (no silent gap).

### 4. Apply path (`WireGuard.js`)

New method `setClientSitePeer({ clientId, allowedIPs, siteMasquerade })`:

1. Load config, resolve client (own-property guard, like `getClient`).
2. Build the candidate next state; run overlap+format validation against **all
   other** peers' effective AllowedIPs. Throw 400 with `errors` on failure.
3. Normalize: empty/whitespace `allowedIPs` → `null` and force
   `siteMasquerade: false` (masq only meaningful with a subnet).
4. **Bounce with rollback**, mirroring `updateServerSettings`'s restart branch:
   `wg-quick down wg0` (using on-disk conf) → assign fields + `saveConfig` →
   `wg-quick up wg0`; on failure roll back to previous client state, `saveConfig`,
   `wg-quick up` again, throw 500.
5. Return `{ client, mustReimport: false }` (server-side only; the *site* peer's own
   config — if it has one — would change, but the typical entry node is configured
   out-of-band, so no client-facing reimport signal needed. Confirm in plan.)

Normal client CRUD (`createClient`, `updateClientName`, enable/disable, legacy
toggle) is unchanged and stays on the `saveConfig → syncconf` path.

### 5. Routes (`Server.js`)

One route, `POST /api/wireguard/client/:clientId/allowedips`, with the existing
prototype-pollution param guard and session auth. Body: `{ allowedIPs, siteMasquerade }`.
Calls `setClientSitePeer`. On thrown `{ statusCode, errors }`, return that status +
errors (same shape the server-settings route already uses).

### 6. Web UI (`index.html`, `app.js`, `api.js`, `i18n.js`)

- An **"Advanced / site peer"** expander per client row, collapsed by default
  (simple-client UX untouched). Reveals:
  - AllowedIPs text input (placeholder `10.20.0.0/24, …`), comma-separated CIDRs.
  - "Masquerade this peer's traffic" checkbox (disabled/ignored when AllowedIPs is
    empty).
  - A Save button for the advanced fields → `api.setClientSitePeer(...)`.
- On 400, surface the field error inline (reuse the server-settings error pattern).
- A small **chip/icon marker** on rows that are site peers (AllowedIPs non-empty),
  so they're visually distinct from normal clients.
- `i18n.js`: keys for the expander label, the masq checkbox, the placeholder, and
  the overlap/format error messages (en first; other locales can fall back).

## Components & boundaries

- `clientValidation.js` — **pure**, no I/O: format + overlap. Independently testable.
- `configRender.js` — **pure** string render; gains client-aware masq lines.
- `WireGuard.js#setClientSitePeer` — orchestration + bounce/rollback (I/O).
- `Server.js` route — HTTP glue.
- UI — presentation; one new API call.

## Testing (`node:test`, per app convention)

- `clientValidation`: well-formed/malformed CIDR; single vs multi CIDR; overlap
  detection (exact dup, containment, partial intersection, `/32` vs subnet, no-overlap
  pass); empty → normal.
- `configRender`: server-conf AllowedIPs override present vs fallback `/32`; PostUp
  emits one masq rule per CIDR only for `siteMasquerade` clients, none otherwise;
  PostDown mirrors; disabled clients excluded.
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
