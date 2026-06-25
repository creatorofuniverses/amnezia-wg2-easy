# Feature note: custom AllowedIPs / site-relay peers

Status: **implemented 2026-06-24.** See the implementation plan at `docs/superpowers/plans/2026-06-23-custom-allowedips-site-peer.md` and the design spec at `docs/superpowers/specs/2026-06-23-custom-allowedips-site-peer-design.md`. The user-facing how-to is below; the original field report (problem / scope / rationale) follows it.

## Using it (web UI)

Every client row has an **Advanced / site peer** expander. Leave it closed for
normal clients — their UX is unchanged. To turn a client into a site / relay peer:

1. **AllowedIPs** — comma-separated CIDRs this peer should carry, e.g.
   `10.20.0.0/24` (a LAN/subnet behind the peer) or `0.0.0.0/0`. This **replaces**
   the peer's default `/32`, so if the peer still needs its own tunnel address,
   include it in the list. A peer with a non-empty AllowedIPs shows a **`site`** chip.
2. **Masquerade this peer's traffic** — source-NAT this peer's subnet out the
   host's WAN. Turn it on for an exit/relay so the carried subnet reaches the
   internet via the server's public IP. (Needs the default hooks — see below.)
3. **Save** — applies the change. The button stays grey until you actually change
   something, and shows **Saving…** while it applies.

### What happens on save

- The kernel **route** for each AllowedIPs CIDR is added automatically — the tunnel
  runs `wg-quick` on `Table = auto`, so no manual `ip route` is ever needed.
- With Masquerade on, the matching `iptables -t nat … -j MASQUERADE` rule is added;
  off, it's removed.
- **The whole tunnel bounces** (`wg-quick down`/`up`, ~2 s) so every client drops
  for a moment. This is by design for site-peer edits — normal client
  add/edit/disable/delete does **not** bounce the tunnel.
- **Clearing** the AllowedIPs field and saving reverts the peer to an ordinary
  `/32` client (route + masq removed, `site` chip gone).

### Rules / guardrails

- **No overlapping AllowedIPs.** Each CIDR must be unique across peers — overlapping
  ranges silently hand all return traffic to the last-configured peer. The UI
  rejects an overlapping AllowedIPs (with the real reason inline), rejects a normal
  client's address edit that would land inside a site subnet, and skips site subnets
  when auto-assigning a new client's address.
- **Masquerade needs the default hooks.** If `WG_POST_UP` / `WG_POST_DOWN` are set in
  the environment, the default `PostUp`/`PostDown` — and therefore all site-masq
  rules — are suppressed, and masquerade silently won't work.

### Typical use case — entry → exit relay

Run an entry node as a peer of this box carrying its client subnet (AllowedIPs
`10.20.0.0/24`, masquerade **on**). Devices behind the entry then egress to the
internet source-NAT'd to this server's public IP, and large TLS handshakes complete
normally. (If you only need to fold a *single* device into this server's own subnet,
a normal `/32` client already does that with zero extra config — site peers are for
genuine foreign-subnet / subnet-carrying peers.)

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
