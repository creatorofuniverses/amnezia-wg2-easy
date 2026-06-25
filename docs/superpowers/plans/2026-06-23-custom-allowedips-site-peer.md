# Custom AllowedIPs / Site-Relay Peers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a client peer carry custom AllowedIPs (a foreign subnet / specific CIDRs) plus an optional per-peer MASQUERADE, editable from the web UI, so relay/site-to-site topologies work without `awg-peers.service` or hand-written iptables.

**Architecture:** A new pure `clientValidation.js` (imports the existing CIDR validators, adds BigInt-range overlap). `configRender.js` renders the per-peer AllowedIPs override (replace semantics) and per-peer masq rules into PostUp/PostDown. `WireGuard.js` gains `setClientSitePeer` + a shared `__applyWithBounce` helper; site-peer enable/disable/delete bounce the tunnel (so masq rules are added/removed by PostUp/PostDown, never orphaned), and `updateClientAddress`/`createClient` enforce the same overlap invariant. One H3 route + a UI expander.

**Tech Stack:** Node 18+ (CommonJS, `node:test`), H3 routes, Vue 2 (vendored). No new dependencies.

Spec: `docs/superpowers/specs/2026-06-23-custom-allowedips-site-peer-design.md` (rev 2). Review: `…-design.review.md`.

## Global Constraints

- **No new dependencies.** Node stdlib only. `npm run lint` (ESLint `athom`) must stay clean.
- **Site peer = presence of non-empty `allowedIPs`.** No separate boolean.
- **Replace, not merge:** when `allowedIPs` is set it is the peer's full AllowedIPs list; the peer's own `/32` is NOT auto-included.
- **Overlap = hard reject (HTTP 400)** across all peers' effective AllowedIPs, enforced on every mutation (`setClientSitePeer`, `updateClientAddress`, `createClient` auto-assign).
- **Overlap computed for v4 AND v6** uniformly via BigInt network ranges — no v6 hole.
- **Site-peer lifecycle bounces:** any path that adds/removes a site peer from the live config (`setClientSitePeer`, enable/disable/delete of a site peer) takes `wg-quick down → save → up`; normal clients keep the `saveConfig → syncconf` fast path.
- **Apply ordering is down → save → up** (tear down old firewall rules using the on-disk conf before writing the new one), mirroring `updateServerSettings` (`WireGuard.js:242-259`).
- **Masq requires default hooks:** a `WG_POST_UP`/`WG_POST_DOWN` override suppresses the default hook and therefore site-masq rules (documented; out of scope to reconcile).
- **Tests:** pure modules (`clientValidation`, `configRender`) get `node:test` TDD; I/O (bounce/route) and UI follow the app's manual-verification convention.

## File Structure

- **Create:** `src/lib/clientValidation.js` — pure: parse, BigInt CIDR ranges, overlap, `effectiveCidrs`, `isSitePeer`.
- **Create:** `src/lib/__tests__/clientValidation.test.js` — `node:test` suite.
- **Modify:** `src/lib/configRender.js` — AllowedIPs override (`:64`); per-peer masq in `defaultPostUp`/`defaultPostDown`/`renderDefaultHooks` (pass clients).
- **Modify:** `src/lib/__tests__/configRender.test.js` — render override + masq-rule tests.
- **Modify:** `src/lib/WireGuard.js` — `__applyWithBounce`, `setClientSitePeer`, bounce-aware `enable/disable/deleteClient`, overlap in `updateClientAddress`, `createClient` seed + skip; `getClients` returns `siteMasquerade`; pass `config.clients` to `renderDefaultHooks`.
- **Modify:** `src/lib/Server.js` — `PUT /api/wireguard/client/:clientId/allowedips`.
- **Modify:** `src/www/js/api.js` — `setClientSitePeer`.
- **Modify:** `src/www/js/app.js` — `saveClientSitePeer` handler + per-client draft state.
- **Modify:** `src/www/index.html` — the "Advanced / site peer" expander.
- **Modify:** `src/www/js/i18n.js` — expander/masq/help/error keys.

## Verification commands

```bash
cd src && node --test                 # all node:test suites
cd src && node --test lib/__tests__/clientValidation.test.js   # Task 1
cd src && node --test lib/__tests__/configRender.test.js       # Task 2
cd src && npm run lint                # ESLint, must stay clean
cd src && npm run serve               # manual UI/route checks (Tasks 3-5)
```

