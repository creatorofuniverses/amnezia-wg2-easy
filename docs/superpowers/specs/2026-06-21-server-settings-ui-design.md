# Server Settings UI — design

The frontend for the server-settings backend (PR #3 / branch `feat/server-settings`).
Adds a web-UI screen for editing the **server's own** AmneziaWG config — network,
client defaults, obfuscation defaults, and server keypair — driving the routes
already shipped on the backend. Without it the backend is untestable through the UI.

Visual reference: section **04 · SERVER SETTINGS** of
`~/Documents/web-admin/AmneziaWG Web Admin.dc.html`, with component rules in
`~/Documents/web-admin/SPEC.md` and `~/Documents/web-admin/SPEC-server-settings.md`.
**Visual language only — reuse every existing M3 component; nothing new to design.**

## Scope

**In scope** (four groups, all backed by existing routes):

- **NETWORK** — public host, listen port, MTU
- **CLIENT DEFAULTS** — address range, persistent keepalive, DNS, AllowedIPs
- **OBFUSCATION DEFAULTS** — `jc/jmin/jmax`, `s1–s4`, `h1–h4`, `i1–i5` (behind an expander)
- **SERVER KEYPAIR** — show public key, regenerate

**Out of scope / deferred:**

- **WEB PANEL** (admin password + session timeout) — no backend yet; its own next cycle.
- The design board's separate **client-detail full view** — not built; the server-settings
  screen only borrows its visual pattern.
- Backfilling the 9 non-English locales for the new strings (English-first this cycle).

## Frontend architecture today (context)

The SPA is a single Vue 2 instance (`src/www/js/app.js`) with an inline template
(`src/www/index.html`), an `API` class (`src/www/js/api.js`), and `VueI18n`
(`src/www/js/i18n.js`, 10 locales). There is **no router and no view system** — the app
shows login or the client list, with modals/inline-edits driven by nullable state flags
(`clientDelete`, `qrcode`, `clientCreate`). The M3 "Network Teal" restyle is already applied
(tokens + custom CSS, not Tailwind utilities). Backend errors are serialized by H3's default
handler; the existing `api.js` reads only `json.error`.

## 1. View switching

Add a reactive `view` data property (`'clients'` default; no router, no new dependency).

- A 38px circular **server-icon button** in the header toolbar, placed **between the theme
  toggle and Logout** (order: theme · server · logout), same chrome as the other toolbar
  buttons. Click → `view = 'server-settings'`. Shown only when authenticated.
- Main content becomes `v-if="view === 'clients'"` (existing list) /
  `v-else-if="view === 'server-settings'"` (new screen). The global toolbar persists.
- The new screen's own header row = **back button** (→ `view = 'clients'`) + title
  "Server settings" + **Save** (filled `--primary`, 12px radius, right-aligned).
- The 1s `refresh()` poll keeps running in the background (it only updates `clients`); it
  must not touch the server-settings draft.

## 2. State (app.js `data`)

| Field | Purpose |
|---|---|
| `view` | `'clients'` \| `'server-settings'` |
| `serverSettings` | last-saved settings object (from GET); the dirty-comparison baseline |
| `serverDraft` | editable deep copy bound to the inputs |
| `serverErrors` | `{ [field]: message }` for inline display (client + backend) |
| `serverLoading` | true while the initial GET is in flight |
| `serverSaving` | true while a save/regenerate is in flight (disables Save) |
| `serverSaveResult` | `{ restarted, mustReimport }` from the last save, drives the post-save hint |
| `regenerateConfirm` | bool — drives the Delete-pattern danger modal |

On first entry to the view (lazy — when the server button is clicked, or guarded by
`serverSettings === null`), call `getServerSettings()` and set both `serverSettings` and
`serverDraft = deepCopy(result)`. `h1–h4` are `{min,max}` objects — deep-copy them.

## 3. API (api.js)

Three methods:

- `getServerSettings()` → `GET /api/server-settings` (via the shared `call()`).
- `regenerateKeypair()` → `POST /api/server-settings/regenerate-keypair` (via `call()`),
  returns `{ publicKey, mustReimport }`.
- `updateServerSettings(patch)` → `POST /api/server-settings`. **Needs its own fetch path**
  (not the shared `call()`), because on a 400 the field errors arrive under `data.errors`
  in H3's default error envelope (`{ statusCode, statusMessage, data: { errors } }`), which
  `call()` discards. On `!res.ok`: if `res.status === 400`, parse the body and throw an Error
  carrying the field map (e.g. `err.fieldErrors = body?.data?.errors || {}`); otherwise throw
  `new Error(body.message || body.error || res.statusText)`. On success return the parsed
  `{ settings, restarted, mustReimport }`. (Confirm the exact H3 envelope nesting at
  implementation time; `data.errors` is the contract the route sends.)

## 4. Data flow

```
enter view → getServerSettings() → serverSettings = result; serverDraft = deepCopy(result)
edit       → v-model on serverDraft.* ; serverErrors recomputed live (client validation)
Save       → enabled iff dirty && valid
             updateServerSettings(serverDraft)
               success → serverSettings = res.settings; serverDraft = deepCopy(res.settings);
                         serverSaveResult = { restarted, mustReimport }; serverErrors = {}
                         show post-save hint (see §6)
               400     → serverErrors = err.fieldErrors  (inline)
               other   → alert(err.message)  (existing pattern)
regenerate → regenerateConfirm = true → confirm modal → regenerateKeypair()
               success → serverDraft.publicKey = serverSettings.publicKey = res.publicKey;
                         serverSaveResult = { restarted: true, mustReimport: true }
```

- `dirty` (computed): `serverDraft` differs from `serverSettings` (deep compare incl. `h*`).
- `valid` (computed): `Object.keys(clientValidate(serverDraft)).length === 0`.
- **Save is disabled unless `dirty && valid && !serverSaving`.**
- The whole patch is sent (not a diff); the backend's `classify` compares against the
  persisted values, so resending unchanged fields is harmless.

## 5. Client-side validation (mirror of the backend rules)

A pure `serverSettingsErrors(draft)` helper in `app.js` (or a small inline function),
mirroring `src/lib/serverSettings.js` so Save-enable and inline styling are instant. The
backend remains authoritative — its 400 is surfaced identically (§3/§4). Rules:

| Field | Rule |
|---|---|
| `host` | non-empty |
| `port` | integer 1–65535 |
| `mtu` | empty/null, or integer 576–1500 |
| `dns` | comma-separated valid IPs (IPv4/IPv6) |
| `allowedIPs` | comma-separated valid CIDRs (IPv4/IPv6, incl. `0.0.0.0/0`, `::/0`) |
| `defaultAddress` | `x`-template (e.g. `10.8.0.x`) in the same /24 as `serverSettings.address`* |
| `persistentKeepalive` | integer ≥ 0 |
| `jc` | integer 1–128 |
| `jmin,jmax,s1–s4` | integers 0–1280; `jmin ≤ jmax` |
| `h1–h4` | `{min,max}` integers 5–2147483647, `min ≤ max` |
| `i1–i5` | free text (any string, or empty) |

*The server's own `address` is read-only and not in the editable set; the GET response does
not include it. To enforce the same-/24 rule client-side, the comparison base is the first
three octets of the current `defaultAddress` (which the server keeps inside its /24). If
that proves insufficient, relax to "valid `x`-template" client-side and rely on the backend
same-/24 check as the authority. IP/CIDR checks use a small browser validator (regex for
IPv4 + a basic IPv6/CIDR check); `net.isIP` is Node-only and unavailable in the browser.

## 6. Layout & components (reuse existing, per SPEC.md)

Top-to-bottom: header row (back · title · Save) → restart-notice banner → four group cards.
Section labels sit **outside/above** each card (11px/700/caps/`--primary`/ls .12em). Cards:
`surface-container` · 1px `outline-variant` · 16px radius · flat. Inputs: `surface-container`
fill · 1px `outline-variant` · 12px radius; focus → 2px `--primary`; invalid → 1px `--error`
+ helper text. **Mono** (`--font-mono`) for host/port/IP/CIDR/MTU/keepalive/keys/obfuscation
values; Manrope for labels/prose.

- **Restart notice** (neutral banner, `surface-container-highest`, info icon, not red):
  "Saving restarts the WireGuard interface — connected clients reconnect automatically.
  Changing host or port means clients must re-download their config."
- **NETWORK** — host (full-width mono input, helper "Hostname or IP clients dial") · port +
  MTU (mono inputs, 2-up grid).
- **CLIENT DEFAULTS** — address range + keepalive (2-up) · DNS + AllowedIPs (full-width).
  Subtitle: "Applied to new clients."
- **OBFUSCATION DEFAULTS** (+ AMNEZIA badge) — collapsed by default behind an "Advanced
  parameters" expander; chips preview the active set when collapsed. Expanded → grouped mono
  number-field grid (jc/jmin/jmax; S1–S4; H1–H4 as min/max pairs; I1–I5). Subtitle:
  "Server-wide defaults — every client inherits these unless it sets its own."
- **SERVER KEYPAIR** — public key (mono, truncated, copy icon → reuse `copyToClipboard`) ·
  Regenerate (outlined `--error` button + helper) → danger confirm modal.

Behaviors per the design:
- Save disabled until `dirty && valid`.
- After save with `mustReimport`, surface a one-line hint that existing clients must reimport
  (their endpoint/params changed). `restarted` may be shown too ("interface restarted").
- **Regenerate keypair** → confirm modal reusing the **Delete** pattern (danger well in
  `error-container`, filled `--error` confirm). Body makes the consequence explicit: it
  rotates the server key and **invalidates every client peer — they must reimport.**

## 7. i18n

All new strings added as `VueI18n` keys with English (`en`) values filled now; the other 9
locales fall back to English for this screen (per the `fallbackLocale: 'en'` already set).
Group headers, field labels, helper texts, the restart notice, post-save hints, and the
regenerate confirm copy all go through `$t`.

## 8. Testing & verification

No frontend test framework exists (inline Vue 2, no build step beyond Tailwind) — verification
is manual, consistent with the repo:

- `cd src && npm run lint` — `app.js`/`api.js` clean.
- `cd src && npm run buildcss` — only if new Tailwind utility classes are introduced; prefer
  reusing existing component classes/tokens so this is a no-op.
- **Manual browser smoke (the end-to-end gate the backend was missing):** open the screen;
  change DNS only → saves, no client reconnect, no restart hint; change port → save, clients
  drop + reimport hint shown; submit an invalid port → inline `--error` + helper, Save blocked,
  and the backend 400 maps to the same field; regenerate the keypair → confirm modal →
  public key updates + reimport hint. Requires a running server (Linux/Docker, `wg-quick`).

## Files touched (anticipated)

- `src/www/js/api.js` — `getServerSettings`, `updateServerSettings` (custom 400 parse),
  `regenerateKeypair`.
- `src/www/js/app.js` — view state, server-settings data + computed (`dirty`, `valid`),
  methods (load/save/regenerate), client validator.
- `src/www/index.html` — header server-icon button; the `view`-switched server-settings
  screen markup (groups, expander, danger modal).
- `src/www/js/i18n.js` — new English keys.
