# awg://v1 Share-String (Export) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `awg://v1/` config share-string (export only) so a client can be imported into the AmneziaWG Android app via a copyable link.

**Architecture:** A pure Node codec (`zlib` + base64url) encodes the client `.conf` the app already generates, prefixed `awg://v1/`. A backend method + endpoint returns it; a "Copy share link" button in the client row copies it (with a non-secure-context clipboard fallback). Wire format and cross-compat verified against the AmneziaWG Android `ConfigShare` reference (`org.amnezia.awg.config.ConfigShare`) and its `share-vector.{awg,conf}` test vectors.

**Tech Stack:** Node 18+ (CommonJS, stdlib `zlib`/`Buffer`, `node:test`), H3 routes, Vue 2 (vendored), no new dependencies.

## Global Constraints

- **Export/generate only.** No import/decode UI or route. `decode()` is implemented in the codec **for tests only**; it is never wired to a route or the frontend.
- **No new dependencies.** Node stdlib only (`zlib`, `Buffer`, `node:test`, `node:assert`, `node:fs`, `node:path`).
- **Wire format (exact):** `awg://v1/<base64url( zlib( utf8(conf) ) )>`. zlib = RFC 1950 (`zlib.deflateSync` with `level: Z_BEST_COMPRESSION`). base64url = RFC 4648 §5, **no padding** on output; decode tolerates the standard `+/` alphabet and `=` padding.
- **Cross-compat is verified via `decode`, never byte-exact `encode`.** Java and Node deflate emit different bytes for the same input; both inflate to identical plaintext. No test may assert `encode(x) === <android vector>`.
- **Automated tests are scoped to the codec** (`awgShareString.js`). The backend method, route, and UI follow the app's existing manual-verification convention (the Node app has no other test suite) — each such task lists concrete manual verification commands. ESLint must stay clean (`npm run lint`).
- **Name sanitization:** strip CR/LF from the user-controlled client name before embedding it in the `# Name = …` comment.
- **Reuse `downloadableConfig`:** the UI gates the button on `client.downloadableConfig`, exactly like the existing QR/download controls. The endpoint mirrors the existing `configuration` route's behavior (no new server gate).
- **Secret handling:** the share-string contains the client PrivateKey, identical to the existing QR/download paths; it is served behind the same session auth and is not encrypted.

## File Structure

- **Create:** `src/lib/awgShareString.js` — codec (`encode`, `decode`), stdlib only.
- **Create:** `src/lib/__tests__/awgShareString.test.js` — `node:test` suite.
- **Create:** `src/lib/__tests__/fixtures/share-vector.awg`, `share-vector.conf` — vendored Android reference vectors (self-contained; no external path dependency at test time).
- **Modify:** `src/lib/WireGuard.js` — add `getClientShareString({ clientId })`; require the codec.
- **Modify:** `src/lib/Server.js` — add the `GET …/share-string` route.
- **Modify:** `src/www/js/api.js` — add `getClientShareString({ clientId })` (text fetch).
- **Modify:** `src/www/js/app.js` — `copiedClientId` data field + `copyShareLink`/`copyToClipboard` methods.
- **Modify:** `src/www/index.html` — "Copy share link" icon button in the client-row action group.
- **Modify:** `src/www/js/i18n.js` — `copyShareLink` + `copied` keys.
- **Modify:** `src/package.json` — `"test": "node --test"`.

## Verification commands

```bash
cd src && npm test          # node --test — codec suite (Task 1)
cd src && npm run lint       # ESLint — must stay clean (all tasks)
cd src && npm run serve      # dev server for manual checks (Tasks 2–3)
```

---

### Task 1: `awgShareString.js` codec + node:test suite

**Files:**
- Create: `src/lib/awgShareString.js`
- Create: `src/lib/__tests__/awgShareString.test.js`
- Create: `src/lib/__tests__/fixtures/share-vector.awg`, `src/lib/__tests__/fixtures/share-vector.conf`
- Modify: `src/package.json` (add `test` script)

**Interfaces:**
- Produces: `encode(confText: string): string` and `decode(shareString: string): string` from `src/lib/awgShareString.js` (CommonJS `module.exports = { encode, decode }`). Task 2 consumes `encode`.

