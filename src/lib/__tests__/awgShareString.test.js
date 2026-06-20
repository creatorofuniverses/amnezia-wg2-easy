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
