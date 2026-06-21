# Review — Server Settings backend design

Reviewer: Claude (Opus 4.8), 2026-06-21. Verified every "how the code works today" claim
against the actual source (`src/lib/WireGuard.js`, `src/config.js`, `src/lib/Server.js`,
`src/lib/Util.js`, `src/lib/__tests__/`). Verdict below, then findings ordered by severity.

## Verdict

**Solid, ship-able design — but not mergeable as written.** The architecture is right:
seed-from-env-then-`config.server`-authoritative, on-demand client generation means most
edits are zero-disruption, and the `classify()` / `validateServerSettings()` pure-function
split is exactly the testable seam this codebase rewards. The spec's reading of the existing
code is ~90% accurate.

There is **one correctness bug** in the apply pipeline (iptables teardown ordering, §4) and
**two gaps** that will bite on first real edit (a `defaultAddress` subnet change strands new
clients; the validators the spec says to "reuse" don't exist). Fix those three and this is
good to implement. The rest are accuracy nits.

---

## Blocking

### B1. Apply pipeline writes the new conf *before* `wg-quick down` → stale iptables rules leak

§4 step 6 does `__saveConfig` (writes the new `wg0.conf`), then step 7 does
`wg-quick down wg0 ; wg-quick up wg0`. **`wg-quick down` reads the on-disk conf to know
which `PostDown` rules to remove.** By step 7 that file already contains the *new* port /
subnet, so:

- On a **port** change: `down` runs `iptables -D … --dport <NEW> -j ACCEPT` (a rule that was
  never added → command errors), while the **old** `--dport <OLD> ACCEPT` rule that the live
  interface actually installed is never torn down and **leaks permanently**. Each port edit
  adds another stale ACCEPT.
- On a **`defaultAddress`** change: same problem with the `MASQUERADE -s <subnet>/24` rule.

This is real because §3 itself makes `PostUp`/`PostDown` render dynamically from
`config.server.port` / `config.server.defaultAddress` — which is the right call, but it's
exactly what makes write-then-down unsafe. Confirmed: `WG_POST_UP`/`WG_POST_DOWN` in
`config.js:23-36` are `--dport ${WG_PORT}` / `-s ${WG_DEFAULT_ADDRESS…}/24` strings, and
`__saveConfig` emits `PreUp/PostUp/PreDown/PostDown` verbatim into `wg0.conf`
(`WireGuard.js:141-144`).

**Fix:** bring the interface down on the *current* config first, then write, then up:
```
snapshot prev
if needsRestart: wg-quick down wg0        // tears down with the rules that are actually live
__saveConfig(new)                          // now write
if needsRestart: wg-quick up wg0           // installs new rules
  catch → restore prev, __saveConfig, wg-quick up, throw 500
```
Trade-off: a few hundred ms where the conf on disk lags the live interface. Acceptable for an
admin-only, rare write, and strictly safer than leaking firewall state.

### B2. Changing `defaultAddress` to a different subnet strands new clients (server `address` is frozen)

The spec (correctly) keeps the server's own `address` read-only (§1 note, §"Out of scope"),
and classifies `defaultAddress` as **client-config-only, "zero client disruption."** That's
true only for a change to the *host-octet template within the same /24*. If an admin edits
`10.8.0.x` → `10.9.0.x`:

- New clients get `10.9.0.N`, but the server interface is still `10.8.0.1`
  (`address` from `WireGuard.js:84`, frozen). New clients are now **on a different subnet
  than the server** → they can't reach it. Not "zero disruption" — fully broken for every
  new client.
- The rendered `MASQUERADE -s 10.9.0.0/24` (from §3) no longer matches the server's actual
  `10.8.0.0/24` interface → NAT for new clients fails too.

**Fix (pick one):**
- Constrain `validateServerSettings`: `defaultAddress` must be in the same /24 as
  `config.server.address` (only the host-octet template may change). Simplest, matches the
  "address is read-only" stance.
- Or explicitly make subnet-base part of the read-only address and only expose the template
  octet in the UI.

Either way the spec should state this — right now §1's "only governs new client IP
assignment; existing clients keep their addresses" undersells the hazard.

### B3. The validators the spec says to "reuse" don't exist for the relevant types

§6 says `dns` → "reuse `Util` IP validation" and `allowedIPs` → "valid CIDRs." Reality:
`Util` exposes **only `isValidIPv4`** (`Util.js:7-18`) — no IPv6, no CIDR validator.
But the shipped defaults are `dns: 1.1.1.1` (fine) and **`allowedIPs: 0.0.0.0/0, ::/0`**
(`config.js:20`) — IPv6 + CIDR. A validator built on `isValidIPv4` alone would **reject the
default value**, locking admins out of saving.

**Fix:** scope new helpers (`isValidCIDR`, IPv6-aware IP check) in `serverSettings.js` (or
extend `Util`) and list them in §"Files touched." Don't describe this as "reuse existing."

---

## Accuracy corrections (not blocking, but the spec misreads the code)

### A1. §3 misattributes which function reads which env constant

The spec lists `WG_HOST, WG_PORT, WG_MTU, WG_DEFAULT_DNS, WG_ALLOWED_IPS,
WG_PERSISTENT_KEEPALIVE, I1–I5` as `__saveConfig()` reads. Actually:
- `__saveConfig()` reads **only `WG_PORT`** (+ `WG_PRE_UP/POST_UP/PRE_DOWN/POST_DOWN`,
  `IMITATE_PROTOCOL`) — `WireGuard.js:140-156`.
- `getClientConfiguration()` reads `WG_DEFAULT_DNS, WG_MTU, I1–I5, WG_ALLOWED_IPS,
  WG_PERSISTENT_KEEPALIVE, WG_HOST, WG_PORT` — `WireGuard.js:259-284`.

The net change-set ("switch these reads to `config.server.*`") is still correct, but get the
call sites right or the implementer will hunt for `WG_MTU` in the wrong function.

### A2. "There is no existing runtime-change path for [obfuscation params]" is wrong

§4 justifies `needsRestart` via down/up by saying no runtime path exists. There **is** one:
`__syncConfig()` → `wg syncconf wg0 <(wg-quick strip wg0)` (`WireGuard.js:182`), run after
every client add/edit today. The *conclusion* (use down/up for `port` + obfuscation) is still
defensible — AWG interface-level S/H/J params are set at device creation and `syncconf`
won't re-apply them — but say *that*, not "no path exists." For **`port` alone**, `syncconf`
can rebind `ListenPort` without an interface bounce; down/up is chosen only because the
firewall port rule also has to change. Worth a sentence so the reasoning is honest.

### A3. "i1–i5 emitted to *every* client config" — legacy clients are the exception

§1's note says i1–i5 are "already emitted to every client config … de-facto server-wide."
Nearly true, but `getClientConfiguration` ends with
`return client.legacy ? stripImitationKeys(conf) : conf` (`WireGuard.js:285`), and
`stripImitationKeys` drops `ImitateProtocol` and any `I*` line containing a `<tag>`
(`stripImitationKeys.js:7-16`). So **legacy clients deliberately don't get tagged i-params.**
Making i1–i5 editable is still consistent — just note that legacy stripping still applies, so
the "server-wide" value isn't literally universal.

### A4. §8 cites CLAUDE.md as "no test suite"; tests do exist

Minor but worth aligning: CLAUDE.md says "The Node app has no test suite — quality relies on
ESLint and manual testing," yet `src/lib/__tests__/` has `node:test` suites and
`package.json` defines `"test": "node --test"`. The spec's plan to add
`serverSettings.test.js` in that dir is correct and good; the "stays manual per CLAUDE.md"
note for the shell side is a fair reading. Consider updating CLAUDE.md's stale sentence as
part of this work.

### A5. §5 "reuses the destructive Delete pattern" — that pattern is UI-side only

The backend `DELETE /api/wireguard/client/:clientId` has **no confirm gate**
(`Server.js:177-181`); confirmation is client-side. Fine to mirror, just don't imply the
backend has a reusable confirm primitive — `regenerate-keypair` will need its own UI
confirm, and the route should be a plain authenticated POST.

---

## Things the spec got right (verified)

- `getConfig()` **does** have the `s3/s4` backfill (`WireGuard.js:68-69`) and an h1–h4
  number→`{min,max}` migration (`62-67`) — §2's "alongside the existing s3/s4 backfill" lands
  exactly. h1–h4 are stored as `{min,max}` as the spec assumes.
- Client configs are genuinely generated on demand, never stored as files — so client-facing
  edits need no filesystem rewrite (§"Background" confirmed: `getClientConfiguration`,
  three delivery routes all regenerate per request).
- Default iptables rules **are** pre-baked in `config.js` from `WG_PORT`/`WG_DEFAULT_ADDRESS`
  (`config.js:23-36`) — the §3 "integration gotcha" is a real, well-spotted issue (the fix
  has the ordering bug B1, but identifying the staleness is correct).
- H3 idioms (`defineEventHandler`, `readBody`, `getRouterParam` + `__proto__/constructor/
  prototype` guards), `PASSWORD` plaintext compare at `Server.js:100`, and the
  privateKey-never-returned posture all match. §"Web panel out of scope" reasoning is
  accurate.
- The two-outcome model (save-only vs save+restart) is the right abstraction given on-demand
  generation.

---

## Smaller notes

- **`mtu` 576–1500:** fine, but AWG-over-UDP frequently wants ≤1420; not a bug, just confirm
  you don't want a lower ceiling. 576 floor is correct for IPv4.
- **`port` is both `needsRestart` *and* `mustReimport`** — the spec has this right
  (Endpoint moves); just make sure the UI surfaces both flags, not one.
- **Concurrency (§7):** relying on the cached `getConfig()` promise + sequential awaits is
  fine for admin-rare writes, but two overlapping saves still read-modify-write the same
  object. Acceptable; maybe one sentence acknowledging the last-writer-wins window.
- **Regenerate-keypair restart** inherits B1's ordering fix (it bounces the interface too).
- **`classify` test matrix (§8):** add a case asserting a same-/24 `defaultAddress` template
  change is save-only while a subnet-base change is rejected (drives B2's validator).

---

## Bottom line

Architecturally sound and faithful to the codebase. Address **B1 (iptables ordering)**,
**B2 (defaultAddress subnet)**, and **B3 (missing validators)** before implementing; fold in
the A-series accuracy fixes so the implementer isn't misled. Everything else is ready.