---

### Task 1: `clientValidation.js` — parse + BigInt overlap (pure)

**Files:**
- Create: `src/lib/clientValidation.js`
- Create: `src/lib/__tests__/clientValidation.test.js`

**Interfaces:**
- Produces (CommonJS `module.exports`):
  - `parseAllowedIPs(str: string): string[]`
  - `cidrRange(cidr: string): { lo: bigint, hi: bigint, v: 4|6 }`
  - `overlaps(a, b): boolean`
  - `findOverlap(candidateCidrs: string[], others: Array<{clientId, name, cidrs: string[]}>): { with: string } | null`
  - `validateClientAllowedIPs(allowedIPs: string|null, others): { allowedIPs?: string }`
  - `effectiveCidrs(client: {allowedIPs?: string, address: string}): string[]`
  - `isSitePeer(client): boolean`
- Consumes: `isValidCIDR` from `./serverSettings` (already exported).

- [ ] **Step 1: Write the failing test**

Create `src/lib/__tests__/clientValidation.test.js`:

```js
'use strict';

const { test } = require('node:test');
const assert = require('node:assert');

const V = require('../clientValidation');

test('parseAllowedIPs splits, trims, drops empties', () => {
  assert.deepStrictEqual(V.parseAllowedIPs('10.20.0.0/24, 192.168.1.0/24'),
    ['10.20.0.0/24', '192.168.1.0/24']);
  assert.deepStrictEqual(V.parseAllowedIPs(' '), []);
  assert.deepStrictEqual(V.parseAllowedIPs(null), []);
});

test('cidrRange computes v4 network range', () => {
  const r = V.cidrRange('10.20.0.0/24');
  assert.strictEqual(r.v, 4);
  assert.strictEqual(r.lo, V.cidrRange('10.20.0.0/32').lo);
  assert.strictEqual(r.hi, V.cidrRange('10.20.0.255/32').lo);
});

test('cidrRange normalizes a non-network host address', () => {
  // 10.20.0.5/24 -> network 10.20.0.0 .. 10.20.0.255
  const r = V.cidrRange('10.20.0.5/24');
  assert.strictEqual(r.lo, V.cidrRange('10.20.0.0/32').lo);
  assert.strictEqual(r.hi, V.cidrRange('10.20.0.255/32').lo);
});

test('overlaps: containment, intersection, /32-in-subnet, disjoint, cross-family', () => {
  const a = V.cidrRange('10.20.0.0/24');
  assert.ok(V.overlaps(a, V.cidrRange('10.20.0.0/25')));   // containment
  assert.ok(V.overlaps(a, V.cidrRange('10.20.0.5/32')));   // /32 inside subnet
  assert.ok(!V.overlaps(a, V.cidrRange('10.21.0.0/24')));  // disjoint
  assert.ok(!V.overlaps(a, V.cidrRange('fd00::/64')));     // different family never overlaps
});

test('overlaps computes IPv6 ranges (no v6 hole)', () => {
  const a = V.cidrRange('fd00::/64');
  assert.ok(V.overlaps(a, V.cidrRange('fd00::1/128')));
  assert.ok(!V.overlaps(a, V.cidrRange('fd01::/64')));
});

test('findOverlap reports the conflicting peer name', () => {
  const others = [{ clientId: 'x', name: 'entry-a', cidrs: ['10.20.0.0/24'] }];
  assert.deepStrictEqual(V.findOverlap(['10.20.0.128/25'], others), { with: 'entry-a' });
  assert.strictEqual(V.findOverlap(['10.30.0.0/24'], others), null);
});

test('validateClientAllowedIPs: malformed, overlap, clean, empty', () => {
  const others = [{ clientId: 'x', name: 'entry-a', cidrs: ['10.20.0.0/24'] }];
  assert.ok(V.validateClientAllowedIPs('not-a-cidr', others).allowedIPs);
  assert.ok(V.validateClientAllowedIPs('10.20.0.0/24', others).allowedIPs);     // overlap
  assert.deepStrictEqual(V.validateClientAllowedIPs('10.30.0.0/24', others), {}); // clean
  assert.deepStrictEqual(V.validateClientAllowedIPs(null, others), {});           // empty -> normal
});

test('effectiveCidrs falls back to /32 for a normal client', () => {
  assert.deepStrictEqual(V.effectiveCidrs({ address: '10.8.0.5' }), ['10.8.0.5/32']);
  assert.deepStrictEqual(V.effectiveCidrs({ address: '10.8.0.5', allowedIPs: '10.20.0.0/24' }),
    ['10.20.0.0/24']);
});

test('isSitePeer reflects non-empty allowedIPs', () => {
  assert.ok(V.isSitePeer({ allowedIPs: '10.20.0.0/24' }));
  assert.ok(!V.isSitePeer({ allowedIPs: '' }));
  assert.ok(!V.isSitePeer({}));
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && node --test lib/__tests__/clientValidation.test.js`
Expected: FAIL — `Cannot find module '../clientValidation'`.