- [ ] **Step 1: Vendor the reference fixtures**

Prefer copying the authoritative files from the local AmneziaWG Android checkout (avoids any transcription error), then verify the byte count:

```bash
mkdir -p src/lib/__tests__/fixtures
SRC=~/projects/vpn/amneziawg-android/tunnel/src/test/resources
if [ -f "$SRC/share-vector.awg" ]; then
  cp "$SRC/share-vector.awg" "$SRC/share-vector.conf" src/lib/__tests__/fixtures/
fi
wc -c src/lib/__tests__/fixtures/share-vector.conf   # expect 266
```

If that path is absent, create the two files by hand with EXACTLY the content below.

Create `src/lib/__tests__/fixtures/share-vector.conf` with EXACTLY this content (note the leading `# Name` comment and trailing newline; 266 bytes):

```
# Name = awg-fi-01
[Interface]
PrivateKey = aP8A1234567890abcdefghijklmnopqrstuvwxyzABC=
Address = 10.8.0.2/32
DNS = 1.1.1.1
Jc = 4
Jmin = 40
Jmax = 70
[Peer]
PublicKey = bQ9B1234567890abcdefghijklmnopqrstuvwxyzABC=
Endpoint = 192.0.2.1:51820
AllowedIPs = 0.0.0.0/0
```

Create `src/lib/__tests__/fixtures/share-vector.awg` with EXACTLY this one-line string (it may have a trailing newline; the test trims):

```
awg://v1/eNqNjk2PgjAQhu_zK0g8U6fFDzDxALt7UBOD8Wj2UOigVShuQdH99dvqH9i8l2fezOSZUbCVDQXLQA7HsNIhcjisTE-2kiV9Q271Xfa0oadfyeOUi2gync3jBGVRKqqOJ32-1I1prz-262_34fH8TbOPJaRKWeo6d8aRxQyZGEcCPrd737BXYF26YQLrRhsP6Eg-HM0RDjmRdf5bUevyrS92SfZv_ZdR11ab3tsS4fWML6Y8FghpXbcDqVXun0P2yhjhD8wwTkQ
```

- [ ] **Step 2: Write the failing test**

Create `src/lib/__tests__/awgShareString.test.js`:

```js
'use strict';

const { test } = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const { encode, decode } = require('../awgShareString');

const FIXTURES = path.join(__dirname, 'fixtures');
const vectorAwg = fs.readFileSync(path.join(FIXTURES, 'share-vector.awg'), 'utf8').trim();
const vectorConf = fs.readFileSync(path.join(FIXTURES, 'share-vector.conf'), 'utf8');

const sampleConf = [
  '# Name = test-client',
  '[Interface]',
  'PrivateKey = aP8A1234567890abcdefghijklmnopqrstuvwxyzABC=',
  'Address = 10.8.0.5/32',
  'Jc = 4', 'Jmin = 40', 'Jmax = 70',
  'S1 = 0', 'S2 = 0', 'S3 = 0', 'S4 = 0',
  'H1 = 1', 'H2 = 2', 'H3 = 3', 'H4 = 4',
  '[Peer]',
  'PublicKey = bQ9B1234567890abcdefghijklmnopqrstuvwxyzABC=',
  'Endpoint = 192.0.2.1:51820',
  'AllowedIPs = 0.0.0.0/0',
  '',
].join('\n');

test('decode of the Android reference vector equals the reference plaintext', () => {
  assert.strictEqual(decode(vectorAwg), vectorConf);
});

test('round-trips an AmneziaWG conf', () => {
  assert.strictEqual(decode(encode(sampleConf)), sampleConf);
});

test('encode output has the awg://v1/ prefix and an unpadded url-safe body', () => {
  const s = encode(sampleConf);
  assert.ok(s.startsWith('awg://v1/'));
  const body = s.slice('awg://v1/'.length);
  assert.ok(!body.includes('='), 'no padding');
  assert.ok(!body.includes('+') && !body.includes('/'), 'url-safe alphabet only');
});

test('decode tolerates the standard +/ alphabet and = padding', () => {
  const body = encode(sampleConf).slice('awg://v1/'.length);
  let std = body.replace(/-/g, '+').replace(/_/g, '/');
  while (std.length % 4) std += '=';
  assert.strictEqual(decode(`awg://v1/${std}`), sampleConf);
});

