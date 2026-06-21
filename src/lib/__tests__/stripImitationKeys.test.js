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
  'I5 = <dns example.com>',
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
  assert.ok(!out.includes('I5 = <dns'), 'I5 (tag) should be stripped');
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