- [ ] **Step 3: Write minimal implementation**

Create `src/lib/clientValidation.js`:

```js
'use strict';

const { isValidCIDR } = require('./serverSettings');

function parseAllowedIPs(str) {
  if (typeof str !== 'string') return [];
  return str.split(',').map((s) => s.trim()).filter((s) => s !== '');
}

function ipv4ToBigInt(ip) {
  return ip.split('.').reduce((acc, o) => (acc << 8n) + BigInt(Number(o)), 0n);
}

function ipv6ToBigInt(ip) {
  const halves = ip.split('::');
  const left = halves[0] ? halves[0].split(':') : [];
  const right = halves.length === 2 && halves[1] ? halves[1].split(':') : [];
  const groups = halves.length === 2
    ? [...left, ...Array(8 - left.length - right.length).fill('0'), ...right]
    : left;
  return groups.reduce((acc, g) => (acc << 16n) + BigInt(parseInt(g, 16) || 0), 0n);
}

function cidrRange(cidr) {
  const [ip, prefixStr] = cidr.split('/');
  const v = ip.includes(':') ? 6 : 4;
  const bits = v === 6 ? 128 : 32;
  const base = v === 6 ? ipv6ToBigInt(ip) : ipv4ToBigInt(ip);
  const hostBits = BigInt(bits - Number(prefixStr));
  const lo = (base >> hostBits) << hostBits;
  const hi = lo + ((1n << hostBits) - 1n);
  return { lo, hi, v };
}

function overlaps(a, b) {
  return a.v === b.v && a.lo <= b.hi && b.lo <= a.hi;
}

function findOverlap(candidateCidrs, others) {
  const cand = candidateCidrs.map(cidrRange);
  for (const other of others) {
    for (const oc of other.cidrs.map(cidrRange)) {
      if (cand.some((cc) => overlaps(cc, oc))) {
        return { with: other.name || other.clientId };
      }
    }
  }
  return null;
}

function validateClientAllowedIPs(allowedIPs, others) {
  const errors = {};
  const cidrs = parseAllowedIPs(allowedIPs);
  if (cidrs.length === 0) return errors; // empty -> normal client, nothing to check
  if (!cidrs.every(isValidCIDR)) {
    errors.allowedIPs = 'Comma-separated CIDRs only';
    return errors;
  }
  const conflict = findOverlap(cidrs, others);
  if (conflict) errors.allowedIPs = `AllowedIPs overlaps ${conflict.with}`;
  return errors;
}

function effectiveCidrs(client) {
  const parsed = parseAllowedIPs(client.allowedIPs);
  return parsed.length ? parsed : [`${client.address}/32`];
}

function isSitePeer(client) {
  return !!(client && client.allowedIPs && String(client.allowedIPs).trim());
}

module.exports = {
  parseAllowedIPs, cidrRange, overlaps, findOverlap,
  validateClientAllowedIPs, effectiveCidrs, isSitePeer,
};
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && node --test lib/__tests__/clientValidation.test.js`
Expected: PASS (all cases).

- [ ] **Step 5: Lint**

