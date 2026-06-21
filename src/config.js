'use strict';

const { release } = require('./package.json');

module.exports.CHECK_UPDATE = process.env.CHECK_UPDATE ? process.env.CHECK_UPDATE.toLowerCase() === 'true' : true;
module.exports.RELEASE = release;
module.exports.PORT = process.env.PORT || '51821';
module.exports.WEBUI_HOST = process.env.WEBUI_HOST || '0.0.0.0';
module.exports.PASSWORD = process.env.PASSWORD;
module.exports.WG_PATH = process.env.WG_PATH || '/etc/amnezia/amneziawg/';
module.exports.WG_DEVICE = process.env.WG_DEVICE || 'eth0';
module.exports.WG_HOST = process.env.WG_HOST;
module.exports.WG_PORT = process.env.WG_PORT || '51820';
module.exports.WG_MTU = process.env.WG_MTU || null;
module.exports.WG_PERSISTENT_KEEPALIVE = process.env.WG_PERSISTENT_KEEPALIVE || '0';
module.exports.WG_DEFAULT_ADDRESS = process.env.WG_DEFAULT_ADDRESS || '10.8.0.x';
module.exports.WG_DEFAULT_DNS = typeof process.env.WG_DEFAULT_DNS === 'string'
  ? process.env.WG_DEFAULT_DNS
  : '1.1.1.1';
module.exports.WG_ALLOWED_IPS = process.env.WG_ALLOWED_IPS || '0.0.0.0/0, ::/0';

module.exports.WG_PRE_UP = process.env.WG_PRE_UP || '';
module.exports.WG_POST_UP = process.env.WG_POST_UP || '';
module.exports.WG_PRE_DOWN = process.env.WG_PRE_DOWN || '';
module.exports.WG_POST_DOWN = process.env.WG_POST_DOWN || '';
module.exports.LANG = process.env.LANGUAGE || 'en';
module.exports.UI_TRAFFIC_STATS = process.env.UI_TRAFFIC_STATS || 'false';
module.exports.UI_CHART_TYPE = process.env.UI_CHART_TYPE || 0;

const IMITATE_ALLOWED = ['none', 'quic', 'dns', 'stun', 'sip'];
const imitateProtocol = (process.env.IMITATE_PROTOCOL || 'none').toLowerCase();
if (!IMITATE_ALLOWED.includes(imitateProtocol)) {
  throw new Error(
    `IMITATE_PROTOCOL must be one of ${IMITATE_ALLOWED.join(', ')} (got: ${process.env.IMITATE_PROTOCOL})`,
  );
}
module.exports.IMITATE_PROTOCOL = imitateProtocol;

const getRandomInt = (min, max) => min + Math.floor(Math.random() * (max - min));
const getRandomJunkSize = () => getRandomInt(15, 150);

// Generate a random H range within a given quadrant [qMin, qMax]
const getRandomHRangeIn = (qMin, qMax) => {
  const span = qMax - qMin;
  const rangeSize = getRandomInt(Math.floor(span * 0.3), Math.floor(span * 0.8));
  const min = getRandomInt(qMin, qMax - rangeSize);
  return { min, max: min + rangeSize };
};

// Parse H param from env: "100-500" → {min,max}, "12345" → {min:12345,max:12345}, undefined → fallback
const parseHParam = (envValue, fallback) => {
  if (!envValue) return fallback;
  const str = String(envValue);
  const dashIdx = str.indexOf('-');
  if (dashIdx > 0) {
    return { min: parseInt(str.slice(0, dashIdx), 10), max: parseInt(str.slice(dashIdx + 1), 10) };
  }
  const val = parseInt(str, 10);
  return { min: val, max: val };
};

// Divide [5, 2^31] into 4 non-overlapping quadrants for H1-H4
const H_SPACE_MIN = 5;
const H_SPACE_MAX = 2_147_483_647;
const H_QUADRANT_SIZE = Math.floor((H_SPACE_MAX - H_SPACE_MIN) / 4);

module.exports.JC = process.env.JC || getRandomInt(3, 10);
module.exports.JMIN = process.env.JMIN || 50;
module.exports.JMAX = process.env.JMAX || 1000;
module.exports.S1 = process.env.S1 || getRandomJunkSize();
module.exports.S2 = process.env.S2 || getRandomJunkSize();
module.exports.S3 = process.env.S3 || getRandomInt(10, 64);
module.exports.S4 = process.env.S4 || getRandomInt(4, 20);
module.exports.H1 = parseHParam(process.env.H1, getRandomHRangeIn(H_SPACE_MIN, H_SPACE_MIN + H_QUADRANT_SIZE));
module.exports.H2 = parseHParam(process.env.H2, getRandomHRangeIn(H_SPACE_MIN + H_QUADRANT_SIZE, H_SPACE_MIN + 2 * H_QUADRANT_SIZE));
module.exports.H3 = parseHParam(process.env.H3, getRandomHRangeIn(H_SPACE_MIN + 2 * H_QUADRANT_SIZE, H_SPACE_MIN + 3 * H_QUADRANT_SIZE));
module.exports.H4 = parseHParam(process.env.H4, getRandomHRangeIn(H_SPACE_MIN + 3 * H_QUADRANT_SIZE, H_SPACE_MAX));

// CPS signatures (client-only, AWG 2.0)
module.exports.I1 = process.env.I1 || null;
module.exports.I2 = process.env.I2 || null;
module.exports.I3 = process.env.I3 || null;
module.exports.I4 = process.env.I4 || null;
module.exports.I5 = process.env.I5 || null;
