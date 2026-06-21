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

const SERVER_SEED_KEYS = ['host', 'port', 'mtu', 'dns', 'defaultAddress', 'allowedIPs', 'persistentKeepalive', 'i1', 'i2', 'i3', 'i4', 'i5'];

function seedServerDefaults(server, seeds) {
  for (const key of Object.keys(seeds)) {
    if (server[key] === undefined) server[key] = seeds[key];
  }
  return server;
}

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

module.exports = {
  H_SPACE_MIN,
  H_SPACE_MAX,
  isValidIP,
  isValidIPList,
  isValidCIDR,
  isValidCIDRList,
  isValidHostname,
  validateServerSettings,
  SERVER_SEED_KEYS,
  seedServerDefaults,
  RESTART_FIELDS,
  REIMPORT_FIELDS,
  classify,
};
