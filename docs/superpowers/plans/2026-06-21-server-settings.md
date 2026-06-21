# Server Settings (backend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin edit the AmneziaWG server's own config (network, client defaults, obfuscation, keypair) from the web UI, persisted in `wg0.json` and applied at runtime without restarting the container.

**Architecture:** Editable values move into `config.server` (seeded from env on first boot, then authoritative). Config generation reads `config.server.*` instead of the `config.js` env constants. A save validates the patch, classifies the change (save-only vs interface-bounce), writes, and bounces the interface only when `port`/obfuscation/keypair changed — with `down`-before-`write` ordering so the live iptables rules are torn down correctly. Pure logic (validation, classification, seeding, config-string rendering) lives in two new dependency-free modules so it is unit-testable; `WireGuard.js`/`Server.js` only orchestrate.

**Tech Stack:** Node.js 18+, H3 HTTP framework, `node:test` + `node:assert`, `node:net` (`isIP`). CommonJS (`'use strict'`, `require`).

## Global Constraints

- Source lives under `src/`; run everything from `src/` (`cd src && node --test`).
- Tests use `node:test` + `node:assert`, CommonJS, files in `src/lib/__tests__/*.test.js` (cf. `stripImitationKeys.test.js`).
- H3 idioms only: `defineEventHandler`, `readBody`, `getRouterParam`, `createError` — never Express `req/res`.
- Prototype-pollution guard on any route param: reject `__proto__`/`constructor`/`prototype`.
- Never return `privateKey` from any route.
- The server's own interface `address` is **read-only** (out of scope to edit).
- `IMITATE_PROTOCOL`, `WG_DEVICE`, `WG_PRE_UP`/`WG_PRE_DOWN` stay env-driven (out of scope).
- ESLint extends `athom`; lint with `cd src && npm run lint` before each commit.
- Restart fields (force `wg-quick down/up`): `port, jc, jmin, jmax, s1, s2, s3, s4, h1, h2, h3, h4`.
- Reimport fields (existing tunnels break until clients re-download): `host, port, jc, jmin, jmax, s1, s2, s3, s4, h1, h2, h3, h4` (+ keypair regeneration).

---

### Task 1: Validation core (`serverSettings.js`)

Pure validators + `validateServerSettings(patch, current)`. Uses `node:net.isIP` for IPv4/IPv6 (`Util` only has `isValidIPv4`; the default `allowedIPs: 0.0.0.0/0, ::/0` is IPv6+CIDR and must pass).

**Files:**
- Create: `src/lib/serverSettings.js`
- Test: `src/lib/__tests__/serverSettings.test.js`

**Interfaces:**
- Consumes: nothing (pure; `node:net`).
- Produces:
  - `isValidIP(str): boolean` — IPv4 or IPv6.
  - `isValidIPList(str): boolean` — non-empty comma list, every entry `isValidIP`.
  - `isValidCIDR(str): boolean` — `ip/prefix`, `ip` valid, prefix integer in `0..32` (v4) / `0..128` (v6).
  - `isValidCIDRList(str): boolean` — non-empty comma list, every entry `isValidCIDR`.
  - `isValidHostname(str): boolean` — non-empty, `^[A-Za-z0-9.:_-]+$`.
  - `H_SPACE_MIN = 5`, `H_SPACE_MAX = 2147483647`.
  - `validateServerSettings(patch, current): { [field]: string }` — empty object = valid. `current` supplies `address` for the same-/24 `defaultAddress` rule and the other operand of `jmin<=jmax`/`h.min<=h.max` when only one side is in `patch`.

- [ ] **Step 1: Write the failing test**

Create `src/lib/__tests__/serverSettings.test.js`:

```js
'use strict';

const { test } = require('node:test');
const assert = require('node:assert');

const S = require('../serverSettings');

const CURRENT = { address: '10.8.0.1' };

test('isValidIP accepts IPv4 and IPv6, rejects junk', () => {
  assert.ok(S.isValidIP('1.1.1.1'));
  assert.ok(S.isValidIP('2606:4700:4700::1111'));
  assert.ok(!S.isValidIP('1.1.1'));
  assert.ok(!S.isValidIP('nope'));
});

test('isValidCIDR accepts default-route v4/v6 and bounds prefix', () => {
  assert.ok(S.isValidCIDR('0.0.0.0/0'));
  assert.ok(S.isValidCIDR('::/0'));
  assert.ok(S.isValidCIDR('10.8.0.0/24'));
  assert.ok(!S.isValidCIDR('10.8.0.0/33'));
  assert.ok(!S.isValidCIDR('10.8.0.0'));   // prefix required
  assert.ok(!S.isValidCIDR('::/129'));
});

test('isValidCIDRList accepts the shipped default', () => {
  assert.ok(S.isValidCIDRList('0.0.0.0/0, ::/0'));
  assert.ok(!S.isValidCIDRList(''));
  assert.ok(!S.isValidCIDRList('0.0.0.0/0, nonsense'));
});

test('validateServerSettings returns {} for a valid patch', () => {
  const patch = {
    host: 'vpn.example.com', port: 51820, mtu: 1420,
    dns: '1.1.1.1, 2606:4700:4700::1111',
    defaultAddress: '10.8.0.x', allowedIPs: '0.0.0.0/0, ::/0',
    persistentKeepalive: 25,
    jc: 4, jmin: 40, jmax: 70, s1: 0, s2: 0, s3: 20, s4: 8,
    h1: { min: 100, max: 500 }, i1: null,
  };
  assert.deepStrictEqual(S.validateServerSettings(patch, CURRENT), {});
});

test('validateServerSettings flags out-of-range and bad formats', () => {
  const errs = S.validateServerSettings(
    { port: 70000, mtu: 100, allowedIPs: 'bad', jmin: 90, jmax: 10 },
    CURRENT,
  );
  assert.ok(errs.port, 'port out of range');
  assert.ok(errs.mtu, 'mtu below floor');
  assert.ok(errs.allowedIPs, 'bad CIDR list');
  assert.ok(errs.jmax || errs.jmin, 'jmin > jmax');
});

test('defaultAddress must stay in the server /24, template ending in x', () => {
  assert.deepStrictEqual(S.validateServerSettings({ defaultAddress: '10.8.0.x' }, CURRENT), {});
  assert.ok(S.validateServerSettings({ defaultAddress: '10.9.0.x' }, CURRENT).defaultAddress, 'subnet base change rejected');
  assert.ok(S.validateServerSettings({ defaultAddress: '10.8.0.5' }, CURRENT).defaultAddress, 'must end in x');
});

test('mtu accepts null/empty', () => {
  assert.deepStrictEqual(S.validateServerSettings({ mtu: null }, CURRENT), {});
  assert.deepStrictEqual(S.validateServerSettings({ mtu: '' }, CURRENT), {});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && node --test lib/__tests__/serverSettings.test.js`
