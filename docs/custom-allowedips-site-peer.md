# Feature note: custom AllowedIPs / site-relay peers

Status: **idea / scoped, not built.** Seeded 2026-06-22 from a real
entry→exit relay deployment (triple-awg-xray RU-exit migration). Pick up here when
adding the feature.

## Problem

awg-easy renders every client peer with a **hardcoded** AllowedIPs:

```js
// src/lib/configRender.js:64
}AllowedIPs = ${client.address}/32`;
```

So a peer can only ever own its single `/32` in the server's own subnet. There is
no way, via the web UI or the data model, to give a peer:

- a **foreign-subnet** address (outside `server.defaultAddress/24`), or
- **custom AllowedIPs** (e.g. `0.0.0.0/0`, or a whole subnet a peer should carry).

This forces anyone running a **relay / site-to-site** topology (an entry node that
tunnels to this box as an exit, a peer that routes a LAN, etc.) to bypass awg-easy
entirely with a separate `awg set` systemd unit (`awg-peers.service`) **plus** a
hand-written `iptables MASQUERADE` and `ip route` — none of which awg-easy knows
about or manages from the UI. That's the exact workaround the triple-awg-xray
RU-exit migration needed (see the wiki's "convert an exit to awg-easy" notes).

## The good news: route is free

awg-easy brings the tunnel up with **`wg-quick`** (`WireGuard.js:123` `wg-quick up
wg0`, `:192` `wg syncconf wg0 <(wg-quick strip wg0)`), and the rendered
`wg0.conf` has **no `Table = off`** → `wg-quick` runs on `Table = auto`. So **any
AllowedIPs written into `wg0.conf` get their kernel route added automatically.**
A custom-AllowedIPs field therefore needs *zero* route-management code — the reason
the manual `ip route replace 10.20.0.0/24 dev wg0` was needed is only that
`awg set` (the awg-peers workaround) bypasses `wg-quick`.

## Scope

1. **Data model + UI:** add an optional `allowedIps` (string, comma-separated CIDRs)
   per client in `wg0.json`, editable in the web UI. There's already a vestigial
   `allowedIps` var in `WireGuard.js:323` (currently `// eslint-disable-line
   no-unused-vars`) — a natural starting point.
2. **Config render:** at `configRender.js:64`, render the override when present,
   else fall back to `${client.address}/32`. (Keep the `/32` default — it's correct
   for normal clients.)
3. **Route:** nothing to do — `wg-quick`/`Table=auto` handles it (see above).
4. **MASQUERADE — the one real piece:** the PostUp hardcodes
   `-s ${server.defaultAddress}/24` (`configRender.js` `defaultPostUp`). A peer
   sourced *outside* that `/24` won't be masqueraded. Options:
   - extend PostUp to also masq each custom-AllowedIPs peer's source subnet, or
   - a per-client "masquerade this peer's traffic" toggle, or
   - simplest: masq the union of all peers' AllowedIPs source ranges.
5. **Validation (cryptokey-routing safety):** AllowedIPs must be **unique /
   non-overlapping** across peers. Overlapping AllowedIPs silently hand all return
   traffic to the last-configured peer (this bit the RU-exit migration: three entry
   peers all claiming `10.20.0.0/24` → only one worked). Reject/​warn on overlap in
   the UI.

## Acceptance

A peer can be given custom AllowedIPs in the web UI → written to `wg0.conf` →
`wg-quick` adds the route → PostUp masquerades it → relay/site traffic works
**without** `awg-peers.service` or any hand-written iptables/ip-route. Editing it in
the UI re-applies all of the above.

## Out of scope / notes

- This is distinct from normal-client management. Gate it behind an "advanced /
  site peer" flag so the simple-client UX stays unchanged.
- For the specific entry→exit relay case, note that aligning the entry into
  awg-easy's own subnet (a normal `/32` client) already works today with zero code —
  custom AllowedIPs is for genuine foreign-subnet / subnet-carrying peers.
