# Web UI Restyle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restyle the AmneziaWG Easy admin web UI to the "Network Teal" (Material 3) design language — visual only, no behavior changes.

**Architecture:** The whole UI is one `src/www/index.html` (Vue 2 inline template) with an inline `<style>` block of `awg-*` component classes keyed to CSS custom properties; light/dark via a `.dark` root class. We replace the token block with the vendored M3 token set, self-host fonts, swap the logo, retire the legacy red→teal Tailwind override hacks, and remap each `awg-*` component class plus a few markup spots to the new anatomy. Tailwind (`tailwind.config.js` → `www/css/app.css`) is unchanged except a rebuild.

**Tech Stack:** Vue.js 2 (vendored), Tailwind CSS, CSS custom properties, self-hosted WOFF/TTF fonts, ApexCharts (vendored).

## Global Constraints

- **Visual language only — no IA or behavior changes.** Auth, create/delete/QR, enable/disable, charts toggle, theme toggle, inline name/address edit must all still work unchanged.
- **Source of truth:** `docs/superpowers/specs/web-ui-restyle/SPEC.md` (component table + 7 screen specs) and `docs/superpowers/specs/web-ui-restyle/visual-board.dc.html` (pixel reference, open in browser). `tokens.css` is the authoritative token set.
- **Fonts:** Manrope (proportional) + JetBrains Mono (keys/IPs/ports/stat values only). **Self-hosted — no Google Fonts.**
- **Shape:** 12px inputs/chips · 16px cards · 18px list rows · 999px pills/toggles. Filled buttons are 12px rounded rect — **never a pill**.
- **Elevation:** flat. Cards = `--surface-container` + 1px `--outline-variant`, no shadow. Only modals lift (`--elevation-modal`).
- **No legacy token names** (`--accent`, `--surface-light`, `--online`, `--bg-light`, `--text-light*`, etc.) may remain at the end of the plan. A temporary `--accent: var(--primary)` alias shim is allowed mid-migration but must be gone by Task 6.
- **Deferred / out of scope:** per-client obfuscation editor + its backend, new screens/navigation, QR scanning, clipboard import, split tunneling, `awg://v1` share-string, dual export. AMNEZIA badge + obfuscation chips render **read-only** only where obfuscation is already shown.
- **No automated UI tests exist.** Per-task verification = `npm run lint` clean + `npm run buildcss` clean + **manual visual check in BOTH light and dark** against the board. State this honestly in commits; never claim a visual result you didn't view.

## Verification commands (used by every task)

```bash
cd src && npm run lint        # ESLint — must be clean
cd src && npm run buildcss    # recompile Tailwind → www/css/app.css, must succeed
cd src && npm run serve       # dev server (DEBUG=Server,WireGuard) for manual visual check
```

Manual check each task: load the app, toggle light↔dark, exercise the touched screen, compare to `visual-board.dc.html`. There is no `PASSWORD` by default in `serve`; use `npm run serve-with-password` (PASSWORD=wg) to view the login screen.

## File Structure

- **Modify:** `src/www/index.html` — the `<style>` block (lines 17–491) and the inline template markup. This is where ~all work happens.
- **Create:** `src/www/fonts/` — 6 self-hosted font files (copied from the vendored package).
- **Create/Modify:** `src/www/img/signal-mark.svg`, `signal-mark-light.svg` (copied from vendored assets); logo `<img>` refs at index.html:499 and :950.
- **Reference (do not edit):** `docs/superpowers/specs/web-ui-restyle/{tokens.css,SPEC.md,visual-board.dc.html,assets/,fonts/}`.
- `tailwind.config.js` — unchanged (rebuild only).

---

### Task 1: Foundations — fonts, tokens, kill legacy overrides

**Files:**
- Create: `src/www/fonts/*` (6 files)
- Modify: `src/www/index.html` head (13–15) + `<style>` (24–54, 56–58, 110–160)

**Interfaces:**
- Produces: the full M3 token set on `:root`/`.dark` (names per `tokens.css`); `@font-face` for Manrope + JetBrains Mono; `--font-sans`/`--font-mono`. All later tasks consume these token names.

- [ ] **Step 1: Vendor the fonts**