Expected: FAIL — `Cannot find module '../serverSettings'`.

- [ ] **Step 3: Write minimal implementation**

Create `src/lib/serverSettings.js`:

```js
'use strict';

const net = require('node:net');

const H_SPACE_MIN = 5;
const H_SPACE_MAX = 2147483647;

const isValidIP = (str) => typeof str === 'string' && net.isIP(str.trim()) !== 0;

const isValidIPList = (str) => {
  if (typeof str !== 'string' || str.trim() === '') return false;
  return str.split(',').every((part) => isValidIP(part.trim()));
};

const isValidCIDR = (str) => {
  if (typeof str !== 'string') return false;
  const parts = str.trim().split('/');
  if (parts.length !== 2) return false;
  const [ip, prefix] = parts;
  const fam = net.isIP(ip);
  if (fam === 0) return false;
  const p = Number(prefix);
  if (!Number.isInteger(p) || p < 0) return false;
  return fam === 4 ? p <= 32 : p <= 128;
};

const isValidCIDRList = (str) => {
  if (typeof str !== 'string' || str.trim() === '') return false;
  return str.split(',').every((part) => isValidCIDR(part.trim()));
};

const isValidHostname = (str) => typeof str === 'string' && /^[A-Za-z0-9.:_-]+$/.test(str.trim()) && str.trim() !== '';

const isInt = (v) => Number.isInteger(Number(v)) && String(v).trim() !== '';

// Numeric range guard helper.
const intInRange = (v, lo, hi) => isInt(v) && Number(v) >= lo && Number(v) <= hi;

function validateServerSettings(patch, current = {}) {
  const errors = {};
  const has = (k) => Object.prototype.hasOwnProperty.call(patch, k);
  // effective value: patch wins, else current
  const val = (k) => (has(k) ? patch[k] : current[k]);

  if (has('host') && !isValidHostname(patch.host)) errors.host = 'Enter a hostname or IP';
  if (has('port') && !intInRange(patch.port, 1, 65535)) errors.port = 'Port must be 1–65535';
  if (has('mtu') && patch.mtu !== null && patch.mtu !== '' && !intInRange(patch.mtu, 576, 1500)) {
    errors.mtu = 'MTU must be 576–1500 (or empty)';
  }
  if (has('dns') && !isValidIPList(patch.dns)) errors.dns = 'Comma-separated IPs only';
  if (has('allowedIPs') && !isValidCIDRList(patch.allowedIPs)) errors.allowedIPs = 'Comma-separated CIDRs only';
  if (has('persistentKeepalive') && !intInRange(patch.persistentKeepalive, 0, 65535)) {
    errors.persistentKeepalive = 'Keepalive must be 0–65535';
  }

  if (has('defaultAddress')) {
    const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.x$/.exec(String(patch.defaultAddress));
    const octetsOk = m && [m[1], m[2], m[3]].every((o) => Number(o) >= 0 && Number(o) <= 255);
    const curBase = String(current.address || '').split('.').slice(0, 3).join('.');
    const newBase = m ? `${m[1]}.${m[2]}.${m[3]}` : null;
    if (!octetsOk) errors.defaultAddress = 'Use a template like 10.8.0.x';
    else if (curBase && newBase !== curBase) errors.defaultAddress = `Must stay in ${curBase}.x (server subnet is fixed)`;
  }

  // Obfuscation
  if (has('jc') && !intInRange(patch.jc, 1, 128)) errors.jc = 'Jc must be 1–128';
  for (const k of ['jmin', 'jmax', 's1', 's2', 's3', 's4']) {
    if (has(k) && !intInRange(patch[k], 0, 1280)) errors[k] = `${k} must be 0–1280`;
  }
  if (!errors.jmin && !errors.jmax && Number(val('jmin')) > Number(val('jmax'))) {
    errors.jmax = 'Jmax must be ≥ Jmin';
  }
  for (const k of ['h1', 'h2', 'h3', 'h4']) {
    if (!has(k)) continue;
    const h = patch[k];
    if (!h || typeof h !== 'object'
      || !intInRange(h.min, H_SPACE_MIN, H_SPACE_MAX)
      || !intInRange(h.max, H_SPACE_MIN, H_SPACE_MAX)
      || Number(h.min) > Number(h.max)) {
      errors[k] = `${k} must be {min,max} within ${H_SPACE_MIN}–${H_SPACE_MAX}, min ≤ max`;
    }
  }
  for (const k of ['i1', 'i2', 'i3', 'i4', 'i5']) {
    if (has(k) && patch[k] !== null && typeof patch[k] !== 'string') errors[k] = `${k} must be text or empty`;
  }

  return errors;
}

module.exports = {
  H_SPACE_MIN,
  H_SPACE_MAX,
  isValidIP,
  isValidIPList,
  isValidCIDR,
  isValidCIDRList,
  isValidHostname,
  validateServerSettings,
};
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && node --test lib/__tests__/serverSettings.test.js`
Expected: PASS (all tests).
Then: `cd src && npm run lint`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add src/lib/serverSettings.js src/lib/__tests__/serverSettings.test.js
git commit -m "feat(server-settings): validation core (net.isIP CIDR/IPv6, same-/24 guard)"
```

---

### Task 2: Change classifier (`serverSettings.js`)

Pure `classify(prev, next)` → which fields changed, whether the interface must bounce, whether clients must reimport.

**Files:**
- Modify: `src/lib/serverSettings.js`
- Test: `src/lib/__tests__/serverSettings.test.js`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `RESTART_FIELDS: string[]` = `['port','jc','jmin','jmax','s1','s2','s3','s4','h1','h2','h3','h4']`.
  - `REIMPORT_FIELDS: string[]` = `['host', ...RESTART_FIELDS]`.
  - `classify(prev, next): { changed: string[], needsRestart: boolean, mustReimport: boolean }` — compares only keys present in `next`; `h*` compared by `{min,max}`, scalars by `String()` (so `'51820'` equals `51820`).

- [ ] **Step 1: Write the failing test**

Append to `src/lib/__tests__/serverSettings.test.js`:

```js
const PREV = {
  host: 'a.example', port: '51820', mtu: 1420, dns: '1.1.1.1',
  defaultAddress: '10.8.0.x', allowedIPs: '0.0.0.0/0', persistentKeepalive: '0',
  jc: 4, jmin: 40, jmax: 70, s1: 0, s2: 0, s3: 20, s4: 8,
  h1: { min: 1, max: 2 }, h2: { min: 3, max: 4 }, h3: { min: 5, max: 6 }, h4: { min: 7, max: 8 },
  i1: null,
};