test('decode rejects a missing or wrong version prefix', () => {
  assert.throws(() => decode('https://example.com/foo'), /awg:\/\/v1/);
  assert.throws(() => decode('awg://v2/abcd'), /awg:\/\/v1/);
});

test('decode rejects a corrupt zlib payload', () => {
  const notZlib = Buffer.from('this is not a zlib stream').toString('base64url');
  assert.throws(() => decode(`awg://v1/${notZlib}`));
});
```

- [ ] **Step 3: Add the test script and run to verify it fails**

In `src/package.json`, add to the `scripts` object:

```json
    "test": "node --test",
```

Run: `cd src && npm test`
Expected: FAIL — `Cannot find module '../awgShareString'` (the module does not exist yet).

- [ ] **Step 4: Write the codec**

Create `src/lib/awgShareString.js`:

```js
'use strict';

const zlib = require('zlib');

const PREFIX = 'awg://v1/';

// Encode a WireGuard/AmneziaWG .conf string into an awg://v1/ share-string:
//   awg://v1/<base64url( zlib( utf8(conf) ) )>
// zlib = RFC 1950 (matches the Android ConfigShare reference); base64url = RFC 4648 §5, no padding.
function encode(confText) {
  const compressed = zlib.deflateSync(Buffer.from(confText, 'utf8'), {
    level: zlib.constants.Z_BEST_COMPRESSION,
  });
  return PREFIX + compressed.toString('base64url');
}

// Decode an awg://v1/ share-string back to the .conf text.
// Tolerant of the standard +/ base64 alphabet and = padding. Throws on a wrong
// prefix or a corrupt payload. Used for tests only — never exposed via the API.
function decode(shareString) {
  if (typeof shareString !== 'string' || !shareString.startsWith(PREFIX)) {
    throw new Error('not an awg://v1 string');
  }
  const body = shareString.slice(PREFIX.length).replace(/-/g, '+').replace(/_/g, '/');
  const compressed = Buffer.from(body, 'base64');
  return zlib.inflateSync(compressed).toString('utf8');
}

module.exports = { encode, decode };
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd src && npm test`
Expected: PASS — all tests pass (6 tests). The reference-vector test passing proves byte-compatibility with the Android decoder.

- [ ] **Step 6: Lint**

Run: `cd src && npm run lint`
Expected: clean (no output / exit 0). If `import/no-unresolved` flags `node:test`/`node:assert`, that is a resolver gap, not a real error — resolve by using the bare specifiers already shown (`require('node:test')` is correct and required; the module has no bare alias) or add them to the eslint `import/core-modules` setting. Do not remove the tests.

- [ ] **Step 7: Commit**

```bash
git add src/lib/awgShareString.js src/lib/__tests__ src/package.json
git commit -m "feat(awg-share): awg://v1 codec + node:test suite (Android-vector verified)"
```

---

### Task 2: Backend — `getClientShareString` + route

**Files:**
- Modify: `src/lib/WireGuard.js` (add method; require codec)
- Modify: `src/lib/Server.js` (add route after the `configuration` route, ~line 153–165)

**Interfaces:**
- Consumes: `encode` from `src/lib/awgShareString.js`; existing `WireGuard.getClient({clientId})` and `WireGuard.getClientConfiguration({clientId})`.
- Produces: `WireGuard.getClientShareString({ clientId }): Promise<string>` (returns the `awg://v1/…` string); HTTP `GET /api/wireguard/client/:clientId/share-string` (text/plain).

- [ ] **Step 1: Require the codec in WireGuard.js**

At the top of `src/lib/WireGuard.js`, alongside the existing `require(...)` lines, add:

```js
const ShareString = require('./awgShareString');
```

- [ ] **Step 2: Add `getClientShareString`**

In `src/lib/WireGuard.js`, immediately after the `getClientConfiguration({ clientId })` method (it ends near line 280, before `getClientQRCodeSVG`), add:

```js
  async getClientShareString({ clientId }) {
    const client = await this.getClient({ clientId });
    const config = await this.getClientConfiguration({ clientId });
    const name = String(client.name || '').replace(/[\r\n]+/g, ' ').trim();
    return ShareString.encode(`# Name = ${name}\n${config}`);
  }
