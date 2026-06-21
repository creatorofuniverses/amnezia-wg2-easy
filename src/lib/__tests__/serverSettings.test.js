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