test('classify: client-only DNS change is save-only', () => {
  const r = S.classify(PREV, { dns: '8.8.8.8' });
  assert.deepStrictEqual(r.changed, ['dns']);
  assert.strictEqual(r.needsRestart, false);
  assert.strictEqual(r.mustReimport, false);
});

test('classify: host change needs reimport but not restart', () => {
  const r = S.classify(PREV, { host: 'b.example' });
  assert.strictEqual(r.needsRestart, false);
  assert.strictEqual(r.mustReimport, true);
});

test('classify: port change needs restart and reimport', () => {
  const r = S.classify(PREV, { port: 51821 });
  assert.strictEqual(r.needsRestart, true);
  assert.strictEqual(r.mustReimport, true);
});

test('classify: obfuscation H-range change needs restart and reimport', () => {
  const r = S.classify(PREV, { h1: { min: 9, max: 10 } });
  assert.deepStrictEqual(r.changed, ['h1']);
  assert.strictEqual(r.needsRestart, true);
  assert.strictEqual(r.mustReimport, true);
});

test('classify: same-value patch (string vs number port) is no change', () => {
  const r = S.classify(PREV, { port: '51820', h1: { min: 1, max: 2 } });
  assert.deepStrictEqual(r.changed, []);
  assert.strictEqual(r.needsRestart, false);
});

