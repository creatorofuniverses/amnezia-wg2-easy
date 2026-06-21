# Per-Client Legacy (No-Imitation) Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persisted per-client "legacy" flag that, when set, shapes every config the server emits for that client (download, QR, awg://v1 share-string) to omit the imitation-only keys.

**Architecture:** A pure `stripImitationKeys(conf)` text transform (drop `ImitateProtocol`; drop `I1–I5` whose value contains `<`); a per-client `legacy` boolean in `wg0.json`; `getClientConfiguration` applies the transform when the flag is set, so all three export paths inherit it with no export-button changes; an enable/disable-style toggle (method + two routes + a per-client icon-toggle in the UI).

**Tech Stack:** Node 18+ (CommonJS, `node:test`), H3 routes, Vue 2 (vendored). No new dependencies.

## Global Constraints

- **Legacy target = AmneziaWG 2.0 *without* imitation support** (NOT 1.x). Both sides share all base params; only the imitation layer differs.
- **Strip rule:** always remove `ImitateProtocol = …`; remove an `I1`–`I5` line **iff its value contains a `<` character**; keep raw-string `I`-params and **everything else** (`Jc/Jmin/Jmax`, `S1`–`S4`, `H1`–`H4` ranges, all standard WireGuard fields).
- **Client-config-only:** the server's own `wg0.conf`/runtime params are NOT changed.
- **Default `false`:** new and pre-existing clients are non-legacy (full config) unless toggled; a missing `legacy` field reads as `false` (no migration).
- **No new export surface:** no `?variant=` params, no separate legacy endpoints; the per-client flag is the single source of truth and `getClientConfiguration` is the single application point.
- **No new dependencies.** Node stdlib only. ESLint (`npm run lint`) must stay clean.
- **Automated tests scoped to `stripImitationKeys`** (`node:test`, per app convention + #3 precedent); the backend flag/routes/hook and the UI follow the app's manual-verification convention.

## File Structure

- **Create:** `src/lib/stripImitationKeys.js` — pure transform.
- **Create:** `src/lib/__tests__/stripImitationKeys.test.js` — `node:test` suite.
- **Modify:** `src/lib/WireGuard.js` — require the transform; `legacy: false` in `createClient`; `legacy` in the `getClients` map; apply the transform in `getClientConfiguration`; add `setClientLegacy`.
- **Modify:** `src/lib/Server.js` — two `legacy/{enable,disable}` routes (with proto guard).
- **Modify:** `src/www/js/api.js` — `enableClientLegacy` / `disableClientLegacy`.
- **Modify:** `src/www/js/app.js` — `toggleClientLegacy` handler.
- **Modify:** `src/www/index.html` — the legacy icon-toggle in the row action group + one active-state CSS rule.
- **Modify:** `src/www/js/i18n.js` — two tooltip keys.

## Verification commands

```bash
cd src && npm test        # node --test — stripImitationKeys suite (Task 1)
cd src && npm run lint    # ESLint — must stay clean (all tasks)
cd src && npm run buildcss # rebuild Tailwind (Task 3)
cd src && npm run serve   # dev server for manual checks (Tasks 2–3)
```

---

### Task 1: `stripImitationKeys` transform + node:test

**Files:**
- Create: `src/lib/stripImitationKeys.js`
- Create: `src/lib/__tests__/stripImitationKeys.test.js`

**Interfaces:**
- Produces: `stripImitationKeys(confText: string): string` (CommonJS `module.exports = { stripImitationKeys }`). Task 2 consumes it.

- [ ] **Step 1: Write the failing test**

Create `src/lib/__tests__/stripImitationKeys.test.js`:

```js
'use strict';

const { test } = require('node:test');
const assert = require('node:assert');

const { stripImitationKeys } = require('../stripImitationKeys');

const full = [
  '[Interface]',
  'PrivateKey = abc=',
  'Address = 10.8.0.2/32',
  'Jc = 4', 'Jmin = 40', 'Jmax = 70',
  'S1 = 0', 'S2 = 0', 'S3 = 0', 'S4 = 0',
  'H1 = 100-500', 'H2 = 600-900', 'H3 = 1000-1500', 'H4 = 1600-2000',
  'ImitateProtocol = quic',
  'I1 = <qinit www.google.com>',
  'I2 = b0xdeadbeef',
  '',
  '[Peer]',
  'PublicKey = def=',
  'AllowedIPs = 0.0.0.0/0',
  'Endpoint = 1.2.3.4:51820',
].join('\n');

test('removes the ImitateProtocol line', () => {
  assert.ok(!stripImitationKeys(full).includes('ImitateProtocol'));
});

test('removes an I-param with an angle-bracket tag, keeps a raw I-param', () => {
  const out = stripImitationKeys(full);
  assert.ok(!out.includes('<qinit'), 'I1 (tag) should be stripped');
  assert.ok(out.includes('I2 = b0xdeadbeef'), 'I2 (raw) should be kept');
});

test('keeps base obfuscation, peer, and standard fields untouched', () => {
  const out = stripImitationKeys(full);
  for (const keep of [
    'Jc = 4', 'S3 = 0', 'S4 = 0', 'H1 = 100-500',
    'PrivateKey = abc=', 'PublicKey = def=',
    'AllowedIPs = 0.0.0.0/0', 'Endpoint = 1.2.3.4:51820',
  ]) {
    assert.ok(out.includes(keep), `should keep: ${keep}`);
  }
});

test('a config with no imitation keys is returned unchanged', () => {
  const plain = '[Interface]\nPrivateKey = abc=\nS1 = 0\n\n[Peer]\nPublicKey = def=\n';
  assert.strictEqual(stripImitationKeys(plain), plain);
});

test('is idempotent', () => {
  assert.strictEqual(stripImitationKeys(stripImitationKeys(full)), stripImitationKeys(full));
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src && npm test`
Expected: FAIL — `Cannot find module '../stripImitationKeys'`.

- [ ] **Step 3: Write the transform**

Create `src/lib/stripImitationKeys.js`:

```js
'use strict';

// Shape a client config for a "legacy" (non-imitation) AmneziaWG 2.0 client:
// drop the `ImitateProtocol` line and any `I1`-`I5` line whose value contains an
// angle-bracket imitation tag (e.g. `<qinit www.google.com>`). Raw-string
// I-params (no `<`) and every other line are kept verbatim.
function stripImitationKeys(confText) {
  return confText
    .split('\n')
    .filter((line) => {
      const m = line.match(/^\s*(ImitateProtocol|I[1-5])\s*=\s*(.*)$/);
      if (!m) return true; // not an imitation-related line — keep
      if (m[1] === 'ImitateProtocol') return false; // always drop
      return !m[2].includes('<'); // drop the I-param only if it has a tag
    })
    .join('\n');
}

module.exports = { stripImitationKeys };
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd src && npm test`
Expected: PASS — the `stripImitationKeys` tests pass (and the existing `awgShareString` suite still passes).

- [ ] **Step 5: Lint**

Run: `cd src && npm run lint`
Expected: clean (exit 0).

- [ ] **Step 6: Commit**

```bash
git add src/lib/stripImitationKeys.js src/lib/__tests__/stripImitationKeys.test.js
git commit -m "feat(legacy-toggle): stripImitationKeys() transform + node:test"
```

---

### Task 2: Backend — legacy flag, generation hook, toggle persistence

**Files:**
- Modify: `src/lib/WireGuard.js` (require transform; `createClient`; `getClients`; `getClientConfiguration`; `setClientLegacy`)
- Modify: `src/lib/Server.js` (two routes after the `disable` route, ~line 197)

**Interfaces:**
- Consumes: `stripImitationKeys` from Task 1; existing `getClient`, `saveConfig`, `getConfig`.
- Produces: persisted `client.legacy` (boolean); `getClients()` output includes `legacy`; `WireGuard.setClientLegacy({ clientId, legacy }): Promise<void>`; `POST /api/wireguard/client/:clientId/legacy/{enable,disable}`.

- [ ] **Step 1: Require the transform in WireGuard.js**

At the top of `src/lib/WireGuard.js`, alongside the existing requires (e.g. after `const ShareString = require('./awgShareString');` added in #3), add:

```js
const { stripImitationKeys } = require('./stripImitationKeys');
```

- [ ] **Step 2: Default the flag in `createClient`**

In `src/lib/WireGuard.js`, in the `createClient` client object (the literal ending `enabled: true,`), add a `legacy` field:

```js
    const client = {
      id,
      name,
      address,
      privateKey,
      publicKey,
      preSharedKey,

      createdAt: new Date(),
      updatedAt: new Date(),

      enabled: true,
      legacy: false,
    };
```

- [ ] **Step 3: Expose the flag in `getClients`**

In `src/lib/WireGuard.js`, in the `getClients()` mapped object (which lists `id`, `name`, `enabled`, …, `transferTx`), add a `legacy` field after `enabled`:

```js
      id: clientId,
      name: client.name,
      enabled: client.enabled,
      legacy: client.legacy === true,
      address: client.address,
```

- [ ] **Step 4: Apply the transform in `getClientConfiguration`**

In `src/lib/WireGuard.js`, `getClientConfiguration` currently does `const client = await this.getClient({ clientId });` then `return \`<template>\`;`. Capture the template in a `const` and return it conditionally. Change the trailing `return \`` to `const conf = \`` and replace the final line `Endpoint = ${WG_HOST}:${WG_PORT}\`;` with:

```js
Endpoint = ${WG_HOST}:${WG_PORT}`;
    return client.legacy ? stripImitationKeys(conf) : conf;
  }
```

(The `client` is already fetched at the top of the method — reuse it; do not fetch again.)

- [ ] **Step 5: Add `setClientLegacy` (mirrors `enableClient`/`disableClient`)**

In `src/lib/WireGuard.js`, immediately after the `disableClient` method (~line 376), add:

```js
  async setClientLegacy({ clientId, legacy }) {
    const client = await this.getClient({ clientId });

    client.legacy = !!legacy;
    client.updatedAt = new Date();

    await this.saveConfig();
  }
```

- [ ] **Step 6: Add the two routes**

In `src/lib/Server.js`, immediately after the `.post('/api/wireguard/client/:clientId/disable', …)` handler block (ends ~line 197), add (mirroring its proto guard exactly):

```js
      .post('/api/wireguard/client/:clientId/legacy/enable', defineEventHandler(async (event) => {
        const clientId = getRouterParam(event, 'clientId');
        if (clientId === '__proto__' || clientId === 'constructor' || clientId === 'prototype') {
          throw createError({ status: 403 });
        }
        await WireGuard.setClientLegacy({ clientId, legacy: true });
        return { success: true };
      }))
      .post('/api/wireguard/client/:clientId/legacy/disable', defineEventHandler(async (event) => {
        const clientId = getRouterParam(event, 'clientId');
        if (clientId === '__proto__' || clientId === 'constructor' || clientId === 'prototype') {
          throw createError({ status: 403 });
        }
        await WireGuard.setClientLegacy({ clientId, legacy: false });
        return { success: true };
      }))
```

- [ ] **Step 7: Lint + test regression**

```bash
cd src && npm run lint && npm test
```
Expected: lint clean; all `node:test` suites pass (Task 1 + the #3 `awgShareString` suite) — confirms the require + edits didn't break anything.

- [ ] **Step 8: Manual verification (no H3/WireGuard test harness — app convention)**

The dev server needs `NET_ADMIN`/`wg-quick`, which may be unavailable in the build sandbox. If you can run it:

```bash
cd src && npm run serve   # http://localhost:51821 (no password in `serve`)
```
Create a client; note its id (`curl -s http://localhost:51821/api/wireguard/client | head -c 400`). Then:

```bash
ID=<client-id>
curl -s "http://localhost:51821/api/wireguard/client/$ID/configuration" | grep -E "ImitateProtocol|^I[1-5] ="   # full: imitation lines present (if server has them)
curl -s -X POST "http://localhost:51821/api/wireguard/client/$ID/legacy/enable"
curl -s "http://localhost:51821/api/wireguard/client/$ID/configuration" | grep -E "ImitateProtocol|<"            # legacy: ImitateProtocol + <…> I-params gone; base params (Jc/S/H) still present
curl -s "http://localhost:51821/api/wireguard/client" | grep -o '"legacy":[a-z]*' | head -1                     # "legacy":true
curl -s -X POST "http://localhost:51821/api/wireguard/client/$ID/legacy/disable"                                # toggles back
```
Expected: with legacy on, `ImitateProtocol` and any `<…>` `I`-params are absent while base obfuscation remains; `getClients` reports `"legacy":true`; toggling off restores them. If the server can't start here, state that honestly in your report — the strip logic itself is covered by Task 1's tests and the hook is a one-line conditional.

- [ ] **Step 9: Commit**

```bash
git add src/lib/WireGuard.js src/lib/Server.js
git commit -m "feat(legacy-toggle): per-client legacy flag, config-shaping hook, toggle routes"
```

---

### Task 3: Frontend — per-client legacy toggle

**Files:**
- Modify: `src/www/js/api.js` (two methods)
- Modify: `src/www/js/app.js` (`toggleClientLegacy`)
- Modify: `src/www/index.html` (icon-toggle button + one CSS rule)
- Modify: `src/www/js/i18n.js` (two keys)

**Interfaces:**
- Consumes: the `legacy/{enable,disable}` routes (Task 2); `client.legacy`, `client.id` from `getClients`; the `.awg-icon-btn` class.
- Produces: a user-facing per-client toggle.

- [ ] **Step 1: Add the API methods**

In `src/www/js/api.js`, after `disableClient` (mirroring it), add:

```js
  async enableClientLegacy({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/legacy/enable`,
    });
  }

  async disableClientLegacy({ clientId }) {
    return this.call({
      method: 'post',
      path: `/wireguard/client/${clientId}/legacy/disable`,
    });
  }
