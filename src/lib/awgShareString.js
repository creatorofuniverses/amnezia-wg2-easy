'use strict';

const zlib = require('zlib');

const PREFIX = 'awg://v1/';

// Encode a WireGuard/AmneziaWG .conf string into an awg://v1/ share-string:
//   awg://v1/<base64url( zlib( utf8(conf) ) )>
// zlib = RFC 1950 (matches the Android ConfigShare reference); base64url = RFC 4648 §5, no padding.
function encode(confText) {
  const compressed = zlib.deflateSync(Buffer.from(confText, 'utf8'), {
    level: zlib.constants.Z_BEST_COMPRESSION,
  });
  return PREFIX + compressed.toString('base64url');
}

// Decode an awg://v1/ share-string back to the .conf text.
// Tolerant of the standard +/ base64 alphabet and = padding. Throws on a wrong
// prefix or a corrupt payload. Used for tests only — never exposed via the API.
function decode(shareString) {
  if (typeof shareString !== 'string' || !shareString.startsWith(PREFIX)) {
    throw new Error('not an awg://v1 string');
  }
  const body = shareString.slice(PREFIX.length).replace(/-/g, '+').replace(/_/g, '/');
  const compressed = Buffer.from(body, 'base64');
  return zlib.inflateSync(compressed).toString('utf8');
}

module.exports = { encode, decode };