test('classify: same-/24 defaultAddress template change is save-only', () => {
  const r = S.classify(PREV, { defaultAddress: '10.8.0.x' });
  assert.deepStrictEqual(r.changed, []); // identical value
  const r2 = S.classify({ ...PREV, defaultAddress: '10.8.1.x' }, { defaultAddress: '10.8.0.x' });
  assert.deepStrictEqual(r2.changed, ['defaultAddress']);
  assert.strictEqual(r2.needsRestart, false);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && node --test lib/__tests__/serverSettings.test.js`
Expected: FAIL — `S.classify is not a function`.

- [ ] **Step 3: Write minimal implementation**

In `src/lib/serverSettings.js`, add before `module.exports` and extend the exports:

```js
const RESTART_FIELDS = ['port', 'jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4', 'h1', 'h2', 'h3', 'h4'];
const REIMPORT_FIELDS = ['host', ...RESTART_FIELDS];

const eq = (a, b) => {
  if (a && b && typeof a === 'object' && typeof b === 'object') {
    return a.min === b.min && a.max === b.max;
  }
  return String(a) === String(b);
};

function classify(prev, next) {
  const changed = Object.keys(next).filter((k) => !eq(prev[k], next[k]));
  return {
    changed,
    needsRestart: changed.some((k) => RESTART_FIELDS.includes(k)),
    mustReimport: changed.some((k) => REIMPORT_FIELDS.includes(k)),
  };
}
```

Add to `module.exports`: `RESTART_FIELDS, REIMPORT_FIELDS, classify`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && node --test lib/__tests__/serverSettings.test.js`
Expected: PASS.
Then: `cd src && npm run lint` — no errors.

- [ ] **Step 5: Commit**

```bash
git add src/lib/serverSettings.js src/lib/__tests__/serverSettings.test.js
git commit -m "feat(server-settings): classify() restart/reimport change detection"
```

---

### Task 3: Seed defaults + wire migration into `getConfig()`

Pure `seedServerDefaults()` fills missing `config.server` fields from env seeds; wire it into `WireGuard.getConfig()` next to the existing `s3/s4` backfill.

**Files:**
- Modify: `src/lib/serverSettings.js`
- Modify: `src/lib/WireGuard.js` (imports block `1-44`; `getConfig()` `48-123`)
- Test: `src/lib/__tests__/serverSettings.test.js`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `SERVER_SEED_KEYS: string[]` = `['host','port','mtu','dns','defaultAddress','allowedIPs','persistentKeepalive','i1','i2','i3','i4','i5']`.
  - `seedServerDefaults(server, seeds): server` — for each key in `seeds`, set `server[key] = seeds[key]` only when `server[key] === undefined`; returns the same `server`.

- [ ] **Step 1: Write the failing test**

Append to `src/lib/__tests__/serverSettings.test.js`:

```js
test('seedServerDefaults fills only missing keys, preserves existing', () => {
  const server = { port: '51999', host: 'kept.example' };
  const seeds = {
    host: 'seed.example', port: '51820', mtu: null, dns: '1.1.1.1',
    defaultAddress: '10.8.0.x', allowedIPs: '0.0.0.0/0, ::/0',
    persistentKeepalive: '0', i1: null, i2: null, i3: null, i4: null, i5: null,
  };
  S.seedServerDefaults(server, seeds);
  assert.strictEqual(server.host, 'kept.example');   // preserved
  assert.strictEqual(server.port, '51999');          // preserved
  assert.strictEqual(server.dns, '1.1.1.1');         // seeded
  assert.strictEqual(server.allowedIPs, '0.0.0.0/0, ::/0');
  assert.strictEqual(server.mtu, null);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && node --test lib/__tests__/serverSettings.test.js`
Expected: FAIL — `S.seedServerDefaults is not a function`.

- [ ] **Step 3a: Implement `seedServerDefaults`**

In `src/lib/serverSettings.js` add:

```js
const SERVER_SEED_KEYS = ['host', 'port', 'mtu', 'dns', 'defaultAddress', 'allowedIPs', 'persistentKeepalive', 'i1', 'i2', 'i3', 'i4', 'i5'];

function seedServerDefaults(server, seeds) {
  for (const key of Object.keys(seeds)) {
    if (server[key] === undefined) server[key] = seeds[key];
  }
  return server;
}
```

Add to `module.exports`: `SERVER_SEED_KEYS, seedServerDefaults`.

- [ ] **Step 3b: Wire into `WireGuard.getConfig()`**

In `src/lib/WireGuard.js`, after the existing `require('./awgShareString')` / `stripImitationKeys` lines (around line 12), add:

```js
const ServerSettings = require('./serverSettings');
```

The destructured `require('../config')` block (lines `14-44`) already imports `WG_HOST, WG_PORT, WG_MTU, WG_DEFAULT_DNS, WG_DEFAULT_ADDRESS, WG_PERSISTENT_KEEPALIVE, WG_ALLOWED_IPS, I1..I5` — keep them. In `getConfig()`, immediately **after** the `if (config.server.s4 === undefined) config.server.s4 = S4;` line (currently line 69), and also covering the freshly-created config, insert the seed call so it runs on **both** load and create paths. Replace lines `103` (`await this.__saveConfig(config);`) region's preceding context by adding before `await this.__saveConfig(config);`:

```js
        ServerSettings.seedServerDefaults(config.server, {
          host: WG_HOST,
          port: WG_PORT,
          mtu: WG_MTU,
          dns: WG_DEFAULT_DNS,
          defaultAddress: WG_DEFAULT_ADDRESS,
          allowedIPs: WG_ALLOWED_IPS,
          persistentKeepalive: WG_PERSISTENT_KEEPALIVE,
          i1: I1, i2: I2, i3: I3, i4: I4, i5: I5,
        });
```

(Place it right before `await this.__saveConfig(config);` at line 103 so it applies to both the loaded and the newly-generated `config`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && node --test lib/__tests__/serverSettings.test.js`
Expected: PASS.
Then: `cd src && npm run lint` — no errors. (WireGuard.js change is exercised manually later; no new shell behavior yet — seeded fields are written to `wg0.json` but generation still reads env until Task 5.)

- [ ] **Step 5: Commit**

```bash
git add src/lib/serverSettings.js src/lib/__tests__/serverSettings.test.js src/lib/WireGuard.js
git commit -m "feat(server-settings): seed config.server defaults from env on load/create"
```

---

### Task 4: Config-string renderers (`configRender.js`)

Pure functions that build `wg0.conf`, a client `.conf`, and the default iptables hooks from `config.server` (no env reads inside). This is the seam that makes the env→`config.server` switch testable.

**Files:**
- Create: `src/lib/configRender.js`
- Test: `src/lib/__tests__/configRender.test.js`

**Interfaces:**
- Consumes: nothing (pure).
- Produces:
  - `renderDefaultHooks(server, env): { preUp, postUp, preDown, postDown }` — `env = { device, preUp, postUp, preDown, postDown }`. Each of `preUp/postUp/preDown/postDown`: if the matching `env.*` override is a non-empty string, use it verbatim; otherwise generate the default. Defaults for `postUp`/`postDown` are the iptables rules built from `server.defaultAddress` (`.replace('x','0')`/`24`), `server.port`, and `env.device`. `preUp`/`preDown` default to `''`.
  - `renderServerConf(server, clients, hooks, imitateProtocol): string` — full `wg0.conf` (interface block from `server.privateKey/address/port`, `hooks`, `jc..h4`, optional `ImitateProtocol`, then one `[Peer]` per enabled client).
  - `renderClientConf(server, client, imitateProtocol): string` — client `.conf` from `server.dns/mtu/jc..h4/i1..i5/publicKey/allowedIPs/persistentKeepalive/host/port`. Does **not** apply legacy stripping (caller does).

- [ ] **Step 1: Write the failing test**

Create `src/lib/__tests__/configRender.test.js`:

```js
'use strict';

const { test } = require('node:test');
const assert = require('node:assert');

const R = require('../configRender');

const SERVER = {
  privateKey: 'srvPriv=', publicKey: 'srvPub=', address: '10.8.0.1',
  port: '51820', host: 'vpn.example.com', mtu: 1420, dns: '1.1.1.1',
  defaultAddress: '10.8.0.x', allowedIPs: '0.0.0.0/0, ::/0', persistentKeepalive: '25',
  jc: 4, jmin: 40, jmax: 70, s1: 0, s2: 0, s3: 20, s4: 8,
  h1: { min: 100, max: 500 }, h2: { min: 600, max: 900 },
  h3: { min: 1000, max: 1500 }, h4: { min: 1600, max: 2000 },
  i1: null, i2: null, i3: null, i4: null, i5: null,
};
const CLIENT = { name: 'phone', address: '10.8.0.2', publicKey: 'cliPub=', privateKey: 'cliPriv=', preSharedKey: '', enabled: true };

test('renderDefaultHooks builds iptables rules from server.port and subnet', () => {
  const { postUp, postDown, preUp } = R.renderDefaultHooks(SERVER, { device: 'eth0' });
  assert.ok(postUp.includes('--dport 51820'));
  assert.ok(postUp.includes('-s 10.8.0.0/24'));
  assert.ok(postUp.includes('-o eth0'));
  assert.ok(postDown.includes('--dport 51820'));
  assert.strictEqual(preUp, '');
});

test('renderDefaultHooks honors an explicit override verbatim', () => {
  const { postUp } = R.renderDefaultHooks(SERVER, { device: 'eth0', postUp: 'echo custom;' });
  assert.strictEqual(postUp, 'echo custom;');
});

test('renderServerConf has ListenPort from server and a peer per enabled client', () => {
  const hooks = R.renderDefaultHooks(SERVER, { device: 'eth0' });
  const conf = R.renderServerConf(SERVER, { phone: CLIENT }, hooks, 'none');
  assert.ok(conf.includes('ListenPort = 51820'));
  assert.ok(conf.includes('PrivateKey = srvPriv='));
  assert.ok(conf.includes('PublicKey = cliPub='));
  assert.ok(conf.includes('H1 = 100-500'));
  assert.ok(!conf.includes('ImitateProtocol'));
});

test('renderClientConf pulls DNS/MTU/Endpoint/AllowedIPs from server', () => {
  const conf = R.renderClientConf(SERVER, CLIENT, 'none');
  assert.ok(conf.includes('DNS = 1.1.1.1'));
  assert.ok(conf.includes('MTU = 1420'));
  assert.ok(conf.includes('Endpoint = vpn.example.com:51820'));
  assert.ok(conf.includes('AllowedIPs = 0.0.0.0/0, ::/0'));
  assert.ok(conf.includes('PersistentKeepalive = 25'));
  assert.ok(conf.includes('PublicKey = srvPub='));
});

test('renderClientConf emits ImitateProtocol and i-params when set', () => {
  const conf = R.renderClientConf({ ...SERVER, i1: '<qinit a.com>' }, CLIENT, 'quic');
  assert.ok(conf.includes('ImitateProtocol = quic'));
  assert.ok(conf.includes('I1 = <qinit a.com>'));
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && node --test lib/__tests__/configRender.test.js`
Expected: FAIL — `Cannot find module '../configRender'`.

- [ ] **Step 3: Write minimal implementation**

Create `src/lib/configRender.js` (templates copied from the current `WireGuard.__saveConfig` / `getClientConfiguration` so output is byte-identical except for the env→server swap):

```js
'use strict';

const defaultPostUp = (server, device) => `
iptables -t nat -A POSTROUTING -s ${server.defaultAddress.replace('x', '0')}/24 -o ${device} -j MASQUERADE;
iptables -A INPUT -p udp -m udp --dport ${server.port} -j ACCEPT;
iptables -A FORWARD -i wg0 -j ACCEPT;
iptables -A FORWARD -o wg0 -j ACCEPT;
`.split('\n').join(' ');

const defaultPostDown = (server, device) => `
iptables -t nat -D POSTROUTING -s ${server.defaultAddress.replace('x', '0')}/24 -o ${device} -j MASQUERADE;
iptables -D INPUT -p udp -m udp --dport ${server.port} -j ACCEPT;
iptables -D FORWARD -i wg0 -j ACCEPT;
iptables -D FORWARD -o wg0 -j ACCEPT;
`.split('\n').join(' ');

const pick = (override, fallback) => (typeof override === 'string' && override !== '' ? override : fallback);

function renderDefaultHooks(server, env = {}) {
  const device = env.device || 'eth0';
  return {
    preUp: pick(env.preUp, ''),
    postUp: pick(env.postUp, defaultPostUp(server, device)),
    preDown: pick(env.preDown, ''),
    postDown: pick(env.postDown, defaultPostDown(server, device)),
  };
}

function renderServerConf(server, clients, hooks, imitateProtocol) {
  let result = `
# Note: Do not edit this file directly.
# Your changes will be overwritten!

# Server
[Interface]
PrivateKey = ${server.privateKey}
Address = ${server.address}/24
ListenPort = ${server.port}
PreUp = ${hooks.preUp}
PostUp = ${hooks.postUp}
PreDown = ${hooks.preDown}
PostDown = ${hooks.postDown}
Jc = ${server.jc}
Jmin = ${server.jmin}
Jmax = ${server.jmax}
S1 = ${server.s1}
S2 = ${server.s2}
S3 = ${server.s3}
S4 = ${server.s4}
H1 = ${server.h1.min}-${server.h1.max}
H2 = ${server.h2.min}-${server.h2.max}
H3 = ${server.h3.min}-${server.h3.max}
H4 = ${server.h4.min}-${server.h4.max}
${imitateProtocol !== 'none' ? `ImitateProtocol = ${imitateProtocol}\n` : ''}`;

  for (const [clientId, client] of Object.entries(clients)) {
    if (!client.enabled) continue;
    result += `

# Client: ${client.name} (${clientId})
[Peer]
PublicKey = ${client.publicKey}
${client.preSharedKey ? `PresharedKey = ${client.preSharedKey}\n` : ''
}AllowedIPs = ${client.address}/32`;
  }
  return result;
}

function renderClientConf(server, client, imitateProtocol) {
  return `
[Interface]
PrivateKey = ${client.privateKey ? `${client.privateKey}` : 'REPLACE_ME'}
Address = ${client.address}
${server.dns ? `DNS = ${server.dns}\n` : ''}\
${server.mtu ? `MTU = ${server.mtu}\n` : ''}\
Jc = ${server.jc}
Jmin = ${server.jmin}
Jmax = ${server.jmax}
S1 = ${server.s1}
S2 = ${server.s2}
S3 = ${server.s3}
S4 = ${server.s4}
H1 = ${server.h1.min}-${server.h1.max}
H2 = ${server.h2.min}-${server.h2.max}
H3 = ${server.h3.min}-${server.h3.max}
H4 = ${server.h4.min}-${server.h4.max}
${imitateProtocol !== 'none' ? `ImitateProtocol = ${imitateProtocol}\n` : ''}\
${server.i1 ? `I1 = ${server.i1}\n` : ''}\
${server.i2 ? `I2 = ${server.i2}\n` : ''}\
${server.i3 ? `I3 = ${server.i3}\n` : ''}\
${server.i4 ? `I4 = ${server.i4}\n` : ''}\
${server.i5 ? `I5 = ${server.i5}\n` : ''}\

[Peer]
PublicKey = ${server.publicKey}
${client.preSharedKey ? `PresharedKey = ${client.preSharedKey}\n` : ''
}AllowedIPs = ${server.allowedIPs}
PersistentKeepalive = ${server.persistentKeepalive}
Endpoint = ${server.host}:${server.port}`;
}

module.exports = { renderDefaultHooks, renderServerConf, renderClientConf };
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && node --test lib/__tests__/configRender.test.js`
Expected: PASS.
Then: `cd src && npm run lint` — no errors.

- [ ] **Step 5: Commit**

```bash
git add src/lib/configRender.js src/lib/__tests__/configRender.test.js
git commit -m "feat(server-settings): pure config renderers reading config.server"
```

---

### Task 5: Wire renderers into `WireGuard` (env → `config.server`)

Replace the inline env-based strings in `__saveConfig` and `getClientConfiguration` with the Task-4 renderers; change `config.js` so the iptables hook defaults are no longer baked (render-time instead), preserving env overrides.

**Files:**
- Modify: `src/config.js` (lines `22-36`)
- Modify: `src/lib/WireGuard.js` (imports `14-44`; `__saveConfig` `131-178`; `getClientConfiguration` `251-286`)

**Interfaces:**
- Consumes: `configRender.renderDefaultHooks/renderServerConf/renderClientConf` (Task 4); `config.server.*` fields (Task 3).
- Produces: no new exported symbols; `wg0.conf` and client `.conf` now derive from `config.server`.

- [ ] **Step 1: Change `config.js` hook defaults to "unset = generate"**

In `src/config.js`, replace the `WG_POST_UP` (lines `23-28`) and `WG_POST_DOWN` (lines `31-36`) blocks so they no longer bake the iptables default — keep only an explicit env override:

```js
module.exports.WG_PRE_UP = process.env.WG_PRE_UP || '';
module.exports.WG_POST_UP = process.env.WG_POST_UP || '';
module.exports.WG_PRE_DOWN = process.env.WG_PRE_DOWN || '';
module.exports.WG_POST_DOWN = process.env.WG_POST_DOWN || '';
```

(The default MASQUERADE/ACCEPT rules now come from `configRender.renderDefaultHooks`, built from the editable `config.server.port`/`defaultAddress`.)

- [ ] **Step 2: Rewire `WireGuard.__saveConfig`**

In `src/lib/WireGuard.js`, add `WG_DEVICE` to the destructured config import (it is currently not imported), and ensure `WG_PRE_UP, WG_POST_UP, WG_PRE_DOWN, WG_POST_DOWN, IMITATE_PROTOCOL` remain imported. Add near the other requires:

```js
const ConfigRender = require('./configRender');
```

Replace the body of `__saveConfig(config)` (the `let result = ...` template through the peer loop, lines `131-168`) with:

```js
  async __saveConfig(config) {
    const hooks = ConfigRender.renderDefaultHooks(config.server, {
      device: WG_DEVICE,
      preUp: WG_PRE_UP,
      postUp: WG_POST_UP,
      preDown: WG_PRE_DOWN,
      postDown: WG_POST_DOWN,
    });
    const result = ConfigRender.renderServerConf(config.server, config.clients, hooks, IMITATE_PROTOCOL);

    debug('Config saving...');
    await fs.writeFile(path.join(WG_PATH, 'wg0.json'), JSON.stringify(config, false, 2), {
      mode: 0o660,
    });
    await fs.writeFile(path.join(WG_PATH, 'wg0.conf'), result, {
      mode: 0o600,
    });
    debug('Config saved.');
  }
```

- [ ] **Step 3: Rewire `getClientConfiguration`**

Replace the `const conf = ...` template literal in `getClientConfiguration` (lines `255-284`) with:

```js
    const conf = ConfigRender.renderClientConf(config.server, client, IMITATE_PROTOCOL);
    return client.legacy ? stripImitationKeys(conf) : conf;
```

Remove the now-unused single-use env imports from the destructure block **only if** nothing else references them: `WG_HOST, WG_PORT, WG_MTU, WG_DEFAULT_DNS, WG_ALLOWED_IPS, WG_PERSISTENT_KEEPALIVE, I1..I5` (these are now read via `config.server` inside the renderers). Keep `WG_HOST` if still used by the first-boot guard in `getConfig` (`if (!WG_HOST) throw`) and keep `WG_DEFAULT_ADDRESS`, `JC..H4`, `S3`, `S4`, `WG_PATH`, `WG_DEVICE`. Run lint to catch leftovers.

- [ ] **Step 4: Verify (unit + manual)**

Run: `cd src && node --test`
Expected: all suites PASS (the pure renderers already cover output; existing suites unaffected).
Run: `cd src && npm run lint`
Expected: no `no-unused-vars` errors (confirms dead env imports were removed).

Manual smoke (Linux/Docker, since `wg-quick` runs): start the server, confirm `wg0.conf` still contains the MASQUERADE/`--dport` rules and a `[Peer]` per client, and that downloading a client config yields the same content as before this task for an unchanged `config.server`. Record the result in the commit body.

- [ ] **Step 5: Commit**

```bash
git add src/config.js src/lib/WireGuard.js
git commit -m "refactor(server-settings): generate wg0.conf + client configs from config.server"
```

---

### Task 6: Apply pipeline — `updateServerSettings` + `regenerateKeypair`

Add the two `WireGuard` methods that validate, classify, persist, and bounce the interface with the correct `down`→`write`→`up` ordering and rollback.

**Files:**
- Modify: `src/lib/WireGuard.js` (add methods near `saveConfig`, `125-129`)

**Interfaces:**
- Consumes: `serverSettings.validateServerSettings/classify` (Tasks 1–2); `getConfig`, `__saveConfig`, `__syncConfig`, `Util.exec` (existing).
- Produces:
  - `async updateServerSettings(patch): { settings, restarted, mustReimport }` — throws `{ statusCode: 400, errors }` on invalid patch.
  - `async regenerateKeypair(): { publicKey, mustReimport: true }`.
  - `async getServerSettings(): object` — editable `config.server` fields, **never** `privateKey`.

- [ ] **Step 1: Write the implementation**

In `src/lib/WireGuard.js`, add `const ServerSettings = require('./serverSettings');` if not already added in Task 3 (it was). Add these methods to the class (after `__syncConfig`, around line 184):

```js
  async getServerSettings() {
    const config = await this.getConfig();
    const s = config.server;
    return {
      host: s.host, port: s.port, mtu: s.mtu, dns: s.dns,
      defaultAddress: s.defaultAddress, allowedIPs: s.allowedIPs,
      persistentKeepalive: s.persistentKeepalive,
      jc: s.jc, jmin: s.jmin, jmax: s.jmax,
      s1: s.s1, s2: s.s2, s3: s.s3, s4: s.s4,
      h1: s.h1, h2: s.h2, h3: s.h3, h4: s.h4,
      i1: s.i1, i2: s.i2, i3: s.i3, i4: s.i4, i5: s.i5,
      publicKey: s.publicKey,
    };
  }

  async updateServerSettings(patch) {
    const config = await this.getConfig();
    const errors = ServerSettings.validateServerSettings(patch, config.server);
    if (Object.keys(errors).length > 0) {
      throw Object.assign(new Error('Invalid server settings'), { statusCode: 400, errors });
    }

    const prev = { ...config.server };
    const diff = ServerSettings.classify(prev, patch);

    if (diff.needsRestart) {
      // Tear down using the CURRENTLY on-disk conf (live firewall rules) BEFORE writing.
      await Util.exec('wg-quick down wg0').catch(() => { });
    }

    Object.assign(config.server, patch);
    await this.__saveConfig(config);

    if (diff.needsRestart) {
      try {
        await Util.exec('wg-quick up wg0');
      } catch (err) {
        // Roll back so a bad value never strands the server offline.
        Object.assign(config.server, prev);
        await this.__saveConfig(config);
        await Util.exec('wg-quick up wg0').catch(() => { });
        throw Object.assign(new Error(`Failed to apply settings: ${err.message}`), { statusCode: 500 });
      }
    } else {
      await this.__syncConfig();
    }

    return { settings: await this.getServerSettings(), restarted: diff.needsRestart, mustReimport: diff.mustReimport };
  }

  async regenerateKeypair() {
    const config = await this.getConfig();
    const privateKey = await Util.exec('wg genkey');
    const publicKey = await Util.exec(`echo ${privateKey} | wg pubkey`, {
      log: 'echo ***hidden*** | wg pubkey',
    });
    await Util.exec('wg-quick down wg0').catch(() => { });
    config.server.privateKey = privateKey;
    config.server.publicKey = publicKey;
    await this.__saveConfig(config);
    await Util.exec('wg-quick up wg0');
    return { publicKey, mustReimport: true };
  }
```

- [ ] **Step 2: Verify (manual — shell side, per CLAUDE.md)**

No unit test (these call `wg-quick`/`wg`). Run: `cd src && node --test` (regression) and `cd src && npm run lint`.
Manual smoke on Linux/Docker:
1. Change `dns` only → response `{ restarted: false, mustReimport: false }`; `wg0.json` updated; clients stay connected.
2. Change `port` → `{ restarted: true, mustReimport: true }`; `wg show wg0` reports the new `listening port`; `wg0.conf` `--dport` rule matches; only one ACCEPT rule for the port (`iptables -S | grep dport` shows no stale old-port rule).
3. Submit an invalid patch (`port: 70000`) → 400 with `errors.port`, `wg0.json` unchanged.
Record results in the commit body.

- [ ] **Step 3: Commit**

```bash
git add src/lib/WireGuard.js
git commit -m "feat(server-settings): updateServerSettings + regenerateKeypair apply pipeline"
```

---

### Task 7: Routes + docs

Expose the three admin-guarded routes in `Server.js`; correct the stale CLAUDE.md "no test suite" sentence.

**Files:**
- Modify: `src/lib/Server.js` (add to `router2`, after the client routes ~`181`)
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: `WireGuard.getServerSettings/updateServerSettings/regenerateKeypair` (Task 6).
- Produces: `GET /api/server-settings`, `POST /api/server-settings`, `POST /api/server-settings/regenerate-keypair` (all under the existing `/api/*` session guard).

- [ ] **Step 1: Add routes**

In `src/lib/Server.js`, append to the `router2` chain (after the last client route, before the chain ends ~line 181 area; match the existing `.post(...)` style):

```js
      .get('/api/server-settings', defineEventHandler(async () => {
        return WireGuard.getServerSettings();
      }))
      .post('/api/server-settings', defineEventHandler(async (event) => {
        const patch = await readBody(event);
        if (!patch || typeof patch !== 'object' || Array.isArray(patch)) {
          throw createError({ status: 400, message: 'Invalid body' });
        }
        for (const key of Object.keys(patch)) {
          if (key === '__proto__' || key === 'constructor' || key === 'prototype') {
            throw createError({ status: 403 });
          }
        }
        try {
          return await WireGuard.updateServerSettings(patch);
        } catch (err) {
          if (err.statusCode === 400) {
            throw createError({ status: 400, message: 'Validation failed', data: { errors: err.errors } });
          }
          throw createError({ status: err.statusCode || 500, message: err.message });
        }
      }))
      .post('/api/server-settings/regenerate-keypair', defineEventHandler(async () => {
        return WireGuard.regenerateKeypair();
      }))
```

- [ ] **Step 2: Fix CLAUDE.md**

In `CLAUDE.md`, replace the sentence:

> The Node app has no test suite — quality relies on ESLint and manual testing.

with:

> The Node app has focused `node:test` unit suites in `src/lib/__tests__/` (run `cd src && node --test`); the shell/`wg-quick` integration side is verified manually.

- [ ] **Step 3: Verify**

Run: `cd src && npm run lint` — no errors.
Run: `cd src && node --test` — all suites PASS.
Manual: `curl` (authenticated session) `GET /api/server-settings` returns fields **without** `privateKey`; `POST` with `{ "dns": "8.8.8.8" }` returns `{ settings, restarted:false, mustReimport:false }`; `POST` with `{ "port": 70000 }` returns 400 with `data.errors.port`.

- [ ] **Step 4: Commit**

```bash
git add src/lib/Server.js CLAUDE.md
git commit -m "feat(server-settings): GET/POST routes + regenerate-keypair; fix CLAUDE.md test note"
```

---

## Self-Review

**Spec coverage:**
- §1 data model + seeding → Task 3 (`seedServerDefaults`, wired into `getConfig`). ✅
- §2 migration (backfill beside `s3/s4`) → Task 3 Step 3b. ✅
- §3 generation reads `config.server` + dynamic iptables hooks + env-override-verbatim → Tasks 4–5. ✅
- §4 apply pipeline, down→write→up ordering, rollback, two outcomes → Task 6. ✅
- §5 three routes, admin-guarded, never return `privateKey`, plain POST regenerate → Task 7. ✅
- §6 validation incl. new `isValidCIDR`/IPv6 (`net.isIP`) and same-/24 `defaultAddress` → Task 1. ✅
- §7 rollback + last-writer-wins (accepted; no new locking) → Task 6 (rollback). ✅
- §8 tests: validate, classify, generation-from-server, migration backfill → Tasks 1,2,4,3. ✅
- Files-touched (serverSettings.js, configRender.js, WireGuard.js, Server.js, config.js, CLAUDE.md, test files) → all tasked. ✅
- Out of scope (WEB PANEL password/session, per-client override, server address edit) → not implemented. ✅

**Placeholder scan:** No TBD/TODO; every code step shows complete code; every command has expected output. ✅

**Type consistency:** `validateServerSettings(patch, current)`, `classify(prev, next)→{changed,needsRestart,mustReimport}`, `seedServerDefaults(server, seeds)`, `renderDefaultHooks(server, env)`, `renderServerConf(server, clients, hooks, imitateProtocol)`, `renderClientConf(server, client, imitateProtocol)`, `updateServerSettings(patch)→{settings,restarted,mustReimport}`, `regenerateKeypair()→{publicKey,mustReimport}`, `getServerSettings()` — names/shapes consistent across producing and consuming tasks. ✅

**Note for the executor:** migration-backfill behavior (Task 3 Step 3b) is exercised only indirectly through `getConfig` (which runs `wg-quick`), so it has no standalone unit test — `seedServerDefaults` is unit-tested in isolation and the wiring is covered by Task 5/6 manual smoke. This is the accepted "shell side stays manual" boundary.
