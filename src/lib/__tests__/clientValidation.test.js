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
  assert.ok(V.overlaps(a, V.cidrRange('10.20.0.0/25'))); // containment
  assert.ok(V.overlaps(a, V.cidrRange('10.20.0.5/32'))); // /32 inside subnet
  assert.ok(!V.overlaps(a, V.cidrRange('10.21.0.0/24'))); // disjoint
  assert.ok(!V.overlaps(a, V.cidrRange('fd00::/64'))); // different family never overlaps
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
  assert.ok(V.validateClientAllowedIPs('10.20.0.0/24', others).allowedIPs); // overlap
  assert.deepStrictEqual(V.validateClientAllowedIPs('10.30.0.0/24', others), {}); // clean
  assert.deepStrictEqual(V.validateClientAllowedIPs(null, others), {}); // empty -> normal
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
