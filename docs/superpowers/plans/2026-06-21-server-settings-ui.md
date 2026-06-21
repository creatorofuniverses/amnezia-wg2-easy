# Server Settings UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a web-UI screen to edit the server's own AmneziaWG config (network, client defaults, obfuscation, keypair), driving the routes already shipped on branch `feat/server-settings`.

**Architecture:** The Vue 2 SPA gets a lightweight `view` flag (no router). A header server-icon button opens a full-view "Server settings" screen that loads `GET /api/server-settings` into an editable draft, validates client-side (mirroring the backend), and saves via `POST /api/server-settings`; keypair regeneration uses a Delete-pattern danger modal. Reuses the existing M3 ("Network Teal") component classes; new component CSS is built from existing tokens.

**Tech Stack:** Vue 2 (global build, inline template in `index.html`), VueI18n, plain `fetch`. No frontend test framework and no build step beyond Tailwind (`npm run buildcss`); verification is `npm run lint` + manual browser smoke.

## Global Constraints

- All frontend files under `src/www/`. Run `npm run lint` and `npm run buildcss` from `src/` (`cd src && npm run lint`).
- **No automated frontend tests exist** (inline Vue 2). Each task ends with `cd src && npm run lint` clean + a specific manual browser check. ESLint covers `js/*.js` only (not `index.html`).
- **Reuse existing component classes** — `awg-card`, `awg-btn`/`awg-btn-primary`/`awg-btn-danger`/`awg-btn-text`/`is-disabled`, `awg-toolbar-btn`, `awg-mono`, `awg-modal`/`awg-modal-overlay`/`awg-modal-footer`, `awg-danger-well`, `awg-fade-in`. New CSS uses ONLY existing tokens (`--primary`, `--on-surface`, `--on-surface-variant`, `--surface-container`, `--surface-container-highest`, `--outline-variant`, `--error`, `--on-error-container`, `--radius-pill`, `--font-mono`). Avoid new Tailwind utility classes so `buildcss` stays a no-op.
- Backend routes (already on this branch): `GET /api/server-settings`, `POST /api/server-settings` (200 → `{settings, restarted, mustReimport}`; **400 → `{ statusCode, statusMessage, data: { errors } }`**), `POST /api/server-settings/regenerate-keypair` (→ `{publicKey, mustReimport}`). The GET response has **no `address`** field and never includes `privateKey`. Never display or send `privateKey`.
- i18n: add new strings as keys under `messages.en` in `js/i18n.js`; the other 9 locales fall back to English (`fallbackLocale: 'en'` is already set). Use `$t('serverSettings.<key>')`.
- Mono font (`awg-mono`) for host, port, IPs/CIDRs, MTU, keepalive, keys, and all obfuscation values; Manrope (default) for labels and prose.
- Branch: `feat/server-settings` (frontend ships with the backend in PR #3).

---

### Task 1: API methods (`api.js`)

**Files:**
- Modify: `src/www/js/api.js` (add methods inside the `API` class, after `getClientShareString`, before the closing `}` at line ~176)

**Interfaces:**
- Consumes: the existing `call()` helper and `fetch`.
- Produces:
  - `getServerSettings(): Promise<object>` — the settings object.
  - `updateServerSettings(patch): Promise<{settings, restarted, mustReimport}>` — on a 400, rejects with an `Error` whose `.fieldErrors` is the `{field: message}` map.
  - `regenerateKeypair(): Promise<{publicKey, mustReimport}>`.

- [ ] **Step 1: Add the three methods**

In `src/www/js/api.js`, immediately before the final closing `}` of the `class API` body, add:

```js
  async getServerSettings() {
    return this.call({
      method: 'get',
      path: '/server-settings',
    });
  }

  async updateServerSettings(patch) {
    const res = await fetch('./api/server-settings', {
      method: 'post',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    });
    let body = {};
    try {
      body = await res.json();
    } catch (e) {
      // no/!json body
    }
    if (!res.ok) {
      if (res.status === 400) {
        const err = new Error(body.statusMessage || body.message || 'Validation failed');
        err.fieldErrors = (body.data && body.data.errors) || {};
        throw err;
      }
      throw new Error(body.message || body.error || res.statusText);
    }
    return body;
  }

  async regenerateKeypair() {
    return this.call({
      method: 'post',
      path: '/server-settings/regenerate-keypair',
    });
  }
```

- [ ] **Step 2: Lint**

Run: `cd src && npm run lint`
Expected: no errors.

- [ ] **Step 3: Manual check (read-only)**

No UI yet. Confirm by reading that `updateServerSettings` reads field errors from `body.data.errors` and the other two delegate to `call()`. (End-to-end exercise lands in Task 3.)

- [ ] **Step 4: Commit**

```bash
git add src/www/js/api.js
git commit -m "feat(server-settings-ui): api.js methods (get/update/regenerate)"
```

---

### Task 2: App logic (`app.js`)

All reactive state, the pure client-side validator, computeds, and methods. No template yet — its end-to-end demo lands in Task 3; this task is verified by lint + code review.

**Files:**
- Modify: `src/www/js/app.js` — add a top-level `validateServerDraft` function (near `bytes`, ~line 26); add `data` fields (~line 53); add `computed` (~line 452); add `methods` (~line 162); add a `watch` block (the instance has none today).

**Interfaces:**
- Consumes: `this.api.getServerSettings/updateServerSettings/regenerateKeypair` (Task 1).
- Produces (used by the template in Tasks 3-6):
  - data: `view`, `serverSettings`, `serverDraft`, `serverErrors`, `serverLoading`, `serverSaving`, `serverSaveResult`, `regenerateConfirm`
  - methods: `openServerSettings()`, `closeServerSettings()`, `saveServerSettings()`, `confirmRegenerateKeypair()`, `fieldErr(field)`, `deepCopySettings(s)`
  - computed: `serverDirty`, `serverClientErrors`, `serverValid`, `serverCanSave`

- [ ] **Step 1: Add the top-level validator**

In `src/www/js/app.js`, after the `bytes(...)` function (around line 26), add:

```js
function svIsIPv4(s) {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(String(s).trim());
  return !!m && m.slice(1).every((o) => Number(o) >= 0 && Number(o) <= 255);
}
function svIsIPv6(s) {
  const t = String(s).trim();
  return /^[0-9a-fA-F:]+$/.test(t) && t.includes(':') && !/:::/.test(t) && (t.match(/:/g) || []).length <= 7;
}
function svIsIP(s) { return svIsIPv4(s) || svIsIPv6(s); }
function svIsCIDR(s) {
  const parts = String(s).trim().split('/');
  if (parts.length !== 2) return false;
  const p = Number(parts[1]);
  if (!Number.isInteger(p) || p < 0) return false;
  if (svIsIPv4(parts[0])) return p <= 32;
  if (svIsIPv6(parts[0])) return p <= 128;
  return false;
}
function svInt(v, lo, hi) {
  if (String(v).trim() === '') return false;
  const n = Number(v);
  return Number.isInteger(n) && n >= lo && n <= hi;
}
// Mirrors src/lib/serverSettings.js for instant inline UX; the backend stays authoritative.
function validateServerDraft(d, base) {
  const e = {};
  if (!d.host || String(d.host).trim() === '') e.host = 'Required';
  if (!svInt(d.port, 1, 65535)) e.port = 'Port 1–65535';
  if (!(d.mtu === null || d.mtu === '' || svInt(d.mtu, 576, 1500))) e.mtu = 'MTU 576–1500 or empty';
  if (!String(d.dns).split(',').every((x) => svIsIP(x.trim()))) e.dns = 'Comma-separated IPs';
  if (!String(d.allowedIPs).split(',').every((x) => svIsCIDR(x.trim()))) e.allowedIPs = 'Comma-separated CIDRs';
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.x$/.exec(String(d.defaultAddress));
  if (!m) {
    e.defaultAddress = 'Use a template like 10.8.0.x';
  } else if (base && base.defaultAddress) {
    const baseBase = String(base.defaultAddress).split('.').slice(0, 3).join('.');
    if (`${m[1]}.${m[2]}.${m[3]}` !== baseBase) e.defaultAddress = `Must stay in ${baseBase}.x`;
  }
  if (!svInt(d.persistentKeepalive, 0, 65535)) e.persistentKeepalive = 'Keepalive ≥ 0';
  if (!svInt(d.jc, 1, 128)) e.jc = 'Jc 1–128';
  ['jmin', 'jmax', 's1', 's2', 's3', 's4'].forEach((k) => { if (!svInt(d[k], 0, 1280)) e[k] = `${k} 0–1280`; });
  if (!e.jmin && !e.jmax && Number(d.jmin) > Number(d.jmax)) e.jmax = 'Jmax ≥ Jmin';
  ['h1', 'h2', 'h3', 'h4'].forEach((k) => {
    const h = d[k];
    if (!h || typeof h !== 'object' || !svInt(h.min, 5, 2147483647) || !svInt(h.max, 5, 2147483647) || Number(h.min) > Number(h.max)) {
      e[k] = `${k} min ≤ max`;
    }
  });
  return e;
}
```

- [ ] **Step 2: Add data fields**

In the `data: { ... }` object (after `copiedClientId: null,` ~line 69), add:

```js
    view: 'clients',
    serverSettings: null,
    serverDraft: null,
    serverErrors: {},
    serverLoading: false,
    serverSaving: false,
    serverSaveResult: null,
    regenerateConfirm: false,
```

- [ ] **Step 3: Add methods**

In `methods: { ... }` (after `toggleCharts()` ~line 372), add:

```js
    deepCopySettings(s) {
      return JSON.parse(JSON.stringify(s));
    },
    openServerSettings() {
      this.view = 'server-settings';
      this.serverErrors = {};
      this.serverSaveResult = null;
      this.serverLoading = true;
      this.api.getServerSettings()
        .then((s) => {
          this.serverSettings = s;
          this.serverDraft = this.deepCopySettings(s);
        })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => { this.serverLoading = false; });
    },
    closeServerSettings() {
      this.view = 'clients';
    },
    fieldErr(field) {
      return this.serverErrors[field] || this.serverClientErrors[field] || '';
    },
    saveServerSettings() {
      if (!this.serverCanSave) return;
      this.serverSaving = true;
      this.serverErrors = {};
      this.api.updateServerSettings(this.serverDraft)
        .then((res) => {
          this.serverSettings = res.settings;
          this.serverDraft = this.deepCopySettings(res.settings);
          this.serverSaveResult = { restarted: res.restarted, mustReimport: res.mustReimport };
        })
        .catch((err) => {
          if (err.fieldErrors) this.serverErrors = err.fieldErrors;
          else alert(err.message || err.toString());
        })
        .finally(() => { this.serverSaving = false; });
    },
    confirmRegenerateKeypair() {
      this.serverSaving = true;
      this.api.regenerateKeypair()
        .then((res) => {
          if (this.serverSettings) this.serverSettings.publicKey = res.publicKey;
          if (this.serverDraft) this.serverDraft.publicKey = res.publicKey;
          this.serverSaveResult = { restarted: true, mustReimport: true };
        })
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => { this.serverSaving = false; this.regenerateConfirm = false; });
    },
```

- [ ] **Step 4: Add computeds**

In `computed: { ... }` (after `theme()` ~line 479), add:

```js
    serverDirty() {
      return !!(this.serverSettings && this.serverDraft
        && JSON.stringify(this.serverSettings) !== JSON.stringify(this.serverDraft));
    },
    serverClientErrors() {
      if (!this.serverDraft) return {};
      return validateServerDraft(this.serverDraft, this.serverSettings);
    },
    serverValid() {
      return Object.keys(this.serverClientErrors).length === 0;
    },
    serverCanSave() {
      return this.serverDirty && this.serverValid && !this.serverSaving;
    },
```

- [ ] **Step 5: Add a watch block to clear stale backend errors on edit**

Add a top-level `watch` option to the Vue instance (place it next to `computed`, e.g. immediately before `computed: {`):

```js
  watch: {
    serverDraft: {
      deep: true,
      handler() {
        if (Object.keys(this.serverErrors).length) this.serverErrors = {};
        this.serverSaveResult = null;
      },
    },
  },
```

- [ ] **Step 6: Lint**

Run: `cd src && npm run lint`
Expected: no errors. (If ESLint flags the new top-level functions as unused, that is expected to resolve once `validateServerDraft` is referenced by the `serverClientErrors` computed added in Step 4 — confirm Step 4 was applied. `svIs*`/`svInt` are referenced by `validateServerDraft`.)

- [ ] **Step 7: Commit**

```bash
git add src/www/js/app.js
git commit -m "feat(server-settings-ui): app state, validator, load/save/regenerate logic"
```

---

### Task 3: Screen shell + NETWORK group (`index.html`)

CSS, the header server button, the `view` switch, the screen header (back/title/Save), restart notice, post-save hint, and the first working group — a complete vertical demo end-to-end.

**Files:**
- Modify: `src/www/index.html` — add CSS to the `<style>` block (after the `.awg-toolbar-btn` rules ~line 381); add the server button in the toolbar (~line 429, between the charts `<label>` and the logout `<span>`); wrap the existing content in the `view` switch; add the server-settings screen markup; close the wrapper.
- Modify: `src/www/js/i18n.js` — add the `serverSettings` key block under `messages.en`.

**Interfaces:**
- Consumes: Task 2's `view`, `openServerSettings`, `closeServerSettings`, `serverLoading`, `serverDraft`, `serverSaveResult`, `serverCanSave`, `saveServerSettings`, `fieldErr`.
- Produces: the `.awg-section-label`, `.awg-group-body`, `.awg-field`, `.awg-grid-2`, `.awg-input`, `.awg-field-label`, `.awg-field-helper`, `.awg-restart-notice`, `.awg-screen-header` classes used by Tasks 4-6.

- [ ] **Step 1: Add CSS**

In the `<style>` block, after the `.awg-toolbar-btn:hover { ... }` rule, add:

```css
  /* ── Server settings screen ─────────────────────────────────── */
  .awg-screen-header { display:flex; align-items:center; gap:12px; margin-bottom:16px; }
  .awg-section-label { font-size:11px; font-weight:700; text-transform:uppercase; color:var(--primary); letter-spacing:.12em; margin:0 0 7px 4px; }
  .awg-group { margin-bottom:12px; }
  .awg-group-body > * + * { border-top:1px solid var(--outline-variant); }
  .awg-field { display:flex; flex-direction:column; gap:4px; padding:9px 0; }
  .awg-grid-2 { display:grid; grid-template-columns:1fr 1fr; gap:12px; padding:9px 0; }
  .awg-grid-2 .awg-field { padding:0; border:none; }
  .awg-field-label { font-size:12px; color:var(--on-surface-variant); }
  .awg-field-helper { font-size:12px; color:var(--on-surface-variant); }
  .awg-field-helper.is-error { color:var(--error); }
  .awg-input { width:100%; background:var(--surface-container); border:1px solid var(--outline-variant); border-radius:12px; padding:12px; color:var(--on-surface); outline:none; box-sizing:border-box; transition:border-color .15s ease; }
  .awg-input:focus { border:2px solid var(--primary); padding:11px; }
  .awg-input.is-invalid { border-color:var(--error); }
  .awg-input.awg-mono { font-family:var(--font-mono); }
  .awg-restart-notice { display:flex; gap:10px; align-items:flex-start; background:var(--surface-container-highest); border:1px solid var(--outline-variant); border-radius:12px; padding:12px; font-size:13px; color:var(--on-surface-variant); margin-bottom:12px; }
  .awg-chip { display:inline-block; font-family:var(--font-mono); font-size:12px; background:var(--surface-container-highest); border:1px solid var(--outline-variant); border-radius:var(--radius-pill); padding:4px 10px; color:var(--on-surface); }
  .awg-chip .awg-chip-k { color:var(--on-surface-variant); }
  .awg-expander { width:100%; display:flex; align-items:center; justify-content:space-between; background:none; border:none; cursor:pointer; color:var(--on-surface); font-weight:600; padding:9px 0; }
  @media (max-width:520px) { .awg-grid-2 { grid-template-columns:1fr; } }
```

- [ ] **Step 2: Add the server-icon toolbar button**

In `index.html`, between the charts `<label v-if="uiChartType > 0">...</label>` (ends ~line 429) and the `<span v-if="requiresPassword" ... @click="logout">` (~line 430), insert:

```html
            <!-- Server settings -->
            <button @click="openServerSettings()"
              class="awg-toolbar-btn transition" :title="$t('serverSettings.title')">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5.25 14.25h13.5m-13.5 0a3 3 0 0 1-3-3m3 3a3 3 0 1 0 0 6h13.5a3 3 0 1 0 0-6m-16.5-3a3 3 0 0 1 3-3h13.5a3 3 0 0 1 3 3m-19.5 0a4.5 4.5 0 0 1 .9-2.7L5.737 5.1a3.375 3.375 0 0 1 2.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 0 1 .9 2.7m0 0a3 3 0 0 1-3 3m0 3h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Zm-3 6h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Z" />
              </svg>
            </button>
```

- [ ] **Step 3: Wrap existing content in the `view` switch**

The clients content begins at the update banner `<div v-if="latestRelease" ...>` (~line 442) and ends after the empty/loading states, right before the modals (`<div v-if="qrcode">` ~line 760). The header toolbar (lines 392-440) stays OUTSIDE the switch (persists across views).

Immediately BEFORE `<div v-if="latestRelease"` insert an opening wrapper:

```html
        <template v-if="view === 'clients'">
```

Immediately AFTER the client-list card/empty/loading block and BEFORE `<div v-if="qrcode">` insert the close + the server-settings view (full screen markup in Step 4):

```html
        </template>

        <template v-else-if="view === 'server-settings'">
          <!-- (server settings screen — Step 4) -->
        </template>
```

(The `qrcode`/`clientCreate`/`clientDelete` modals stay outside the switch so they keep working in the clients view.)

- [ ] **Step 4: Server-settings screen markup (header + notices + NETWORK)**

Replace the `<!-- (server settings screen — Step 4) -->` placeholder with:

```html
          <div class="awg-fade-in">
            <div class="awg-screen-header">
              <button @click="closeServerSettings()" class="awg-toolbar-btn" :title="$t('serverSettings.back')">
                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-5 h-5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 19.5 8.25 12l7.5-7.5" />
                </svg>
              </button>
              <h1 class="awg-title flex-grow dark:text-neutral-200" style="margin:0">{{$t('serverSettings.title')}}</h1>
              <button @click="saveServerSettings()" :disabled="!serverCanSave"
                class="awg-btn awg-btn-primary py-2 px-4" :class="{ 'is-disabled': !serverCanSave }">
                {{$t('serverSettings.save')}}
              </button>
            </div>

            <div v-if="serverLoading" class="p-5 text-sm" style="color:var(--on-surface-variant)">{{$t('serverSettings.loading')}}</div>

            <template v-else-if="serverDraft">
              <div class="awg-restart-notice">
                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5 flex-shrink-0">
                  <path stroke-linecap="round" stroke-linejoin="round" d="m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z" />
                </svg>
                <span>{{$t('serverSettings.restartNotice')}}</span>
              </div>
              <div v-if="serverSaveResult && serverSaveResult.mustReimport" class="awg-restart-notice" style="color:var(--on-surface)">
                <span>{{$t('serverSettings.reimportHint')}}</span>
              </div>

              <!-- NETWORK -->
              <div class="awg-group">
                <p class="awg-section-label">{{$t('serverSettings.network')}}</p>
                <div class="awg-card" style="padding:16px">
                  <div class="awg-group-body">
                    <div class="awg-field">
                      <label class="awg-field-label">{{$t('serverSettings.host')}}</label>
                      <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('host') }" v-model.trim="serverDraft.host" />
                      <span class="awg-field-helper" :class="{ 'is-error': fieldErr('host') }">{{ fieldErr('host') || $t('serverSettings.hostHelper') }}</span>
                    </div>
                    <div class="awg-grid-2">
                      <div class="awg-field">
                        <label class="awg-field-label">{{$t('serverSettings.port')}}</label>
                        <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('port') }" v-model.trim="serverDraft.port" />
                        <span v-if="fieldErr('port')" class="awg-field-helper is-error">{{ fieldErr('port') }}</span>
                      </div>
                      <div class="awg-field">
                        <label class="awg-field-label">{{$t('serverSettings.mtu')}}</label>
                        <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('mtu') }" v-model.trim="serverDraft.mtu" />
                        <span v-if="fieldErr('mtu')" class="awg-field-helper is-error">{{ fieldErr('mtu') }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </template>
          </div>
```

- [ ] **Step 5: Add i18n keys**

In `src/www/js/i18n.js`, inside `messages.en`, after the `theme: { ... },` line, add:

```js
    serverSettings: {
      title: 'Server settings',
      back: 'Back',
      save: 'Save',
      loading: 'Loading…',
      restartNotice: 'Saving restarts the WireGuard interface — connected clients reconnect automatically. Changing host or port means clients must re-download their config.',
      reimportHint: 'Saved. Existing clients must re-download their config to reconnect.',
      network: 'Network',
      host: 'Public host / endpoint',
      hostHelper: 'Hostname or IP clients dial',
      port: 'Listen port',
      mtu: 'MTU',
      clientDefaults: 'Client defaults',
      clientDefaultsHelper: 'Applied to new clients.',
      addressRange: 'Address range',
      keepalive: 'Persistent keepalive',
      dns: 'DNS servers',
      allowedIps: 'Allowed IPs',
      obfuscation: 'Obfuscation defaults',
      obfuscationHelper: 'Server-wide defaults — every client inherits these unless it sets its own.',
      advanced: 'Advanced parameters',
      keypair: 'Server keypair',
      publicKey: 'Public key',
      regenerate: 'Regenerate keypair',
      regenerateHelper: 'Rotates the server key and invalidates every client — they must reimport.',
      regenerateConfirmTitle: 'Regenerate server keypair?',
      regenerateConfirmBody: 'This rotates the server private key and invalidates every client peer. All clients must re-download their config to reconnect.',
      copyKey: 'Copy public key',
    },
```

- [ ] **Step 6: Lint + buildcss**

Run: `cd src && npm run lint` → no errors.
Run: `cd src && npm run buildcss` → completes; `git diff --stat src/www/css/app.css` should show no change (no new Tailwind utilities were introduced). If it changed, that is acceptable — commit the regenerated CSS too.

- [ ] **Step 7: Manual browser smoke (the end-to-end gate)**

With a running server (Linux/Docker, `wg-quick`): log in → click the new server icon in the toolbar → the "Server settings" screen opens, loads, and shows host/port/MTU populated. Edit MTU to `1400` → Save enables → click Save → no error, value persists (reopen shows 1400). Clear the port / set it to `70000` → the port input gets the `--error` outline, a helper appears, and Save is disabled. Back button returns to the client list.

- [ ] **Step 8: Commit**

```bash
git add src/www/index.html src/www/js/i18n.js src/www/css/app.css
git commit -m "feat(server-settings-ui): screen shell, view switch, NETWORK group"
```

---

### Task 4: CLIENT DEFAULTS group (`index.html`)

**Files:**
- Modify: `src/www/index.html` — add a group card after the NETWORK group's closing `</div>` (the `.awg-group` div), still inside the `v-else-if="view === 'server-settings'"` → `template v-else-if="serverDraft"` block.

**Interfaces:**
- Consumes: `serverDraft.defaultAddress/persistentKeepalive/dns/allowedIPs`, `fieldErr`. i18n keys `clientDefaults`, `clientDefaultsHelper`, `addressRange`, `keepalive`, `dns`, `allowedIps` (added in Task 3).

- [ ] **Step 1: Add the group markup**

Immediately after the NETWORK `<div class="awg-group">...</div>` block (before the closing `</template>`), add:

```html
              <!-- CLIENT DEFAULTS -->
              <div class="awg-group">
                <p class="awg-section-label">{{$t('serverSettings.clientDefaults')}}</p>
                <div class="awg-card" style="padding:16px">
                  <div class="awg-group-body">
                    <div class="awg-grid-2">
                      <div class="awg-field">
                        <label class="awg-field-label">{{$t('serverSettings.addressRange')}}</label>
                        <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('defaultAddress') }" v-model.trim="serverDraft.defaultAddress" />
                        <span v-if="fieldErr('defaultAddress')" class="awg-field-helper is-error">{{ fieldErr('defaultAddress') }}</span>
                      </div>
                      <div class="awg-field">
                        <label class="awg-field-label">{{$t('serverSettings.keepalive')}}</label>
                        <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('persistentKeepalive') }" v-model.trim="serverDraft.persistentKeepalive" />
                        <span v-if="fieldErr('persistentKeepalive')" class="awg-field-helper is-error">{{ fieldErr('persistentKeepalive') }}</span>
                      </div>
                    </div>
                    <div class="awg-field">
                      <label class="awg-field-label">{{$t('serverSettings.dns')}}</label>
                      <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('dns') }" v-model.trim="serverDraft.dns" />
                      <span v-if="fieldErr('dns')" class="awg-field-helper is-error">{{ fieldErr('dns') }}</span>
                    </div>
                    <div class="awg-field">
                      <label class="awg-field-label">{{$t('serverSettings.allowedIps')}}</label>
                      <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('allowedIPs') }" v-model.trim="serverDraft.allowedIPs" />
                      <span class="awg-field-helper" :class="{ 'is-error': fieldErr('allowedIPs') }">{{ fieldErr('allowedIPs') || $t('serverSettings.clientDefaultsHelper') }}</span>
                    </div>
                  </div>
                </div>
              </div>
```

- [ ] **Step 2: Lint + manual**

Run: `cd src && npm run lint` → no errors.
Manual (running server): open server settings → change DNS to `8.8.8.8, 1.1.1.1` → Save → succeeds; set AllowedIPs to `bad` → inline error + Save disabled; set address range to a different subnet base (e.g. `10.9.0.x`) → inline "Must stay in 10.8.0.x" error.

- [ ] **Step 3: Commit**

```bash
git add src/www/index.html
git commit -m "feat(server-settings-ui): CLIENT DEFAULTS group"
```

---

### Task 5: OBFUSCATION group with expander + chips (`index.html`, `app.js`)

**Files:**
- Modify: `src/www/js/app.js` — add `obfExpanded: false` to `data` (after `regenerateConfirm: false,`).
- Modify: `src/www/index.html` — add the OBFUSCATION group after CLIENT DEFAULTS.

**Interfaces:**
- Consumes: `serverDraft.jc/jmin/jmax/s1..s4`, `serverDraft.h1..h4` (`{min,max}`), `serverDraft.i1..i5`, `fieldErr`, `obfExpanded`. i18n keys `obfuscation`, `obfuscationHelper`, `advanced` (Task 3).

- [ ] **Step 1: Add `obfExpanded` state**

In `app.js` `data`, after `regenerateConfirm: false,` add:

```js
    obfExpanded: false,
```

- [ ] **Step 2: Add the OBFUSCATION group markup**

After the CLIENT DEFAULTS `<div class="awg-group">...</div>`, add:

```html
              <!-- OBFUSCATION DEFAULTS -->
              <div class="awg-group">
                <p class="awg-section-label">{{$t('serverSettings.obfuscation')}}</p>
                <div class="awg-card" style="padding:16px">
                  <div class="awg-group-body">
                    <div class="awg-field" style="gap:8px">
                      <button class="awg-expander" @click="obfExpanded = !obfExpanded">
                        <span>{{$t('serverSettings.advanced')}}</span>
                        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-4 h-4" :style="obfExpanded ? 'transform:rotate(180deg)' : ''">
                          <path stroke-linecap="round" stroke-linejoin="round" d="m19.5 8.25-7.5 7.5-7.5-7.5" />
                        </svg>
                      </button>
                      <div v-if="!obfExpanded" style="display:flex; flex-wrap:wrap; gap:6px">
                        <span class="awg-chip"><span class="awg-chip-k">Jc</span> {{serverDraft.jc}}</span>
                        <span class="awg-chip"><span class="awg-chip-k">S1</span> {{serverDraft.s1}}</span>
                        <span class="awg-chip"><span class="awg-chip-k">S2</span> {{serverDraft.s2}}</span>
                        <span class="awg-chip"><span class="awg-chip-k">H1</span> {{serverDraft.h1.min}}-{{serverDraft.h1.max}}</span>
                      </div>
                      <p v-else class="awg-field-helper">{{$t('serverSettings.obfuscationHelper')}}</p>
                    </div>
                    <div v-if="obfExpanded" class="awg-field">
                      <div class="awg-grid-2">
                        <div class="awg-field">
                          <label class="awg-field-label">Jc</label>
                          <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('jc') }" v-model.trim="serverDraft.jc" />
                          <span v-if="fieldErr('jc')" class="awg-field-helper is-error">{{ fieldErr('jc') }}</span>
                        </div>
                        <div class="awg-field">
                          <label class="awg-field-label">Jmin / Jmax</label>
                          <div class="awg-grid-2">
                            <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('jmin') }" v-model.trim="serverDraft.jmin" />
                            <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr('jmax') }" v-model.trim="serverDraft.jmax" />
                          </div>
                          <span v-if="fieldErr('jmin') || fieldErr('jmax')" class="awg-field-helper is-error">{{ fieldErr('jmin') || fieldErr('jmax') }}</span>
                        </div>
                      </div>
                      <div class="awg-grid-2" style="margin-top:12px">
                        <div class="awg-field" v-for="k in ['s1','s2','s3','s4']" :key="k">
                          <label class="awg-field-label">{{ k.toUpperCase() }}</label>
                          <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr(k) }" v-model.trim="serverDraft[k]" />
                          <span v-if="fieldErr(k)" class="awg-field-helper is-error">{{ fieldErr(k) }}</span>
                        </div>
                      </div>
                      <div class="awg-field" v-for="k in ['h1','h2','h3','h4']" :key="k" style="margin-top:12px">
                        <label class="awg-field-label">{{ k.toUpperCase() }} (min / max)</label>
                        <div class="awg-grid-2">
                          <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr(k) }" v-model.trim="serverDraft[k].min" />
                          <input class="awg-input awg-mono" :class="{ 'is-invalid': fieldErr(k) }" v-model.trim="serverDraft[k].max" />
                        </div>
                        <span v-if="fieldErr(k)" class="awg-field-helper is-error">{{ fieldErr(k) }}</span>
                      </div>
                      <div class="awg-field" v-for="k in ['i1','i2','i3','i4','i5']" :key="k" style="margin-top:12px">
                        <label class="awg-field-label">{{ k.toUpperCase() }}</label>
                        <input class="awg-input awg-mono" v-model.trim="serverDraft[k]" />
                      </div>
                    </div>
                  </div>
                </div>
              </div>
```

Note: `v-model.trim="serverDraft[k].min"` binds to the nested `{min,max}`; `serverDraft.h1` etc. are objects in the GET response, deep-copied by `deepCopySettings`, so they exist. The `i*` fields may be `null` — `v-model` on a null shows empty and sets a string on edit, which the backend accepts.

- [ ] **Step 3: Lint + manual**

Run: `cd src && npm run lint` → no errors.
Manual (running server): open server settings → collapsed shows the chips (Jc/S1/S2/H1) → click "Advanced parameters" → grid expands → change `S1` to a non-number → inline error + Save disabled; set `H1` min > max → inline error; set valid values → Save → succeeds and the obfuscation change triggers the reimport hint.

- [ ] **Step 4: Commit**

```bash
git add src/www/index.html src/www/js/app.js
git commit -m "feat(server-settings-ui): OBFUSCATION group with expander + chips"
```

---

### Task 6: SERVER KEYPAIR group + regenerate danger modal (`index.html`)

**Files:**
- Modify: `src/www/index.html` — add the KEYPAIR group after OBFUSCATION; add a regenerate confirm modal alongside the existing modals (after the Delete modal `</div>` ~line 869, still inside the `v-if="authenticated === true"` block which closes at line 870).

**Interfaces:**
- Consumes: `serverDraft.publicKey`, `copyToClipboard` (existing), `copiedClientId`/a copy affordance, `regenerateConfirm`, `confirmRegenerateKeypair`, `serverSaving`. i18n keys `keypair`, `publicKey`, `regenerate`, `regenerateHelper`, `regenerateConfirmTitle`, `regenerateConfirmBody`, `copyKey`, `cancel` (existing).

- [ ] **Step 1: Add the KEYPAIR group markup**

After the OBFUSCATION `<div class="awg-group">...</div>` (still before the closing `</template>` of `v-else-if="serverDraft"`), add:

```html
              <!-- SERVER KEYPAIR -->
              <div class="awg-group">
                <p class="awg-section-label">{{$t('serverSettings.keypair')}}</p>
                <div class="awg-card" style="padding:16px">
                  <div class="awg-group-body">
                    <div class="awg-field">
                      <label class="awg-field-label">{{$t('serverSettings.publicKey')}}</label>
                      <div style="display:flex; gap:8px; align-items:center">
                        <input class="awg-input awg-mono" :value="serverDraft.publicKey" readonly style="flex:1; overflow:hidden; text-overflow:ellipsis" />
                        <button class="awg-toolbar-btn" :title="$t('serverSettings.copyKey')" @click="copyToClipboard(serverDraft.publicKey)">
                          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M15.666 3.888A2.25 2.25 0 0 0 13.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 0 1-.75.75H9a.75.75 0 0 1-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 0 1-2.25 2.25H6.75A2.25 2.25 0 0 1 4.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 0 1 1.927-.184" />
                          </svg>
                        </button>
                      </div>
                    </div>
                    <div class="awg-field">
                      <button class="awg-btn awg-btn-text" style="border:1px solid var(--error); color:var(--error); align-self:flex-start; padding:8px 14px" @click="regenerateConfirm = true">
                        {{$t('serverSettings.regenerate')}}
                      </button>
                      <span class="awg-field-helper">{{$t('serverSettings.regenerateHelper')}}</span>
                    </div>
                  </div>
                </div>
              </div>
```

- [ ] **Step 2: Add the regenerate confirm modal**

After the Delete-dialog `</div>` that closes at ~line 869 (and before the `</div>` at ~line 870 that closes `v-if="authenticated === true"`), add a danger modal mirroring the Delete pattern:

```html
        <!-- Regenerate keypair confirm -->
        <div v-if="regenerateConfirm" class="fixed z-10 inset-0 overflow-y-auto">
          <div class="flex items-center justify-center min-h-screen pt-4 px-4 pb-20 text-center sm:block sm:p-0">
            <div class="awg-modal-overlay fixed inset-0 transition-opacity" aria-hidden="true"></div>
            <span class="hidden sm:inline-block sm:align-middle sm:h-screen" aria-hidden="true">&#8203;</span>
            <div class="awg-modal inline-block align-bottom text-left overflow-hidden transform transition-all sm:my-8 sm:align-middle sm:max-w-lg w-full"
              role="dialog" aria-modal="true" aria-labelledby="modal-headline">
              <div class="px-4 pt-5 pb-4 sm:p-6 sm:pb-4">
                <div class="sm:flex sm:items-start">
                  <div class="awg-danger-well mx-auto flex-shrink-0 flex items-center justify-center h-12 w-12 sm:mx-0 sm:h-10 sm:w-10">
                    <svg class="h-6 w-6" style="color: var(--on-error-container)" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                    </svg>
                  </div>
                  <div class="mt-3 text-center sm:mt-0 sm:ml-4 sm:text-left">
                    <h3 class="text-lg leading-6 font-semibold text-gray-900 dark:text-neutral-200" id="modal-headline">
                      {{$t("serverSettings.regenerateConfirmTitle")}}
                    </h3>
                    <div class="mt-2">
                      <p class="text-sm text-gray-500 dark:text-neutral-300">{{$t("serverSettings.regenerateConfirmBody")}}</p>
                    </div>
                  </div>
                </div>
              </div>
              <div class="awg-modal-footer px-4 py-3 sm:px-6 sm:flex sm:flex-row-reverse">
                <button type="button" :disabled="serverSaving" @click="confirmRegenerateKeypair()"
                  class="awg-btn awg-btn-danger w-full inline-flex justify-center shadow-sm px-4 py-2 text-base font-medium sm:ml-3 sm:w-auto sm:text-sm">
                  {{$t("serverSettings.regenerate")}}
                </button>
                <button type="button" @click="regenerateConfirm = false"
                  class="awg-btn awg-btn-text mt-3 w-full inline-flex justify-center shadow-sm px-4 py-2 text-base sm:mt-0 sm:ml-3 sm:w-auto sm:text-sm">
                  {{$t("cancel")}}
                </button>
              </div>
            </div>
          </div>
        </div>
```

- [ ] **Step 3: Lint + manual**

Run: `cd src && npm run lint` → no errors.
Manual (running server): open server settings → KEYPAIR card shows the public key (truncated) → copy icon copies it → click "Regenerate keypair" → danger modal appears with the explicit warning → Cancel dismisses; Regenerate rotates the key (the displayed public key changes) and shows the reimport hint.

- [ ] **Step 4: Commit**

```bash
git add src/www/index.html
git commit -m "feat(server-settings-ui): SERVER KEYPAIR group + regenerate confirm modal"
```

---

## Self-Review

**Spec coverage:**
- §1 view switch (flag, server button, full-view, back) → Task 2 (state/methods) + Task 3 (button, wrapper, header). ✅
- §2 state fields → Task 2 Step 2 (+ `obfExpanded` Task 5). ✅
- §3 api.js three methods incl. 400 `data.errors` parse → Task 1. ✅
- §4 data flow (load → draft → dirty/valid → save → 400 inline → regenerate) → Task 2 computeds/methods, wired in Tasks 3-6. ✅
- §5 client validation mirror (all fields, same-/24, IPv6/CIDR browser checks) → Task 2 `validateServerDraft`. ✅
- §6 layout/components: NETWORK (T3), CLIENT DEFAULTS (T4), OBFUSCATION expander+chips (T5), KEYPAIR + regenerate danger modal (T6); restart notice + post-save hint (T3). ✅
- §7 i18n English-first keys → Task 3 Step 5 (block covers all groups). ✅
- §8 testing: lint + manual smoke per task; buildcss no-op check (T3). ✅
- Out of scope (WEB PANEL, client-detail view) → not present. ✅

**Placeholder scan:** No TBD/TODO. The `<!-- (server settings screen — Step 4) -->` marker in Task 3 Step 3 is explicitly replaced in Step 4. All code blocks are complete and copy-pasteable.

**Type consistency:** `view`, `serverSettings`, `serverDraft`, `serverErrors`, `serverLoading`, `serverSaving`, `serverSaveResult`, `regenerateConfirm`, `obfExpanded`; methods `openServerSettings`/`closeServerSettings`/`saveServerSettings`/`confirmRegenerateKeypair`/`fieldErr`/`deepCopySettings`; computeds `serverDirty`/`serverClientErrors`/`serverValid`/`serverCanSave`; api `getServerSettings`/`updateServerSettings`(+`.fieldErrors`)/`regenerateKeypair` — all names consistent across producing (Task 1/2) and consuming (Task 3-6) tasks. `serverDraft.h1..h4` are `{min,max}`; `i1..i5` may be null. Backend field names (`defaultAddress`, `persistentKeepalive`, `allowedIPs`) match `serverDraft.*` bindings and `fieldErr` keys.

**Note for the executor:** Tasks 1-2 have no standalone manual demo (no template yet); their gate is lint + code review, and the first true end-to-end smoke is Task 3 Step 7. This matches the repo's no-frontend-test convention; the client-side validator is intentionally lenient (browser IPv6 check) with the backend as the authority.