```

(`getClientConfiguration` returns a string beginning with a newline, so the `# Name` comment sits on its own line above `[Interface]`.)

- [ ] **Step 3: Add the route**

In `src/lib/Server.js`, immediately after the `.get('/api/wireguard/client/:clientId/configuration', …)` handler block (ends ~line 165), add:

```js
      .get('/api/wireguard/client/:clientId/share-string', defineEventHandler(async (event) => {
        const clientId = getRouterParam(event, 'clientId');
        const shareString = await WireGuard.getClientShareString({ clientId });
        setHeader(event, 'Content-Type', 'text/plain');
        return shareString;
      }))
```

- [ ] **Step 4: Lint**

Run: `cd src && npm run lint`
Expected: clean.

- [ ] **Step 5: Manual verification (no automated test at this layer — app convention)**

```bash
cd src && npm run serve            # starts on http://localhost:51821 (no password in `serve`)
```
In the web UI, create a client named e.g. `test client` (with a space). Find its id (the `New`/list flow). Then in another shell:

```bash
# Replace <ID> with the client id (visible via: curl -s http://localhost:51821/api/wireguard/client | head -c 400)
curl -s "http://localhost:51821/api/wireguard/client/<ID>/share-string" > /tmp/share.txt
head -c 40 /tmp/share.txt; echo            # expect: awg://v1/...
node -e 'const{decode}=require("./lib/awgShareString");const fs=require("fs");const c=decode(fs.readFileSync("/tmp/share.txt","utf8").trim());console.log(c.split("\n").slice(0,3).join("\n"));'
```
Expected: the string starts with `awg://v1/`; the decoded output's first line is `# Name = test client` (space preserved, no CR/LF), followed by `[Interface]`. This confirms the route, the encode, and the name sanitization end-to-end.

- [ ] **Step 6: Commit**

```bash
git add src/lib/WireGuard.js src/lib/Server.js
git commit -m "feat(awg-share): getClientShareString() + GET /client/:id/share-string"
```

---

### Task 3: Frontend — copy-link button

**Files:**
- Modify: `src/www/js/api.js` (add `getClientShareString`)
- Modify: `src/www/js/app.js` (add `copiedClientId` + `copyShareLink`/`copyToClipboard`)
- Modify: `src/www/index.html` (add the button in the client-row action group)
- Modify: `src/www/js/i18n.js` (add `copyShareLink`, `copied`)

**Interfaces:**
- Consumes: the `GET …/share-string` endpoint from Task 2; existing `client.downloadableConfig`, `client.id`, `client.name`; the `.awg-icon-btn` class.
- Produces: a user-facing "Copy share link" control.

- [ ] **Step 1: Add the API client method**

In `src/www/js/api.js`, add this method to the `API` class (e.g. after `updateClientAddress`). It uses a direct `fetch` because the existing `call()` always parses JSON, whereas this endpoint returns `text/plain`:

```js
  async getClientShareString({ clientId }) {
    const res = await fetch(`./api/wireguard/client/${clientId}/share-string`);
    if (!res.ok) {
      let message = res.statusText;
      try { message = (await res.json()).error || message; } catch (e) { /* body is not JSON */ }
      throw new Error(message);
    }
    return res.text();
  }
```

- [ ] **Step 2: Add the data field**

In `src/www/js/app.js`, in the `data` object near `qrcode: null,` (~line 68), add:

```js
    copiedClientId: null,
```

- [ ] **Step 3: Add the methods**

In `src/www/js/app.js`, in the `methods` object (e.g. near `deleteClient`), add:

```js
    copyShareLink(client) {
      this.api.getClientShareString({ clientId: client.id })
        .then((link) => this.copyToClipboard(link))
        .then(() => {
          this.copiedClientId = client.id;
          setTimeout(() => {
            if (this.copiedClientId === client.id) this.copiedClientId = null;
          }, 1500);
        })
        .catch((err) => alert(err.message || err.toString()));
    },
    async copyToClipboard(text) {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        return;
      }
      // Fallback for non-secure contexts (wg-easy is often served over plain HTTP).
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.top = '-1000px';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.focus();
      ta.select();
      let ok = false;
      try {
        ok = document.execCommand('copy');
      } finally {
        document.body.removeChild(ta);
      }
      if (!ok) {
        // Last resort: show the string so the user can copy it manually.
        window.prompt('Copy this share link:', text);
      }
    },
```

