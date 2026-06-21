'use strict';

// Shape a client config for a "legacy" (non-imitation) AmneziaWG 2.0 client:
// drop the `ImitateProtocol` line and any `I1`-`I5` line whose value contains an
// angle-bracket imitation tag (e.g. `<qinit www.google.com>`). Raw-string
// I-params (no `<`) and every other line are kept verbatim.
function stripImitationKeys(confText) {
  return confText
    .split('\n')
    .filter((line) => {
      const m = line.match(/^\s*(ImitateProtocol|I[1-5])\s*=\s*(.*)$/);
      if (!m) return true; // not an imitation-related line — keep
      if (m[1] === 'ImitateProtocol') return false; // always drop
      return !m[2].includes('<'); // drop the I-param only if it has a tag
    })
    .join('\n');
}

module.exports = { stripImitationKeys };
