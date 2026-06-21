# Per-Client Legacy (No-Imitation) Toggle — Design

**Date:** 2026-06-20
**Subsystem:** #4 of the proxy-redesign split (after #1 native datapath/responder, #2 web UI restyle, #3 awg://v1 share-string).
**Status:** Approved (brainstorm complete) — ready for implementation plan.

## Goal

Let each client be flagged as a **legacy client** — an AmneziaWG **2.0** client that does **not**
support the imitation-protocol feature. When a client is flagged legacy, every config the server
emits for it (download, QR, and the #3 `awg://v1` share-string) is automatically shaped to omit the
imitation-only keys, so the config imports and runs on a non-imitation 2.0 client.

## Key distinction (NOT 1.x vs 2.0)

- **"new" client** = supports the imitation protocol → gets the full config (today's output).
- **"legacy" client** = AmneziaWG 2.0 *without* imitation support → gets the full config minus the
  imitation-only keys.

Both are AmneziaWG 2.0, so they share all base obfuscation params. Only the imitation layer differs.

## What "shaping for legacy" removes

Applied as a pure text transform over the already-generated config:

- **Always remove** the `ImitateProtocol = …` line.
- **Remove `I1`–`I5`** lines **only when the value contains an angle-bracket tag** (`<` … `>`, e.g.
  `I1 = <qinit www.google.com>`). An `I`-param whose value is a plain raw string (no `<`) is **kept**.
- **Keep everything else unchanged:** `Jc/Jmin/Jmax`, `S1`–`S4` (all four — both sides are 2.0),
  `H1`–`H4` ranges as-is, and all standard WireGuard fields (`PrivateKey`, `Address`, `DNS`, `MTU`,
  `[Peer]` `PublicKey`/`PresharedKey`/`AllowedIPs`/`PersistentKeepalive`/`Endpoint`).

**Detection rule (chosen):** an `I`-param is the imitation form iff its value contains a `<`
character. Raw strings never contain `<`; this faithfully implements "strip if it contains `<…>`".

## Feasibility note (client-config-only; server unchanged)

This changes only what config text the server *emits* for a client. The server's own `wg0.conf` and
runtime params are untouched. A legacy client's traffic simply isn't imitation-shaped — the tunnel
still uses the shared 2.0 base params (`Jc`, `S1`–`S4`, `H1`–`H4`), which both sides already agree
on. Whether a real non-imitation 2.0 client connects cleanly is to be confirmed by a live test in the
user's environment (as with #3), but there is no protocol mismatch in the base handshake params.

## Architecture & components

Five small, well-bounded units.

### 1. `src/lib/stripImitationKeys.js` — pure text transform (no app deps)

- `stripImitationKeys(confText: string): string` — splits into lines, drops every
  `ImitateProtocol = …` line and every `I[1-5] = …` line whose value contains `<`, returns the
  rejoined text. No other line is altered. Stdlib only.
- Unit-tested with `node:test` (the suite added in #3 already wires `npm test`).

### 2. Per-client `legacy` flag (persisted state)

- New boolean field `client.legacy` in `wg0.json`, **default `false`**.
- `createClient` sets `legacy: false` on the new client object (alongside `enabled: true`).
- `getClients()` exposes it in its mapped output: `legacy: client.legacy === true` (so the frontend
  can render the toggle state). Existing clients without the field read as `false`.

### 3. Config-generation intermediate step

- In `WireGuard.getClientConfiguration({ clientId })`, after building the full config string and
  before returning, fetch the client and apply the transform when flagged:
  `return client.legacy ? stripImitationKeys(config) : config;`
- Because `/configuration` (download), `/qrcode.svg`, and `/share-string` (#3 `getClientShareString`)
  all derive from `getClientConfiguration`, **all three automatically respect the per-client flag.**
  No export route or button changes.

### 4. Toggle persistence — method + routes (mirror enable/disable)

- `WireGuard.setClientLegacy({ clientId, legacy })` — `getClient`, set `client.legacy = !!legacy`,
  `client.updatedAt = new Date()`, `saveConfig()` (mirrors `enableClient`/`disableClient`).
- Routes mirroring the existing enable/disable pair, including the same explicit prototype-pollution
  guard those write routes use:
  - `POST /api/wireguard/client/:clientId/legacy/enable` → `setClientLegacy({clientId, legacy:true})`
  - `POST /api/wireguard/client/:clientId/legacy/disable` → `setClientLegacy({clientId, legacy:false})`
  - Each guards `clientId` against `__proto__`/`constructor`/`prototype` (as the existing
    enable/disable/name/address routes do) and returns `{ success: true }`.

### 5. Frontend — per-client toggle (no export-button changes)

- `api.enableClientLegacy({clientId})` / `api.disableClientLegacy({clientId})` — `POST` to the two
  routes (mirroring `enableClient`/`disableClient` in `api.js`).
- A per-client **icon-button toggle** in the existing client-row action group (using the
  `.awg-icon-btn` class + an active state, from subsystem #2): a "mask/imitation" icon, **active when
  `client.legacy` is true**, tooltip e.g. *"Legacy client — imitation off"* vs *"Imitation on"*.
  Clicking calls `enableClientLegacy`/`disableClientLegacy` based on the current state, then
  `refresh()` (same pattern as the enable/disable handler in `app.js`).
- The download / QR / share-link buttons are **untouched**.

## Data flow

```
[client row] click legacy toggle
  → api.{enable,disable}ClientLegacy(id)
  → POST /client/:id/legacy/{enable,disable}
  → WireGuard.setClientLegacy({id, legacy}) → client.legacy = bool → saveConfig()
  → refresh() re-fetches getClients() (legacy reflected) → toggle re-renders

[any export] download / QR / copy-share-link (unchanged buttons)
  → getClientConfiguration({id})
       → build full config text
       → client.legacy ? stripImitationKeys(text) : text
  → served by /configuration, /qrcode.svg, or encoded by /share-string
```

## Error handling

- **Unknown / malicious clientId:** the toggle routes carry the explicit
  `__proto__`/`constructor`/`prototype` guard (mirroring enable/disable), and `getClient` already
  own-property-guards (subsystem #3 fix); unknown ids 404.
- **Missing `legacy` field on older clients:** treated as `false` everywhere (`client.legacy === true`
  / `client.legacy ?`), so pre-existing clients default to full config — no migration needed.
- **Idempotent toggling:** setting the flag to its current value is harmless (writes the same value).
- **Strip transform robustness:** line-based; a config with no imitation keys passes through
  unchanged. The transform never throws on normal input.

## Testing

- **`stripImitationKeys` — `node:test`** (scoped automated tests, per app convention + #3 precedent):
  - removes `ImitateProtocol`; leaves a config without it unchanged.
  - removes `I1`–`I5` whose value contains `<…>`; **keeps** raw-string `I`-params.
  - does not alter `S1`–`S4`, `H1`–`H4`, `Jc/Jmin/Jmax`, or any `[Peer]`/standard field.
  - mixed case: some `I`-params angle-bracket (stripped), some raw (kept), in one config.
  - idempotent: `strip(strip(x)) === strip(x)`.
- **Backend flag + routes + generation hook:** manual verification (no H3/WireGuard test harness):
  toggle a client legacy, download its config, confirm `ImitateProtocol` and any `<…>` `I`-params are
  gone while base params remain; toggle off, confirm they return. Confirm `getClients` reports
  `legacy`. (Live verification deferred to the user's `NET_ADMIN` env, like #2/#3.)
- **UI:** manual — toggle reflects/persists state; export buttons unchanged; `buildcss` + `lint` clean.

## File structure

- **Create:** `src/lib/stripImitationKeys.js` — the transform.
- **Create:** `src/lib/__tests__/stripImitationKeys.test.js` — `node:test` suite.
- **Modify:** `src/lib/WireGuard.js` — `legacy: false` in `createClient`; `legacy` in `getClients`
  map; `setClientLegacy`; apply the transform in `getClientConfiguration`; require the module.
- **Modify:** `src/lib/Server.js` — the two `legacy/{enable,disable}` routes (with proto guard).
- **Modify:** `src/www/js/api.js` — `enableClientLegacy` / `disableClientLegacy`.
- **Modify:** `src/www/js/app.js` — the toggle handler (mirror enable/disable).
- **Modify:** `src/www/index.html` — the legacy icon-toggle in the client-row action group.
- **Modify:** `src/www/js/i18n.js` — tooltip keys (English fallback acceptable in all locales).

## Out of scope / deferred

- No server-side config changes (the server's own imitation params are unchanged).
- No 1.x (pre-2.0) support, no plain-WireGuard stripping (the legacy target is non-imitation **2.0**).
- No `?variant=` export params or separate legacy endpoints — the per-client flag is the single
  source of truth and all existing exports inherit it.
- Styled QR generation (carried from #3) remains deferred.