- [ ] **Step 4: Add the button to the client-row action group**

In `src/www/index.html`, in the row action group, insert this button between the `<!-- QR Code -->` button and the `<!-- Delete -->` button (i.e. right before the `<!-- Delete -->` comment, ~line 684):

```html
                    <!-- Copy share link -->
                    <button v-if="client.downloadableConfig"
                      class="awg-icon-btn align-middle text-gray-400 dark:text-neutral-400 transition"
                      :title="copiedClientId === client.id ? $t('copied') : $t('copyShareLink')"
                      @click="copyShareLink(client)">
                      <svg v-if="copiedClientId === client.id" class="w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
                      </svg>
                      <svg v-else class="w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.828 10.172a4 4 0 010 5.656l-3 3a4 4 0 01-5.656-5.656l1.5-1.5m6.328-1.328a4 4 0 010-5.656l3-3a4 4 0 015.656 5.656l-1.5 1.5" />
                      </svg>
                    </button>
```

- [ ] **Step 5: Add i18n keys**

In `src/www/js/i18n.js`, alongside the existing `showQR`/`downloadConfig` keys **in every locale object**, add (English text is an acceptable fallback for untranslated locales — do not block on translations):

```js
    copyShareLink: 'Copy share link',
    copied: 'Copied!',
```

- [ ] **Step 6: Build CSS and lint**

The button reuses the existing `.awg-icon-btn` class, so no new Tailwind classes are introduced; rebuild anyway to keep `app.css` in sync, then lint.

```bash
cd src && npm run buildcss && npm run lint
```
Expected: buildcss succeeds; lint clean.

- [ ] **Step 7: Manual verification**

```bash
cd src && npm run serve     # http://localhost:51821
```
- A "Copy share link" (chain) icon appears in each client's row, beside the QR and download controls, only when the client has a downloadable config.
- Click it: the icon briefly switches to a checkmark (~1.5s). Paste from the clipboard → the pasted text starts with `awg://v1/`.
- (Optional non-secure-context check) Access the UI over a plain-HTTP LAN/VPN IP rather than `localhost`; `navigator.clipboard` is unavailable there, so the fallback path runs — the copy still succeeds (or, in the worst case, a prompt shows the string). The button never silently no-ops.

- [ ] **Step 8: Commit**

```bash
git add src/www/js/api.js src/www/js/app.js src/www/index.html src/www/js/i18n.js src/www/css/app.css
git commit -m "feat(awg-share): copy-link button with non-secure-context clipboard fallback"
```

---

## Self-Review

**Spec coverage:** Codec `encode`/`decode` (T1) · zlib+base64url wire format exact (T1) · Android-vector cross-compat via decode (T1) · `node:test` + `npm test` (T1) · `getClientShareString` + `# Name` + CR/LF sanitization (T2) · route mirroring `configuration` (T2) · `downloadableConfig` gate + copy button (T3) · non-secure-context clipboard fallback (T3) · i18n label (T3). Deferred items (import/decode UI, second QR, encryption, subsystem #4) intentionally absent. ✓

**Placeholder scan:** No TBD/TODO; every code step has complete content. The two manual-verification steps (T2 S5, T3 S7) are explicit commands/observations, not placeholders — they exist because the app has no test harness at the H3/Vue layer (per CLAUDE.md) and the spec scopes automated tests to the codec. ✓

**Type/name consistency:** `encode`/`decode` (T1) consumed as `ShareString.encode` (T2); `getClientShareString({ clientId })` defined in T2, called by `api.getClientShareString` (T3, own name) → endpoint path identical in T2/T3; `copiedClientId` defined (T3 S2) and used in the button (T3 S4); i18n keys `copyShareLink`/`copied` defined (T3 S5) and used (T3 S4). ✓

**Risk note:** The one cross-process correctness risk (Node deflate vs Java deflate) is handled by verifying compatibility through `decode` of the real Android vector rather than byte-exact encode — already proven empirically during planning (`decode(share-vector.awg) === share-vector.conf`, 266/266 bytes).
