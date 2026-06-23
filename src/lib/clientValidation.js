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
  parseAllowedIPs,
  cidrRange,
  overlaps,
  findOverlap,
  validateClientAllowedIPs,
  effectiveCidrs,
  isSitePeer,
};
