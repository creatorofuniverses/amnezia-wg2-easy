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
  assert.ok(!S.isValidCIDR('10.8.0.0')); // prefix required
  assert.ok(!S.isValidCIDR('::/129'));
});

test('isValidCIDRList accepts the shipped default', () => {
  assert.ok(S.isValidCIDRList('0.0.0.0/0, ::/0'));
  assert.ok(!S.isValidCIDRList(''));
  assert.ok(!S.isValidCIDRList('0.0.0.0/0, nonsense'));
});

test('validateServerSettings returns {} for a valid patch', () => {
  const patch = {
    host: 'vpn.example.com',
    port: 51820,
    mtu: 1420,
    dns: '1.1.1.1, 2606:4700:4700::1111',
    defaultAddress: '10.8.0.x',
    allowedIPs: '0.0.0.0/0, ::/0',
    persistentKeepalive: 25,
    jc: 4,
    jmin: 40,
    jmax: 70,
    s1: 0,
    s2: 0,
    s3: 20,
    s4: 8,
    h1: {
      min: 100,
      max: 500,
    },
    i1: null,
  };
  assert.deepStrictEqual(S.validateServerSettings(patch, CURRENT), {});
});

test('validateServerSettings flags out-of-range and bad formats', () => {
  const errs = S.validateServerSettings(
    {
      port: 70000,
      mtu: 100,
      allowedIPs: 'bad',
      jmin: 90,
      jmax: 10,
    },
    CURRENT,
  );
  assert.ok(errs.port, 'port out of range');
  assert.ok(errs.mtu, 'mtu below floor');
  assert.ok(errs.allowedIPs, 'bad CIDR list');
  assert.ok(errs.jmax || errs.jmin, 'jmin > jmax');
});

test('defaultAddress must stay in the server /24, template ending in x', () => {
  assert.deepStrictEqual(
    S.validateServerSettings({ defaultAddress: '10.8.0.x' }, CURRENT),
    {},
  );
  assert.ok(
    S.validateServerSettings({ defaultAddress: '10.9.0.x' }, CURRENT).defaultAddress,
    'subnet base change rejected',
  );
  assert.ok(
    S.validateServerSettings({ defaultAddress: '10.8.0.5' }, CURRENT).defaultAddress,
    'must end in x',
  );
});

test('mtu accepts null/empty', () => {
  assert.deepStrictEqual(
    S.validateServerSettings({ mtu: null }, CURRENT),
    {},
  );
  assert.deepStrictEqual(
    S.validateServerSettings({ mtu: '' }, CURRENT),
    {},
  );
});

const PREV = {
  host: 'a.example',
  port: '51820',
  mtu: 1420,
  dns: '1.1.1.1',
  defaultAddress: '10.8.0.x',
  allowedIPs: '0.0.0.0/0',
  persistentKeepalive: '0',
  jc: 4,
  jmin: 40,
  jmax: 70,
  s1: 0,
  s2: 0,
  s3: 20,
  s4: 8,
  h1: {
    min: 1,
    max: 2,
  },
  h2: {
    min: 3,
    max: 4,
  },
  h3: {
    min: 5,
    max: 6,
  },
  h4: {
    min: 7,
    max: 8,
  },
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

test('seedServerDefaults fills only missing keys, preserves existing', () => {
  const server = { port: '51999', host: 'kept.example' };
  const seeds = {
    host: 'seed.example',
    port: '51820',
    mtu: null,
    dns: '1.1.1.1',
    defaultAddress: '10.8.0.x',
    allowedIPs: '0.0.0.0/0, ::/0',
    persistentKeepalive: '0',
    i1: null,
    i2: null,
    i3: null,
    i4: null,
    i5: null,
  };
  S.seedServerDefaults(server, seeds);
  assert.strictEqual(server.host, 'kept.example'); // preserved
  assert.strictEqual(server.port, '51999'); // preserved
  assert.strictEqual(server.dns, '1.1.1.1'); // seeded
  assert.strictEqual(server.allowedIPs, '0.0.0.0/0, ::/0');
  assert.strictEqual(server.mtu, null);
});