```bash
mkdir -p src/www/fonts
cp docs/superpowers/specs/web-ui-restyle/fonts/*.ttf src/www/fonts/
ls src/www/fonts/   # expect 6 .ttf files
```

- [ ] **Step 2: Remove Google Fonts, keep the rest of head**

In `src/www/index.html` delete lines 13–15 (the two `preconnect` links and the `fonts.googleapis.com` stylesheet link). Leave lines 4–12 intact.

- [ ] **Step 3: Add @font-face at the top of the `<style>` block**

Insert immediately after `<style>` (before `[v-cloak]`):

```css
@font-face { font-family: "Manrope"; font-weight: 400; font-display: swap; src: url("./fonts/Manrope-Regular.ttf") format("truetype"); }
@font-face { font-family: "Manrope"; font-weight: 500; font-display: swap; src: url("./fonts/Manrope-Medium.ttf") format("truetype"); }
@font-face { font-family: "Manrope"; font-weight: 600; font-display: swap; src: url("./fonts/Manrope-SemiBold.ttf") format("truetype"); }
@font-face { font-family: "Manrope"; font-weight: 700; font-display: swap; src: url("./fonts/Manrope-Bold.ttf") format("truetype"); }
@font-face { font-family: "JetBrains Mono"; font-weight: 400; font-display: swap; src: url("./fonts/JetBrainsMono-Regular.ttf") format("truetype"); }
@font-face { font-family: "JetBrains Mono"; font-weight: 500; font-display: swap; src: url("./fonts/JetBrainsMono-Medium.ttf") format("truetype"); }
```

- [ ] **Step 4: Replace the token block**