```

- [ ] **Step 2: Add the toggle handler**

In `src/www/js/app.js`, in `methods`, after `disableClient` (~line 332), add:

```js
    toggleClientLegacy(client) {
      const req = client.legacy
        ? this.api.disableClientLegacy({ clientId: client.id })
        : this.api.enableClientLegacy({ clientId: client.id });
      req
        .catch((err) => alert(err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
```

- [ ] **Step 3: Add the active-state CSS rule**

In `src/www/index.html`, in the `<style>` block immediately after the `.awg-icon-btn:hover` rule (~line 210), add:

```css
  .awg-icon-btn--active { background-color: var(--primary-container) !important; color: var(--on-primary-container) !important; }
```

- [ ] **Step 4: Add the toggle button to the row action group**

In `src/www/index.html`, insert this button between the `<!-- Copy share link -->` button and the `<!-- Delete -->` button (i.e. right before the `<!-- Delete -->` comment, ~line 697):

```html
                    <!-- Legacy (no-imitation) toggle -->
                    <button
                      class="awg-icon-btn align-middle transition"
                      :class="{ 'awg-icon-btn--active': client.legacy }"
                      :title="client.legacy ? $t('legacyOn') : $t('legacyOff')"
                      @click="toggleClientLegacy(client)">
                      <svg class="w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
                      </svg>
                    </button>
```

(The icon is heroicons "eye-slash" — reads as "imitation hidden/stripped"; it highlights via `--active` when the client is legacy. Swap the path if you prefer a different glyph; the meaning lives in the tooltip.)

- [ ] **Step 5: Add the i18n keys**

In `src/www/js/i18n.js`, alongside the existing `showQR`/`downloadConfig` keys **in every locale object** (English fallback acceptable), add:

```js
    legacyOff: 'Legacy mode: off (full imitation config)',
    legacyOn: 'Legacy mode: on (imitation keys stripped)',
```

- [ ] **Step 6: Build CSS and lint**

```bash
cd src && npm run buildcss && npm run lint
```
Expected: buildcss succeeds; lint clean.

- [ ] **Step 7: Manual verification**

```bash
cd src && npm run serve   # http://localhost:51821
```
- Each client row shows the eye-slash toggle, between the copy-share-link and delete icons.
- Click it: the icon highlights (active state) and the title flips to "Legacy mode: on …". Download that client's config (existing download button) → `ImitateProtocol` and `<…>` `I`-params are absent. Click again → highlight clears and the full config returns.
- If the dev server can't start here (needs `NET_ADMIN`), verify by reading that the button is bound to `client.legacy`, calls `toggleClientLegacy`, and the active class toggles — and note the env limitation.

- [ ] **Step 8: Commit**

```bash
git add src/www/js/api.js src/www/js/app.js src/www/index.html src/www/js/i18n.js src/www/css/app.css
git commit -m "feat(legacy-toggle): per-client legacy icon-toggle in the client row"
```

---

## Self-Review

**Spec coverage:** `stripImitationKeys` transform + rule (T1) · `node:test` (T1) · per-client `legacy` flag default-false + `getClients` exposure (T2 S2–S3) · generation hook so all exports inherit (T2 S4) · `setClientLegacy` + enable/disable-style routes with proto guard (T2 S5–S6) · API methods + toggle handler + row icon-toggle + active state + i18n (T3) · client-config-only / no server change (no server-side edits anywhere) · no new export surface (no `?variant=`, exports untouched). Deferred items (1.x support, plain-WG strip, styled QR) intentionally absent. ✓

**Placeholder scan:** No TBD/TODO; every code step has complete content. The two manual-verification steps (T2 S8, T3 S7) are concrete commands/observations with an explicit env-limitation fallback, not placeholders — automated tests are spec-scoped to `stripImitationKeys`. ✓

**Type/name consistency:** `stripImitationKeys` (T1) → consumed in `WireGuard.js` (T2 S1, S4). `client.legacy` written (T2 S2, S5), exposed (T2 S3), read by the hook (T2 S4) and the UI (`client.legacy`, T3 S2/S4). `setClientLegacy({clientId, legacy})` (T2 S5) ← routes (T2 S6). `enableClientLegacy`/`disableClientLegacy` (T3 S1) ← `toggleClientLegacy` (T3 S2) ← button (T3 S4). i18n `legacyOff`/`legacyOn` (T3 S5) ← button title (T3 S4). `.awg-icon-btn--active` defined (T3 S3) ← bound (T3 S4). All consistent. ✓

**Risk note:** The detection rule is "value contains `<`" — if a raw (keep) `I`-param could legitimately contain `<`, it would be wrongly stripped; the spec records this and the rule was the user's explicit choice. The generation hook reuses the already-fetched `client` (no extra `getClient`), and all three exports inherit the flag through the single `getClientConfiguration` application point.
