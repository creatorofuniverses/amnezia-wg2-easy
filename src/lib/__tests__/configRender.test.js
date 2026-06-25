'use strict';

const { test } = require('node:test');
const assert = require('node:assert');

const R = require('../configRender');

const SERVER = {
  privateKey: 'srvPriv=',
  publicKey: 'srvPub=',
  address: '10.8.0.1',
  port: '51820',
  host: 'vpn.example.com',
  mtu: 1420,
  dns: '1.1.1.1',
  defaultAddress: '10.8.0.x',
  allowedIPs: '0.0.0.0/0, ::/0',
  persistentKeepalive: '25',
  jc: 4,
  jmin: 40,
  jmax: 70,
  s1: 0,
  s2: 0,
  s3: 20,
  s4: 8,
  h1: { min: 100, max: 500 },
  h2: { min: 600, max: 900 },
  h3: { min: 1000, max: 1500 },
  h4: { min: 1600, max: 2000 },
  i1: null,
  i2: null,
  i3: null,
  i4: null,
  i5: null,
};
const CLIENT = {
  name: 'phone',
  address: '10.8.0.2',
  publicKey: 'cliPub=',
  privateKey: 'cliPriv=',
  preSharedKey: '',
  enabled: true,
};

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

test('renderServerConf uses custom AllowedIPs when set, else /32', () => {
  const clients = {
    normal: {
      enabled: true, name: 'n', publicKey: 'k1', address: '10.8.0.2',
    },
    site: {
      enabled: true, name: 's', publicKey: 'k2', address: '10.8.0.3', allowedIPs: '10.20.0.0/24',
    },
  };
  const hooks = R.renderDefaultHooks(SERVER, { device: 'eth0' }, clients);
  const conf = R.renderServerConf(SERVER, clients, hooks, 'none');
  assert.ok(conf.includes('AllowedIPs = 10.8.0.2/32'), 'normal client keeps /32');
  assert.ok(conf.includes('AllowedIPs = 10.20.0.0/24'), 'site peer uses override');
  assert.ok(!conf.includes('AllowedIPs = 10.8.0.3/32'), 'site peer does NOT also emit its /32 (replace)');
});

test('renderDefaultHooks emits masq rule only for siteMasquerade peers', () => {
  const clients = {
    a: {
      enabled: true, name: 'a', publicKey: 'k', address: '10.8.0.2', allowedIPs: '10.20.0.0/24', siteMasquerade: true,
    },
    b: {
      enabled: true, name: 'b', publicKey: 'k', address: '10.8.0.3', allowedIPs: '10.30.0.0/24', siteMasquerade: false,
    },
    c: {
      enabled: false, name: 'c', publicKey: 'k', address: '10.8.0.4', allowedIPs: '10.40.0.0/24', siteMasquerade: true,
    },
  };
  const hooks = R.renderDefaultHooks(SERVER, { device: 'eth0' }, clients);
  assert.ok(hooks.postUp.includes('-A POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE'), 'masq peer A');
  assert.ok(!hooks.postUp.includes('10.30.0.0/24'), 'no masq for siteMasquerade:false');
  assert.ok(!hooks.postUp.includes('10.40.0.0/24'), 'no masq for disabled peer');
  assert.ok(hooks.postDown.includes('-D POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE'), 'postDown mirrors');
});

// Regression: site-masq rules leaked + duplicated because wg-quick runs the
// hooks as one ';'-joined line under `set -e`. A bare `-D` for an absent rule
// aborted the rest of the chain (skipping the site rule, rendered last), and a
// bare `-A` re-appended on every `up`. Rules must be idempotent + chain-safe.
test('firewall rules are idempotent and chain-safe', () => {
  const clients = {
    a: {
      enabled: true, name: 'a', publicKey: 'k', address: '10.8.0.2', allowedIPs: '10.20.0.0/24', siteMasquerade: true,
    },
  };
  const { postUp, postDown } = R.renderDefaultHooks(SERVER, { device: 'eth0' }, clients);

  // Adds are check-then-add: a repeated `up` can't stack a duplicate rule.
  assert.ok(
    postUp.includes('iptables -t nat -C POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE'),
    'site masq add is guarded by -C',
  );
  assert.ok(
    postUp.includes('iptables -t nat -C POSTROUTING -s 10.8.0.0/24 -o eth0 -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s 10.8.0.0/24 -o eth0 -j MASQUERADE'),
    'default masq add is guarded by -C',
  );

  // Deletes loop until gone (clears pre-existing duplicates) and `|| break`
  // keeps one failure from aborting the rest of the set -e chain.
  assert.ok(
    postDown.includes('while iptables -t nat -C POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE 2>/dev/null; do iptables -t nat -D POSTROUTING -s 10.20.0.0/24 -o eth0 -j MASQUERADE || break; done'),
    'site masq delete is a self-healing loop',
  );
  assert.ok(postDown.includes('|| break; done'), 'deletes never abort the chain');
});
