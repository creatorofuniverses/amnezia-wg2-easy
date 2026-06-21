# Bug: changing MTU in the web-UI doesn't apply to the server interface

Status: **confirmed in code, not fixed.** Found 2026-06-22 — set MTU in the web-UI,
the **clients** got the new MTU but the **server's own wg0 interface kept the old
one**, producing a server↔client MTU mismatch (the triple-awg-xray RU-exit ended up
at 1420 while the entries were 1280; big TLS handshakes — e.g. gosuslugi.ru — wedged
while small responses worked).

## Root cause (confirmed)

`mtu` is **missing from `RESTART_FIELDS`**:

```js
// src/lib/serverSettings.js:99
const RESTART_FIELDS = ['port', 'jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4', 'h1', 'h2', 'h3', 'h4'];
// :109 classify() -> needsRestart: changed.some(k => RESTART_FIELDS.includes(k))
```

So an MTU change yields `needsRestart: false` and is applied via
`wg syncconf wg0 <(wg-quick strip wg0)` (`WireGuard.js:192`). **`wg syncconf` only
syncs peers and a few device attrs — it does NOT change the interface MTU.** The
server `[Interface] MTU` *is* rendered into `wg0.conf` when `server.mtu` is set
(`configRender.js:75`), but since the change is never applied with a full
`wg-quick down/up` (or an `ip link set`), the **live** wg0 keeps its boot-time MTU.
Meanwhile every generated **client** config carries the new MTU immediately → the
two disagree.

## Fix

Pick one:

- **Simple:** add `mtu` to `RESTART_FIELDS` (likely `address` too — same class:
  interface-level, not syncconf-able) so an MTU change triggers a full
  `wg-quick down/up`. Cost: a brief tunnel bounce on MTU change.
- **No-bounce:** on an MTU-only change, additionally run
  `ip link set dev wg0 mtu <n>` directly (live, no client drop), in addition to the
  `syncconf`. Keeps the tunnel up.

Either way the live server MTU must end up matching what clients are handed.

## Acceptance

After changing MTU in the UI, `ip link show wg0` on the host reflects the new MTU
(server and clients match). Add a test around `classify()` asserting `mtu` (and
`address`) force `needsRestart`.

## Refs

- `src/lib/serverSettings.js:99` `RESTART_FIELDS`, `:109` `classify`
- `src/lib/WireGuard.js:192` `wg syncconf` (the apply path that skips interface MTU)
- `src/lib/configRender.js:75` server `MTU` render (works; just isn't applied live)