Replace `src/www/index.html` lines 24–54 (the `:root { … }` and `.dark { … }` blocks) **verbatim with the contents of** `docs/superpowers/specs/web-ui-restyle/tokens.css` (its `:root { … }` and `.dark { … }` blocks — everything between the file's banner comment and EOF). This brings in `--primary`, the surface ladder, `--on-surface*`, `--outline*`, `--status-*`, `--connected-*`, `--error*`, the type scale, shape, and spacing variables, and sets `--font-sans: "Manrope", …`.

- [ ] **Step 5: Add a temporary alias shim (removed in Task 6)**

Directly after the pasted `.dark { … }` block, add:

```css
/* TEMP shim — lets unmigrated awg-* classes resolve until Task 6 removes them */
:root, .dark {
  --accent: var(--primary);
  --accent-hover: var(--primary);
  --accent-light: var(--primary);
  --accent-glow: transparent;
  --accent-subtle: var(--surface-container-highest);
  --online: var(--status-connected);
  --online-glow: transparent;
  --danger: var(--error);
  --danger-hover: var(--error);
}
```

- [ ] **Step 6: Remove the red→teal override hacks**

Delete `src/www/index.html` lines 110–153 (the `/* ── Accent color overrides ── */` block: every `.bg-red-*`, `.hover\:bg-red-*`, `.text-red-*`, `.dark\:bg-red-*`, border/focus red rules). **Keep** `.awg-btn-danger` (154–160) but repoint it (next step). Then, in the template markup, replace Tailwind red utility classes with neutral/primary equivalents as you touch each component in Tasks 3–5; for now the deletion is safe because the alias shim + the component-class CSS carry the colors. After deletion, grep to confirm no `red-` utility is relied on for the accent:

```bash
grep -n "bg-red-\|text-red-\|border-red-" src/www/index.html | head
```

Any hit that is an *accent* (not the genuine danger/delete button) gets swapped to a token-based `awg-*` class during its component task. Note hits in the plan's commit message.

- [ ] **Step 7: Repoint danger button + global font**

`.awg-btn-danger` (now ~110–115 after deletion): change `var(--danger)` → `var(--error)` and `var(--danger-hover)` → `var(--error)`. The `* { font-family: var(--font-sans) !important; }` rule (was line 56) is unchanged and now resolves to Manrope.

- [ ] **Step 8: Build, lint, and visually smoke-test**

```bash
cd src && npm run buildcss && npm run lint
cd src && npm run serve   # open http://localhost:51821
```
Expected: page renders in light AND dark; text is Manrope; mono spans are JetBrains Mono; no console 404s for fonts; no element falls back to an unstyled/invisible color. (Components still look "old shape" — that's fine; anatomy comes next.)

- [ ] **Step 9: Commit**

```bash
git add src/www/fonts src/www/index.html
git commit -m "feat(ui): self-host fonts + M3 Network Teal tokens; drop Google Fonts + red overrides"
```

---

### Task 2: Shared atoms — buttons, inputs, toggles, badges

**Files:**
- Modify: `src/www/index.html` `<style>` — `.awg-btn*` (~195–225), `.awg-icon-btn` (~227–243), `.awg-toggle-*` (~245–258), inputs (~293–306), `.awg-mono` (~274–280), `.awg-version` (~479–490), `.awg-spinner`, scrollbar.

**Interfaces:**
- Produces: restyled `awg-btn-primary`, a new `awg-btn-tonal`, `awg-btn-danger`, outlined input focus, `awg-toggle-on/off`, `awg-version` — consumed by chrome/list/modals in Tasks 3–5.

- [ ] **Step 1: Filled + tonal + text buttons**

Replace `.awg-btn`/`.awg-btn-primary` rules (lines ~197–225) with (per SPEC.md Components):

```css
.awg-btn { border-radius: var(--radius-input); font-weight: var(--weight-semibold); font-size: var(--type-button); transition: background-color .15s ease, transform .1s ease; }
.awg-btn:active { transform: scale(0.98); }
.awg-btn-primary { background-color: var(--primary) !important; color: var(--on-primary) !important; border: none !important; }
.awg-btn-primary:hover { filter: brightness(0.96); }
.awg-btn-primary.is-disabled, .awg-btn-primary:disabled { background-color: var(--surface-variant) !important; color: var(--on-surface-variant) !important; }
.awg-btn-tonal { background-color: var(--primary-container) !important; color: var(--on-primary-container) !important; border: none !important; border-radius: var(--radius-input); font-weight: var(--weight-semibold); }
.awg-btn-text { background: transparent !important; color: var(--on-surface-variant) !important; }
.awg-btn-danger { background-color: var(--error) !important; color: #fff !important; border-radius: var(--radius-input); }
.awg-btn-danger:hover { filter: brightness(0.96); }
```

- [ ] **Step 2: Icon buttons + toolbar buttons**

Replace `.awg-icon-btn` (227–243) and `.awg-toolbar-btn` (450–477) to use tokens: transparent → hover `--surface-container-highest`; color `--on-surface-variant` → hover `--on-surface`; toolbar buttons become **38px circular** (`border-radius: var(--radius-pill)`) per SPEC screen 2:

```css
.awg-icon-btn { border-radius: var(--radius-input); padding: .5rem; background: transparent; color: var(--on-surface-variant); transition: background-color .15s ease, color .15s ease; }
.awg-icon-btn:hover { background-color: var(--surface-container-highest); color: var(--on-surface); }
.awg-toolbar-btn { width:38px; height:38px; border-radius: var(--radius-pill); display:flex; align-items:center; justify-content:center; background: var(--surface-container-high); color: var(--on-surface-variant); border:none; cursor:pointer; transition: background-color .15s ease, color .15s ease; }
.awg-toolbar-btn:hover { background: var(--surface-container-highest); color: var(--on-surface); }
```
(Delete the now-redundant `.dark .awg-toolbar-btn*` and `.dark .awg-icon-btn:hover` rules — tokens flip automatically.)

- [ ] **Step 3: Toggle switch**

Replace `.awg-toggle-on/off` (247–258):

```css
.awg-toggle-on { background: var(--primary) !important; }
.awg-toggle-off { background: var(--surface-variant) !important; }
```
(Delete `.dark .awg-toggle-off`.)

- [ ] **Step 4: Outlined inputs**

Replace input rules (293–306):

```css
input[type="text"], input[type="password"] {
  font-family: var(--font-sans) !important;
  background: var(--surface-container) !important;
  border: 1px solid var(--outline-variant) !important;
  color: var(--on-surface) !important;
  border-radius: var(--radius-input) !important;
  transition: border-color .15s ease, box-shadow .15s ease !important;
}
input[type="text"]:focus, input[type="password"]:focus {
  border-color: var(--primary) !important;
  box-shadow: 0 0 0 1px var(--primary) !important;   /* effective 2px stroke */
  outline: none !important;
}
input.awg-invalid { border-color: var(--error) !important; }
```

- [ ] **Step 5: Mono, version pill, spinner, scrollbar**

```css
.awg-mono { font-family: var(--font-mono) !important; font-size: var(--type-mono); letter-spacing: -0.01em; }
.awg-version { font-family: var(--font-mono) !important; font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); background: var(--primary-container); color: var(--on-primary-container); font-weight: var(--weight-semibold); }
.awg-spinner { color: var(--primary) !important; }
```
Replace scrollbar-thumb colors (377/382) with `var(--surface-variant)` (light) — drop the `.dark` override.

- [ ] **Step 6: Build, lint, visual check, commit**

```bash
cd src && npm run buildcss && npm run lint
git add src/www/index.html
git commit -m "feat(ui): restyle buttons, inputs, toggles, version pill to M3 tokens"
```
Visual: buttons are 12px filled (not pill), focus ring is teal, toggles teal/grey, both themes.

---

### Task 3: Global chrome — header, update banner, login, logo

**Files:**
- Create: `src/www/img/signal-mark.svg`, `src/www/img/signal-mark-light.svg`
- Modify: `src/www/index.html` markup (header ~496–545, update banner ~546, login ~940–960) + `<style>` (`.awg-title`/`.awg-subtitle`, `.awg-update-banner/btn`, `.awg-login-card/avatar`, `.awg-card-header`)

**Interfaces:**
- Consumes: atoms from Task 2. Produces: themed header + login.

- [ ] **Step 1: Vendor the Signal marks**

```bash
cp docs/superpowers/specs/web-ui-restyle/assets/signal-mark.svg src/www/img/
cp docs/superpowers/specs/web-ui-restyle/assets/signal-mark-light.svg src/www/img/
```

- [ ] **Step 2: Swap the logo images**

index.html:499 (header) — replace `src="./img/logo.svg" width="44"` with the Signal mark, light/dark via two `<img>` tags toggled by Vue's existing `uiTheme`, e.g.:

```html
<img v-if="uiTheme !== 'dark'" src="./img/signal-mark.svg" width="36" class="inline align-middle" />
<img v-else src="./img/signal-mark-light.svg" width="36" class="inline align-middle" />
```
index.html:950 (login) — same swap at `width="48"`. (Use `uiTheme` — the same var the theme toggle at :509 reads.)

- [ ] **Step 3: Header toolbar + title**

Header bar background → flat `--surface` (no card). `.awg-title` → `font-size: var(--type-title); font-weight: var(--weight-semibold); letter-spacing: -0.01em;`. `.awg-subtitle` → `color: var(--on-surface-variant)` (drop `.dark` override). Toolbar buttons already done in Task 2.

- [ ] **Step 4: Update banner → tonal (not red/gradient)**

```css
.awg-update-banner { background: var(--primary-container) !important; color: var(--on-primary-container) !important; border: none !important; border-radius: var(--radius-card) !important; }
.awg-update-btn { background: var(--primary) !important; color: var(--on-primary) !important; border: none !important; border-radius: var(--radius-input) !important; }
.awg-update-btn:hover { filter: brightness(0.96); }
```
In the banner markup, render the version with `.awg-mono`.

- [ ] **Step 5: Login card (drop neumorphic glow)**

```css
.awg-login-card { border-radius: var(--radius-card) !important; background: var(--surface-container) !important; border: 1px solid var(--outline-variant) !important; box-shadow: none !important; }
.awg-login-avatar { background: var(--primary) !important; box-shadow: none; }
.awg-card-header { border-bottom: 1px solid var(--outline-variant); }
```
(Delete `.dark .awg-login-card`, `.dark .awg-card-header`.) Ensure the Sign In button uses `awg-btn awg-btn-primary` and is disabled until the password field is non-empty (behavior already exists; just confirm classes).

- [ ] **Step 6: Build, lint, visual check (login via serve-with-password), commit**

```bash
cd src && npm run buildcss && npm run lint
cd src && npm run serve-with-password   # verify login screen
git add src/www/img/signal-mark*.svg src/www/index.html
git commit -m "feat(ui): Signal mark + themed header, tonal update banner, flat login card"
```

---

### Task 4: Client list — cards, rows, connected state, empty state

**Files:**
- Modify: `src/www/index.html` markup (clients card ~562–820, the `v-for` client row) + `<style>` (`.awg-card`, `.awg-client-row`, `.awg-online-dot/ping`, `.awg-avatar`) + add connected-row + stats-strip + empty-state CSS.

**Interfaces:**
- Consumes: tokens + atoms. Produces: connected/disconnected row anatomy. No data changes — `client.transferTx/transferRx/latestHandshakeAt` already exist; the strip re-presents them.

- [ ] **Step 1: Flatten the card**

```css
.awg-card { background: var(--surface-container); border: 1px solid var(--outline-variant); border-radius: var(--radius-card); box-shadow: none; overflow: hidden; }
```
(Delete `.dark .awg-card` shadow/border overrides.) Remove the `body::before` grain overlay rules (75–87) — the M3 look is flat; confirm with the board.

- [ ] **Step 2: "Clients" header + New button**

In the clients card header (~562–580), ensure the title reads "Clients" and the create trigger is a filled button with a leading `+` (`awg-btn awg-btn-primary`, label `$t('newClient')` or existing key). No new behavior — reuse the existing create-modal trigger.

- [ ] **Step 3: Row anatomy + status dot**

```css
.awg-client-row { transition: background-color .15s ease; border-radius: var(--radius-list-row); }
.awg-online-dot { background-color: var(--status-connected) !important; box-shadow: none; }
.awg-offline-dot { background-color: var(--status-disconnected) !important; }
.awg-avatar { background: var(--surface-container-highest); border-radius: 12px; }
```
(Delete `.awg-online-ping` glow + `.dark .awg-avatar`.) The "connected" predicate already exists in markup at ~610 (`client.latestHandshakeAt && (now - handshake < 10min)`) — reuse it to pick dot color and the connected card style below.

- [ ] **Step 4: Connected card + stats strip**

Add CSS:

```css
.awg-client-connected { background: var(--connected-fill); border: 1.5px solid var(--connected-stroke); border-radius: var(--radius-list-row); }
.awg-stats-strip { display:flex; gap: var(--space-xl); border-top: 1px solid var(--outline-variant); margin-top: var(--space-md); padding-top: var(--space-md); }
.awg-stat-label { font-size: var(--type-stat-label); font-weight: var(--weight-semibold); letter-spacing: .05em; text-transform: uppercase; color: var(--on-surface-variant); }
.awg-stat-value { font-family: var(--font-mono); font-size: var(--type-mono); }
.awg-stat-value--rate { color: var(--primary); }
.awg-stat-value--meta { color: var(--on-surface); }
```
In markup, bind the connected class on the row wrapper (`:class="{ 'awg-client-connected': isConnected(client) }"` — reuse the existing inline predicate, do not add a method unless one already exists). Inside connected rows, render a stats strip:

```html
<div class="awg-stats-strip" v-if="isConnectedPredicate">
  <div><div class="awg-stat-label">{{ $t('totalDownload') }}</div><div class="awg-stat-value awg-stat-value--rate">{{ bytes(client.transferRx) }}</div></div>
  <div><div class="awg-stat-label">{{ $t('totalUpload') }}</div><div class="awg-stat-value awg-stat-value--rate">{{ bytes(client.transferTx) }}</div></div>
  <div><div class="awg-stat-label">HANDSHAKE</div><div class="awg-stat-value awg-stat-value--meta">{{ timeago(client.latestHandshakeAt) }}</div></div>
</div>
```
Use the existing `bytes()` / timeago helpers already used in the current rows (~672–693). Disconnected rows keep the existing single mono meta line; no strip.

- [ ] **Step 5: Empty state**

When `clients.length === 0`, replace the empty area with a centered Signal mark in a `--surface-container-highest` well + a short hint, keeping the New button in the card header:

```html
<div v-if="clients && clients.length === 0" class="flex flex-col items-center justify-center text-center" style="padding: var(--space-xl) 0;">
  <div style="background: var(--surface-container-highest); border-radius: var(--space-xl); padding: var(--space-xl);">
    <img :src="uiTheme !== 'dark' ? './img/signal-mark.svg' : './img/signal-mark-light.svg'" width="40" />
  </div>
  <p class="awg-subtitle" style="margin-top: var(--space-md);">{{ $t('noClients') || 'No clients yet' }}</p>
</div>
```
(If `noClients` i18n key is absent, use a plain string fallback as shown; do not add keys to all 10 locale files in this task.)

- [ ] **Step 6: Build, lint, visual check (serve), commit**

```bash
cd src && npm run buildcss && npm run lint
git add src/www/index.html
git commit -m "feat(ui): M3 client list — connected tint + stats strip, status dots, empty state"
```
Visual: a connected client shows the teal-tinted card + DOWNLOAD/UPLOAD/HANDSHAKE strip; disconnected stays plain; empty state centered; both themes.

---

### Task 5: Modals — create, delete, QR/config

**Files:**
- Modify: `src/www/index.html` modal markup (create, delete, QR ~421 styles + the modal templates) + `<style>` (`.awg-modal*`, `.awg-qr-card`) + add `.awg-badge`, `.awg-chip`, `.awg-field-row` CSS.

**Interfaces:**
- Consumes: tokens + atoms. Produces: `.awg-badge`, `.awg-chip`, `.awg-field-row` (reusable read-only anatomy).

- [ ] **Step 1: Modal shell**

```css
.awg-modal-overlay { background: rgba(16,24,40,.45); backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px); }
.awg-modal { background: var(--surface-container-lowest) !important; border-radius: 18px !important; border: none !important; box-shadow: var(--elevation-modal) !important; }
.awg-modal-footer { background: var(--surface-container-low); border-top: 1px solid var(--outline-variant); }
```
(Delete `.dark .awg-modal`.) Apply `.awg-modal-footer` to each modal's button row.

- [ ] **Step 2: Create modal**

Teal avatar tile (reuse `.awg-login-avatar` or a `--primary` tile), name input outlined; Create button `awg-btn awg-btn-primary` disabled until the name is non-empty (behavior already present — keep). No obfuscation fields (deferred).

- [ ] **Step 3: Delete modal**

Danger well using `--error-container`/`--on-error-container`; client name in `.awg-mono`; confirm button `awg-btn awg-btn-danger`. Add:

```css
.awg-danger-well { background: var(--error-container); color: var(--on-error-container); border-radius: var(--radius-input); }
```

- [ ] **Step 4: QR / config modal + field-row/badge/chip anatomy**

```css
.awg-qr-card { border-radius: 18px; padding: var(--space-xl); background: #fff; box-shadow: var(--elevation-modal); }
.awg-badge { display:inline-flex; align-items:center; background: var(--primary-container); color: var(--on-primary-container); font-size: 10px; font-weight: var(--weight-bold); letter-spacing: .05em; text-transform: uppercase; padding: 2px 8px; border-radius: var(--radius-pill); }
.awg-chip { display:inline-flex; gap:6px; font-family: var(--font-mono); font-size:12px; background: var(--surface-container-highest); border: 1px solid var(--outline-variant); border-radius: var(--radius-pill); padding: 3px 10px; }
.awg-chip .k { color: var(--on-surface-variant); } .awg-chip .v { color: var(--on-surface); }
.awg-field-row { display:flex; flex-direction:column; gap:2px; padding: 9px 0; border-bottom: 1px solid var(--outline-variant); }
.awg-field-row:last-child { border-bottom: none; }
.awg-field-label { font-size: var(--type-field-label); color: var(--on-surface-variant); }
.awg-field-value { font-size: var(--type-field-value); font-weight: var(--weight-semibold); color: var(--on-surface); }
.awg-field-value.mono { font-family: var(--font-mono); }
.awg-section-label { font-size: var(--type-section); font-weight: var(--weight-bold); letter-spacing:.12em; text-transform:uppercase; color: var(--primary); padding-left: 4px; margin: 7px 0 4px; }
```
QR modal keeps the white quiet-zone card on the scrim, name caption (`.awg-mono`), ✕ close. If the modal already shows config fields (endpoint/address), wrap them as `.awg-field-row`s under section labels; render an `.awg-badge` reading "AMNEZIA" by the OBFUSCATION label and any visible obfuscation params as `.awg-chip`s — **read-only**. If the current modal shows only the QR image, leave structure as-is and just restyle the card (do not invent new data).

- [ ] **Step 5: Build, lint, visual check (all three modals), commit**

```bash
cd src && npm run buildcss && npm run lint
git add src/www/index.html
git commit -m "feat(ui): M3 modals — create/delete/QR + field-row/badge/chip read-only anatomy"
```

---

### Task 6: Polish — remove shim, dark/locale/responsive pass, board diff

**Files:**
- Modify: `src/www/index.html` (`<style>` shim removal + cleanup) ; possibly small markup fixes.

- [ ] **Step 1: Remove the alias shim and verify no legacy tokens remain**

Delete the TEMP shim block added in Task 1 Step 5. Then:

```bash
grep -nE "var\(--accent|var\(--surface-light|var\(--surface-dark|var\(--bg-light|var\(--bg-dark|var\(--online|var\(--danger|var\(--text-light|var\(--text-dark|--accent-" src/www/index.html
```
Expected: **no matches.** Fix any remaining usage by pointing it at the correct M3 token, then re-run until clean.

- [ ] **Step 2: Dark-mode pass**

Toggle dark and walk every screen (login, list empty, list with connected+disconnected, charts on, create/delete/QR modals). Fix any low-contrast or wrong-surface spots by adjusting the offending `awg-*` rule to the right token (no new `.dark` overrides — tokens flip).

- [ ] **Step 3: Locale overflow + responsive**

Switch `LANGUAGE` (e.g. `de`, `ru`) and confirm buttons/labels size to content with no clipping (the section-header `padding-left: 4px` clears card corners). Narrow the viewport to mobile: cards go full-width, the stats strip wraps. Fix with flex-wrap/min-width where needed.

- [ ] **Step 4: ApexCharts inside the connected card**

With the charts toggle on, confirm the existing ApexCharts mounts inside the connected card area; set its line stroke to `--primary` (sparkline style) via the existing chart options in `js/app.js` if a color is hard-coded. Visual only; do not change chart data wiring.

- [ ] **Step 5: Final board diff**

Open `docs/superpowers/specs/web-ui-restyle/visual-board.dc.html` side-by-side with the running app; reconcile remaining spacing/shape deltas against SPEC.md conventions (12/16/18/999 radii; 4px spacing grid).

- [ ] **Step 6: Final build, lint, commit**

```bash
cd src && npm run buildcss && npm run lint
git add src/www/index.html src/www/js/app.js
git commit -m "polish(ui): remove token shim, dark/locale/responsive pass, charts in connected card"
```

---

## Self-Review

**Spec coverage:** Tokens (T1), fonts self-hosted + no Google Fonts (T1), logo/Signal mark (T3), kill red overrides + no legacy tokens (T1+T6), buttons/inputs/toggles/badge/chips/field-row/switch (T2,T5), header+update banner+login (T3), client list connected/disconnected + stats strip + empty state (T4), modals create/delete/QR (T5), dark/locale/responsive/charts (T6). Deferred items (obfuscation editor, new screens, #3/#4) intentionally absent. ✓

**Placeholder scan:** No TBD/TODO; every code step has concrete CSS/markup. The two i18n fallbacks (`noClients`, New label) are explicit "use existing key or literal" instructions, not placeholders. ✓

**Type/name consistency:** New class names used consistently — `awg-btn-tonal`, `awg-client-connected`, `awg-stats-strip`, `awg-stat-value--rate/--meta`, `awg-badge`, `awg-chip`, `awg-field-row`, `awg-section-label`, `awg-danger-well`, `awg-modal-footer`. Token names match `tokens.css`. Predicate for "connected" reuses the existing inline expression (handshake < 10min), not a new method. ✓

**Risk note:** The biggest risk is residual reliance on deleted red-utility classes — Task 1 Step 6 greps for it and Task 6 Step 1 enforces zero legacy tokens before completion.