Run: `cd src && npm run lint`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add src/lib/clientValidation.js src/lib/__tests__/clientValidation.test.js
git commit -m "feat(clients): clientValidation — CIDR parse + v4/v6 overlap (Round 2 Task 1)"
```

---

### Task 2: `configRender.js` — AllowedIPs override + per-peer masq (pure)

**Files:**
- Modify: `src/lib/configRender.js`
- Modify: `src/lib/__tests__/configRender.test.js`

**Interfaces:**
- Changes: `defaultPostUp(server, device, clients)`, `defaultPostDown(server, device, clients)`, `renderDefaultHooks(server, env, clients)` — new trailing `clients` param (a `{ id: client }` map; default `{}`).
- `renderServerConf` unchanged signature; line 64 uses the override.
- Consumes: nothing new (pure string building).

- [ ] **Step 1: Write the failing test**

Add to `src/lib/__tests__/configRender.test.js` (reuse the existing `SERVER`/`CLIENT` fixtures; add a clients map):

```js
const R = require('../configRender'); // (already required at top of the file)

test('renderServerConf uses custom AllowedIPs when set, else /32', () => {
  const clients = {
    normal: { enabled: true, name: 'n', publicKey: 'k1', address: '10.8.0.2' },
    site: { enabled: true, name: 's', publicKey: 'k2', address: '10.8.0.3', allowedIPs: '10.20.0.0/24' },
  };
  const hooks = R.renderDefaultHooks(SERVER, { device: 'eth0' }, clients);
  const conf = R.renderServerConf(SERVER, clients, hooks, 'none');
  assert.ok(conf.includes('AllowedIPs = 10.8.0.2/32'), 'normal client keeps /32');
  assert.ok(conf.includes('AllowedIPs = 10.20.0.0/24'), 'site peer uses override');
  assert.ok(!conf.includes('AllowedIPs = 10.8.0.3/32'), 'site peer does NOT also emit its /32 (replace)');
});

test('renderDefaultHooks emits masq rule only for siteMasquerade peers', () => {
  const clients = {
    a: { enabled: true, name: 'a', publicKey: 'k', address: '10.8.0.2', allowedIPs: '10.20.0.0/24', siteMasquerade: true },
    b: { enabled: true, name: 'b', publicKey: 'k', address: '10.8.0.3', allowedIPs: '10.30.0.0/24', siteMasquerade: false },
    c: { enabled: false, name: 'c', publicKey: 'k', address: '10.8.0.4', allowedIPs: '10.40.0.0/24', siteMasquerade: true },
  };
  const hooks = R.renderDefaultHooks(SERVER, { device: 'eth0' }, clients);
  assert.ok(hooks.postUp.includes('-A POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE'), 'masq peer A');
  assert.ok(!hooks.postUp.includes('10.30.0.0/24'), 'no masq for siteMasquerade:false');
  assert.ok(!hooks.postUp.includes('10.40.0.0/24'), 'no masq for disabled peer');
  assert.ok(hooks.postDown.includes('-D POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE'), 'postDown mirrors');
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src && node --test lib/__tests__/configRender.test.js`
Expected: FAIL — masq rule absent / `renderDefaultHooks` ignores 3rd arg.

- [ ] **Step 3: Implement**

In `src/lib/configRender.js`:

Add a helper above `defaultPostUp`:

```js
const siteMasqRules = (clients, device, op) => Object.values(clients || {})
  .filter((c) => c.enabled && c.siteMasquerade && c.allowedIPs)
  .flatMap((c) => c.allowedIPs.split(',').map((s) => s.trim()).filter(Boolean)
    .map((cidr) => `iptables -t nat -${op} POSTROUTING -s ${cidr} -o ${device} -j MASQUERADE;`))
  .join(' ');
```

Change the two hook builders to take `clients` and append the rules:

```js
const defaultPostUp = (server, device, clients) => `
iptables -t nat -A POSTROUTING -s ${server.defaultAddress.replace('x', '0')}/24 -o ${device} -j MASQUERADE;
iptables -A INPUT -p udp -m udp --dport ${server.port} -j ACCEPT;
iptables -A FORWARD -i wg0 -j ACCEPT;
iptables -A FORWARD -o wg0 -j ACCEPT;
${siteMasqRules(clients, device, 'A')}
`.split('\n').join(' ');

const defaultPostDown = (server, device, clients) => `
iptables -t nat -D POSTROUTING -s ${server.defaultAddress.replace('x', '0')}/24 -o ${device} -j MASQUERADE;
iptables -D INPUT -p udp -m udp --dport ${server.port} -j ACCEPT;
iptables -D FORWARD -i wg0 -j ACCEPT;
iptables -D FORWARD -o wg0 -j ACCEPT;
${siteMasqRules(clients, device, 'D')}
`.split('\n').join(' ');
```

Thread `clients` through `renderDefaultHooks`:

```js
function renderDefaultHooks(server, env = {}, clients = {}) {
  const device = env.device || 'eth0';
  return {
    preUp: pick(env.preUp, ''),
    postUp: pick(env.postUp, defaultPostUp(server, device, clients)),
    preDown: pick(env.preDown, ''),
    postDown: pick(env.postDown, defaultPostDown(server, device, clients)),
  };
}
```

Change the server-conf AllowedIPs line (`:64`):

```js
}AllowedIPs = ${client.allowedIPs && client.allowedIPs.trim() ? client.allowedIPs : `${client.address}/32`}`;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd src && node --test lib/__tests__/configRender.test.js`
Expected: PASS.

- [ ] **Step 5: Lint, then commit**

```bash
cd src && npm run lint
git add src/lib/configRender.js src/lib/__tests__/configRender.test.js
git commit -m "feat(render): per-peer AllowedIPs override + siteMasquerade rules (Round 2 Task 2)"
```

---

### Task 3: `WireGuard.js` — apply path, bounce-aware lifecycle, overlap gating

**Files:**
- Modify: `src/lib/WireGuard.js`

**Interfaces:**
- Consumes: `clientValidation` (Task 1), the new `renderDefaultHooks(…, clients)` (Task 2).
- Produces: `setClientSitePeer({ clientId, allowedIPs, siteMasquerade }): Promise<{ client, mustReimport: false }>` (consumed by Task 4).

Note: these methods invoke `wg-quick`/`wg` via `Util.exec`, so they follow the app's **manual-verification** convention (no `node:test`); the pure validation/render they call is already covered by Tasks 1–2.

- [ ] **Step 1: Import clientValidation**

At the top of `src/lib/WireGuard.js`, near the other `require`s:

```js
const ClientValidation = require('./clientValidation');
```

- [ ] **Step 2: Pass clients to the hook renderer**

In `__saveConfig` (`:171`), change:

```js
const hooks = ConfigRender.renderDefaultHooks(config.server, {
  device: WG_DEVICE,
  preUp: WG_PRE_UP,
  postUp: WG_POST_UP,
  preDown: WG_PRE_DOWN,
  postDown: WG_POST_DOWN,
}, config.clients);
```

- [ ] **Step 3: Add the shared bounce helper**

Add a private method (e.g. after `__syncConfig`):

```js
// Apply a config mutation with a full tunnel bounce + rollback.
// `mutate(config)` changes the shared config in place and returns a rollback fn.
// Order: down (tears down OLD on-disk firewall rules) -> mutate+save -> up.
async __applyWithBounce(mutate) {
  const config = await this.getConfig();
  await Util.exec('wg-quick down wg0').catch(() => { });
  const rollback = mutate(config);
  await this.__saveConfig(config);
  try {
    await Util.exec('wg-quick up wg0');
  } catch (err) {
    rollback();
    await this.__saveConfig(config);
    await Util.exec('wg-quick up wg0').catch(() => { });
    throw Object.assign(new Error(`Failed to apply site-peer change: ${err.message}`), { statusCode: 500 });
  }
}
```

- [ ] **Step 4: `setClientSitePeer`**

Add the method (near `setClientLegacy`):

```js
async setClientSitePeer({ clientId, allowedIPs, siteMasquerade }) {
  const config = await this.getConfig();
  const client = await this.getClient({ clientId });

  const norm = (typeof allowedIPs === 'string' && allowedIPs.trim()) ? allowedIPs.trim() : null;
  const masq = norm ? !!siteMasquerade : false;

  const others = Object.entries(config.clients)
    .filter(([id]) => id !== clientId)
    .map(([id, c]) => ({ clientId: id, name: c.name, cidrs: ClientValidation.effectiveCidrs(c) }));
  const errors = ClientValidation.validateClientAllowedIPs(norm, others);
  if (Object.keys(errors).length > 0) {
    throw Object.assign(new Error('Invalid AllowedIPs'), { statusCode: 400, errors });
  }

  const prevAllowed = client.allowedIPs;
  const prevMasq = client.siteMasquerade;
  await this.__applyWithBounce(() => {
    client.allowedIPs = norm;
    client.siteMasquerade = masq;
    client.updatedAt = new Date();
    return () => { client.allowedIPs = prevAllowed; client.siteMasquerade = prevMasq; };
  });

  return { client, mustReimport: false };
}
```

- [ ] **Step 5: Make enable/disable/delete bounce-aware**

Replace `enableClient`, `disableClient`, `deleteClient` with:

```js
async enableClient({ clientId }) {
  const client = await this.getClient({ clientId });
  if (ClientValidation.isSitePeer(client)) {
    await this.__applyWithBounce(() => {
      const prev = client.enabled;
      client.enabled = true; client.updatedAt = new Date();
      return () => { client.enabled = prev; };
    });
    return;
  }
  client.enabled = true;
  client.updatedAt = new Date();
  await this.saveConfig();
}

async disableClient({ clientId }) {
  const client = await this.getClient({ clientId });
  if (ClientValidation.isSitePeer(client)) {
    await this.__applyWithBounce(() => {
      const prev = client.enabled;
      client.enabled = false; client.updatedAt = new Date();
      return () => { client.enabled = prev; };
    });
    return;
  }
  client.enabled = false;
  client.updatedAt = new Date();
  await this.saveConfig();
}

async deleteClient({ clientId }) {
  const config = await this.getConfig();
  const client = config.clients[clientId];
  if (!client) return;
  if (ClientValidation.isSitePeer(client)) {
    await this.__applyWithBounce((cfg) => {
      const removed = cfg.clients[clientId];
      delete cfg.clients[clientId];
      return () => { cfg.clients[clientId] = removed; };
    });
    return;
  }
  delete config.clients[clientId];
  await this.saveConfig();
}
```

- [ ] **Step 6: Overlap gate in `updateClientAddress`**

Replace `updateClientAddress` (`:476`) with:

```js
async updateClientAddress({ clientId, address }) {
  const config = await this.getConfig();
  const client = await this.getClient({ clientId });

  if (!Util.isValidIPv4(address)) {
    throw new ServerError(`Invalid Address: ${address}`, 400);
  }
  const others = Object.entries(config.clients)
    .filter(([id]) => id !== clientId)
    .map(([id, c]) => ({ clientId: id, name: c.name, cidrs: ClientValidation.effectiveCidrs(c) }));
  const conflict = ClientValidation.findOverlap([`${address}/32`], others);
  if (conflict) {
    throw new ServerError(`Address ${address} overlaps ${conflict.with}`, 400);
  }

  client.address = address;
  client.updatedAt = new Date();
  await this.saveConfig();
}
```

- [ ] **Step 7: Seed new fields + skip overlapping auto-addresses in `createClient`**

In `createClient`, change the address-scan loop (`:391-405`) to skip site-peer overlaps, and add the new fields to the client object (`:409-422`):

```js
// Calculate next IP (skip taken addresses and any that fall inside a site peer's subnet)
let address;
const siteOthers = Object.entries(config.clients)
  .filter(([, c]) => ClientValidation.isSitePeer(c))
  .map(([id, c]) => ({ clientId: id, name: c.name, cidrs: ClientValidation.effectiveCidrs(c) }));
for (let i = 2; i < 255; i++) {
  const candidate = WG_DEFAULT_ADDRESS.replace('x', i);
  if (Object.values(config.clients).some((c) => c.address === candidate)) continue;
  if (ClientValidation.findOverlap([`${candidate}/32`], siteOthers)) continue;
  address = candidate;
  break;
}
```

And in the `const client = { … }` literal, alongside `legacy: false`:

```js
      enabled: true,
      legacy: false,
      allowedIPs: null,
      siteMasquerade: false,
```

- [ ] **Step 8: Return `siteMasquerade` from `getClients`**

In the `getClients` map (`:302`, next to `allowedIPs: client.allowedIPs`):

```js
      allowedIPs: client.allowedIPs,
      siteMasquerade: client.siteMasquerade === true,
```

- [ ] **Step 9: Lint + manual verification**

```bash
cd src && npm run lint
cd src && WG_HOST=127.0.0.1 npm run serve   # then exercise via the UI/curl in Task 4
```

Manual checks (with the dev server up, or on a real host):
- Set a client's AllowedIPs to `10.20.0.0/24` + masq on → tunnel bounces → `ip route show dev wg0` lists `10.20.0.0/24`; `iptables -t nat -S POSTROUTING` shows `-s 10.20.0.0/24 … MASQUERADE`.
- Disable that client → bounce → the masq rule is **gone** (not orphaned).
- Try to set a second client's AllowedIPs to an overlapping `10.20.0.128/25` → 400 rejected.
- Try `updateClientAddress` into `10.20.0.5` → 400 rejected.

- [ ] **Step 10: Commit**

```bash
git add src/lib/WireGuard.js
git commit -m "feat(clients): setClientSitePeer + bounce-aware lifecycle + overlap gating (Round 2 Task 3)"
```

---

### Task 4: `Server.js` — the route

**Files:**
- Modify: `src/lib/Server.js`

**Interfaces:**
- Consumes: `WireGuard.setClientSitePeer` (Task 3).
- Produces: `PUT /api/wireguard/client/:clientId/allowedips` (consumed by Task 5).

- [ ] **Step 1: Add the route**

After the `…/address` route (`:223-231`), add:

```js
      .put('/api/wireguard/client/:clientId/allowedips', defineEventHandler(async (event) => {
        const clientId = getRouterParam(event, 'clientId');
        if (clientId === '__proto__' || clientId === 'constructor' || clientId === 'prototype') {
          throw createError({ status: 403 });
        }
        const { allowedIPs, siteMasquerade } = await readBody(event);
        try {
          return await WireGuard.setClientSitePeer({ clientId, allowedIPs, siteMasquerade });
        } catch (err) {
          if (err.statusCode === 400) {
            throw createError({ status: 400, message: 'Validation failed', data: { errors: err.errors } });
          }
          throw createError({ status: err.statusCode || 500, message: err.message });
        }
      }))
```

- [ ] **Step 2: Lint + manual verify**

```bash
cd src && npm run lint
cd src && WG_HOST=127.0.0.1 npm run serve
# in another shell (PASSWORD unset in dev):
curl -s -X PUT localhost:51821/api/wireguard/client/<id>/allowedips \
  -H 'Content-Type: application/json' \
  -d '{"allowedIPs":"10.20.0.0/24","siteMasquerade":true}'
# expect {"client":{…},"mustReimport":false}; an overlapping value -> HTTP 400 with data.errors.allowedIPs
```

- [ ] **Step 3: Commit**

```bash
git add src/lib/Server.js
git commit -m "feat(api): PUT client/:id/allowedips site-peer route (Round 2 Task 4)"
```

---

### Task 5: Web UI — expander, API client, handler, i18n

**Files:**
- Modify: `src/www/js/api.js`
- Modify: `src/www/js/app.js`
- Modify: `src/www/index.html`
- Modify: `src/www/js/i18n.js`

**Interfaces:**
- Consumes: the route from Task 4; the existing `svIsCIDR` (`app.js:39`), `fieldErr` (`app.js:455`), and the server-settings AllowedIPs input pattern (`index.html`).

- [ ] **Step 1: API client method**

In `src/www/js/api.js`, after `updateClientAddress` (`:160`):

```js
  async setClientSitePeer({ clientId, allowedIPs, siteMasquerade }) {
    return this.call({
      method: 'put',
      path: `/wireguard/client/${clientId}/allowedips`,
      body: { allowedIPs, siteMasquerade },
    });
  }
```

- [ ] **Step 2: app.js handler**

In `src/www/js/app.js`, after `updateClientAddress` (`:408-412`):

```js
    saveClientSitePeer(client, allowedIPs, siteMasquerade) {
      // client-side guard mirrors the server (svIsCIDR already exists)
      const list = String(allowedIPs || '').split(',').map((s) => s.trim()).filter(Boolean);
      if (list.length && !list.every(svIsCIDR)) {
        alert(this.$t('allowedIPsInvalid'));
        return;
      }
      this.api.setClientSitePeer({ clientId: client.id, allowedIPs, siteMasquerade })
        .catch((err) => alert((err.fieldErrors && err.fieldErrors.allowedIPs) || err.message || err.toString()))
        .finally(() => this.refresh().catch(console.error));
    },
```

- [ ] **Step 3: index.html expander**

In the client row template, following the existing address-edit affordance, add a collapsed-by-default "Advanced / site peer" block bound to local draft state. Minimal markup (adapt classes to the surrounding row styles):

```html
<details class="text-xs mt-1">
  <summary class="cursor-pointer text-gray-400">{{ $t('advancedSitePeer') }}
    <span v-if="client.allowedIPs" class="ml-1 px-1 rounded bg-gray-200 text-gray-600">site</span>
  </summary>
  <div class="mt-1 flex flex-col gap-1">
    <input
      :value="client.allowedIPs || ''"
      @input="client._allowedIPsDraft = $event.target.value"
      :placeholder="$t('allowedIPsPlaceholder')"
      class="border rounded px-1 py-0.5" />
    <p class="text-gray-400">{{ $t('allowedIPsHelp') }}</p>
    <label class="flex items-center gap-1">
      <input type="checkbox"
        :checked="client.siteMasquerade"
        @change="client._masqDraft = $event.target.checked" />
      {{ $t('masqueradePeer') }}
    </label>
    <button
      @click="saveClientSitePeer(client,
        client._allowedIPsDraft !== undefined ? client._allowedIPsDraft : (client.allowedIPs || ''),
        client._masqDraft !== undefined ? client._masqDraft : client.siteMasquerade)"
      class="self-start border rounded px-2 py-0.5">{{ $t('save') }}</button>
  </div>
</details>
```

(Exact styling should match the existing row controls; the behaviour — draft inputs + a save button calling `saveClientSitePeer` — is what matters.)

- [ ] **Step 4: i18n keys**

In `src/www/js/i18n.js`, add to the `en` block (other locales fall back to en):

```js
    advancedSitePeer: 'Advanced / site peer',
    allowedIPsPlaceholder: '10.20.0.0/24, …',
    allowedIPsHelp: 'Replaces the peer’s /32 — include it in the list if you still need it.',
    allowedIPsInvalid: 'AllowedIPs must be comma-separated CIDRs.',
    masqueradePeer: 'Masquerade this peer’s traffic',
```

- [ ] **Step 5: Rebuild CSS (if Tailwind classes were added) + lint**

```bash
cd src && npm run buildcss
cd src && npm run lint
```

- [ ] **Step 6: Manual verification**

```bash
cd src && WG_HOST=127.0.0.1 npm run serve
```
- Create a client → expand "Advanced / site peer" → enter `10.20.0.0/24`, tick masquerade, Save → row shows the `site` chip; the tunnel bounced.
- Re-enter an overlapping CIDR on another client → inline/alert error, no change.
- Clear the AllowedIPs field + Save → reverts to a normal `/32` client (chip gone).

- [ ] **Step 7: Commit**

```bash
git add src/www/js/api.js src/www/js/app.js src/www/index.html src/www/js/i18n.js src/www/css/app.css
git commit -m "feat(ui): site-peer expander (AllowedIPs + masquerade) (Round 2 Task 5)"
```

---

### Task 6: Wrap-up — docs + roadmap

**Files:**
- Modify: `docs/superpowers/plans/2026-06-23-fork-notes-roadmap.md`
- Modify: `docs/custom-allowedips-site-peer.md`
- Modify: `CLAUDE.md` / `README.md` (env + feature note)

- [ ] **Step 1:** Mark Round 2 done in the roadmap; note the `WG_POST_UP`/`WG_POST_DOWN`-override caveat.
- [ ] **Step 2:** Update `docs/custom-allowedips-site-peer.md` status → implemented (link the plan + spec).
- [ ] **Step 3:** Add a short "site peers / custom AllowedIPs" note to `README.md` (and the masq-needs-default-hooks caveat).
- [ ] **Step 4:** Commit.

```bash
git add docs/ CLAUDE.md README.md
git commit -m "docs: mark Round 2 (custom AllowedIPs / site peers) implemented"
```

---

## Out of scope (per spec)

- No-bounce live delta apply (bounce chosen).
- Reconciling custom `WG_POST_UP`/`WG_POST_DOWN` overrides with site masq (mutually exclusive; documented).
- Per-peer DNS / routes / keepalive overrides.
- Styled QR / share-string changes for site peers.
