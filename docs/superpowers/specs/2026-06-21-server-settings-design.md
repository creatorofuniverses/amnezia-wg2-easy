# Server Settings — backend design

Adds a web-UI screen for editing the **server's own** configuration — the
WireGuard interface and the defaults new clients inherit — without touching the
running Docker container or its env vars. Backend design only; the visual
language lives in the design concept (`~/Documents/web-admin/SPEC-server-settings.md`,
section **04 · SERVER SETTINGS**). The design concept was authored without code
visibility, so where its backend assumptions diverge from the code (e.g. "every
save restarts the interface"), this spec is authoritative.

## Scope

**In scope** (editable in the UI, persisted, applied at runtime):

- **NETWORK** — public host, listen port, MTU
- **CLIENT DEFAULTS** — address range, persistent keepalive, DNS, AllowedIPs
- **OBFUSCATION DEFAULTS** — `jc/jmin/jmax`, `s1–s4`, `h1–h4`, `i1–i5`
- **SERVER KEYPAIR** — regenerate (rotate server private/public key)

**Out of scope / deferred:**

- **WEB PANEL** (admin password + session timeout) — requires moving auth off
  `process.env.PASSWORD` (today a plaintext compare in `Server.js`) onto a
  persisted, hashed settings store. Separable follow-up subsystem.
- **Per-client obfuscation override** — the design concept's "default ↔ override"
  model. This feature only makes the server-wide values editable; they keep
  applying to all non-legacy clients exactly as today.
- **Editing the server's own interface address / subnet** — changing the subnet
  base would strand existing clients. The server `address` stays read-only.
- `IMITATE_PROTOCOL` / `RESPONDER` — baked at container start; genuinely need a
  container restart. Correctly absent from the design concept too.

## Background: how config is read today

Config generation pulls from **two sources**:

- `config.server.*` (persisted in `wg0.json`, seeded from env on first boot):
  `privateKey`, `publicKey`, `address`, `jc`, `jmin`, `jmax`, `s1–s4`,
  `h1–h4` (`{min,max}`).
- `config.js` env constants (read once at boot, used directly in
  `WireGuard.js`): `WG_HOST`, `WG_PORT`, `WG_MTU`, `WG_DEFAULT_DNS`,
  `WG_DEFAULT_ADDRESS`, `WG_ALLOWED_IPS`, `WG_PERSISTENT_KEEPALIVE`, `I1–I5`.

Everything in the second bucket is what becomes editable. Client configs are
**not stored as files** — `getClientConfiguration()` generates them on demand —
so a change to a client-facing field needs no filesystem rewrite; the next
config download/QR reflects it automatically.

## 1. Data model — extend `config.server`

Add the following fields to `config.server` in `wg0.json`. Each is **seeded from
its env var on first boot / migration**, then becomes authoritative (UI edits
win; env is ignored thereafter).

| Field | Seed (env) | Default | Change class |
|---|---|---|---|
| `host` | `WG_HOST` | (required at first boot) | client-config-only |
| `port` | `WG_PORT` | `51820` | **restart** |
| `mtu` | `WG_MTU` | `null` | client-config-only |
| `dns` | `WG_DEFAULT_DNS` | `1.1.1.1` | client-config-only |
| `defaultAddress` | `WG_DEFAULT_ADDRESS` | `10.8.0.x` | client-config-only |
| `allowedIPs` | `WG_ALLOWED_IPS` | `0.0.0.0/0, ::/0` | client-config-only |
| `persistentKeepalive` | `WG_PERSISTENT_KEEPALIVE` | `0` | client-config-only |
| `i1`–`i5` | `I1`–`I5` | `null` | client-config-only |
| existing `jc,jmin,jmax,s1–s4,h1–h4` | — | — | **restart** |

Notes:

- The server's own `address` (e.g. `10.8.0.1`) stays **read-only / out of scope**.
- `defaultAddress` (the `x` template) only governs **new** client IP assignment;
  existing clients keep their addresses. **Constraint:** because the server
  `address` is frozen, `defaultAddress` must stay in the **same /24** as
  `config.server.address` — only the host-octet template (`x`) may change.
  Editing the subnet base (`10.8.0.x` → `10.9.0.x`) would put new clients on a
  different subnet than the `10.8.0.1` server and break the rendered
  `MASQUERADE -s …/24` rule. This is enforced in validation (§6), not merely
  "zero disruption."
- `i1–i5` are already emitted to client configs today (from env constants), so
  they are de-facto server-wide already — making them editable is consistent, not
  new behavior. **Exception:** `getClientConfiguration()` ends with
  `client.legacy ? stripImitationKeys(conf) : conf` (`WireGuard.js:285`), which
  strips `ImitateProtocol` and tagged `I*` lines — so legacy clients deliberately
  don't receive tagged i-params. The "server-wide" value isn't literally
  universal; legacy stripping still applies.

## 2. Migration

In `getConfig()`'s load block (alongside the existing `s3/s4` backfill): for each
new field, `if (config.server.<field> === undefined) config.server.<field> =
<env-or-default>`. Existing deployments adopt their current env values as the
persisted starting point — **zero behavior change** until someone edits in the UI.

## 3. Generation reads from `config.server`, not env

Two call sites switch their direct env-constant reads to `config.server.*` —
note which reads which, so the implementer looks in the right function:

- `__saveConfig()` (`WireGuard.js:140–156`) reads **only `WG_PORT`** (as
  `ListenPort`) among the editable set (plus `WG_PRE_UP/POST_UP/PRE_DOWN/POST_DOWN`
  and `IMITATE_PROTOCOL`, which are out of scope here).
- `getClientConfiguration()` (`WireGuard.js:259–284`) reads `WG_DEFAULT_DNS`,
  `WG_MTU`, `I1–I5`, `WG_ALLOWED_IPS`, `WG_PERSISTENT_KEEPALIVE`, `WG_HOST`,
  `WG_PORT`.

The `config.js` env constants remain — **only as the seed source** for first
boot / migration.

**Integration gotcha — iptables / port coupling:** the default
`WG_POST_UP` / `WG_PRE_DOWN` / `WG_POST_DOWN` rules are pre-baked in `config.js`
from env `WG_PORT` and `WG_DEFAULT_ADDRESS`. Once port/address-template are
editable, those strings go stale on edit. Fix: `__saveConfig()` renders the
**default** PreUp/PostUp/PreDown/PostDown from `config.server.port` /
`config.server.defaultAddress` at write time. A user-supplied `WG_PRE_UP` /
`WG_POST_UP` / `WG_PRE_DOWN` / `WG_POST_DOWN` env override is still honored
**verbatim** (only the auto-generated default rules pick up the dynamic values).

## 4. Apply pipeline — `WireGuard.updateServerSettings(patch)`

Client-config-only changes need no interface action (configs are generated on
demand), so application collapses to **two outcomes**: save-only, or
save + interface restart.

**Ordering is correctness-critical:** `wg-quick down` runs the `PostDown` rules
*from the on-disk conf at down-time*. Since §3 renders those rules dynamically
from `config.server.port` / `defaultAddress`, we must bring the interface **down
on the current conf first, then write, then up** — otherwise `down` tears down
with the *new* port/subnet (a rule never installed → error) while the live old
`--dport`/`MASQUERADE` rules leak permanently. (The existing first-boot sequence
at `WireGuard.js:103–105` writes-then-down, but it's benign there because there's
no live interface with a different port yet.)

```
1. config = await getConfig()
2. errors = validateServerSettings(patch)
     if errors → throw HTTP 400 { errors }            // nothing written
3. prev = snapshot(config.server)                       // for rollback
4. diff = classify(prev, patch)                         // pure → {changed[], needsRestart, mustReimport}
5. if diff.needsRestart: wg-quick down wg0              // DOWN on CURRENT conf (live rules)
6. Object.assign(config.server, patch)
7. await __saveConfig(config)                           // now write wg0.json + wg0.conf
8. if diff.needsRestart:
     try   { wg-quick up wg0 }                          // installs new rules
     catch { restore prev → __saveConfig → wg-quick up ; throw HTTP 500 }
9. return { settings, restarted: diff.needsRestart, mustReimport: diff.mustReimport }
```

Trade-off: a few hundred ms where the on-disk conf lags the (down) interface.
Acceptable for an admin-only, rare write, and strictly safer than leaking
firewall state. `regenerate-keypair` (§5) bounces the interface too and follows
the same down→write→up ordering.

### Classification (`classify(prev, next)` — pure function)

- **`needsRestart`** = any of `{ port, jc, jmin, jmax, s1, s2, s3, s4,
  h1, h2, h3, h4 }` changed → `wg-quick down/up`.
  - A runtime-sync path *does* exist (`__syncConfig()` →
    `wg syncconf wg0 <(wg-quick strip wg0)`, `WireGuard.js:182`, run after every
    client add/edit). We deliberately use down/up anyway: the AWG-specific S/H/J
    junk params are set at **device creation** and `syncconf` won't re-apply them;
    and although `port` alone could be rebound, its firewall `--dport` rule also
    has to change, which needs the down/up teardown. So down/up is the correct
    tool for both — not because no sync path exists.
- **Everything else** (`host, mtu, dns, defaultAddress, allowedIPs,
  persistentKeepalive, i1–i5`) → **save-only, zero client disruption**.
- **`mustReimport`** (existing tunnels break until clients re-download their
  config) = any of `{ host, port, obfuscation params, keypair }`:
  - `host`/`port` → the client `Endpoint` moved.
  - obfuscation/keypair → handshake fails against the new params/key.
  - The remaining client-facing fields (`dns, mtu, allowedIPs,
    persistentKeepalive, defaultAddress, i1–i5`) keep existing clients **working**
    with their old values; the new value applies only to newly-imported configs.

The UI restart-notice and post-save hint map directly onto the two returned
flags (`restarted`, `mustReimport`).

## 5. Routes (H3, in `Server.js`)

All under the existing `/api/*` session guard (admin-only when `PASSWORD` set).
Follow existing idioms: `defineEventHandler`, `readBody`, prototype-pollution
guards on params, the `WireGuard` singleton.

- `GET /api/server-settings` → the editable `config.server` fields.
  **Never returns `privateKey`**; `publicKey` is included.
- `POST /api/server-settings` → body = patch → `updateServerSettings(patch)` →
  `{ settings, restarted, mustReimport }`.
- `POST /api/server-settings/regenerate-keypair` → rotate `privateKey` /
  `publicKey`, restart the interface, return `{ publicKey, mustReimport: true }`.
  A **plain authenticated POST** — confirmation is **UI-side only** (the backend
  `DELETE /client/:id` has no confirm gate either, `Server.js:177–181`; there is
  no reusable backend confirm primitive to inherit).

## 6. Validation — `lib/serverSettings.js` (backend-authoritative)

Pure `validateServerSettings(patch, currentServer) → { <field>: message }`
(empty object = valid). `currentServer` is passed so cross-field rules (the
same-/24 `defaultAddress` constraint) can reference the frozen `address`. The
frontend mirrors these rules for inline UX, but the backend is the gate.

**New validation helpers are required** — `Util` exposes **only `isValidIPv4`**
(`Util.js:7–18`); there is no IPv6 or CIDR validator. The shipped default
`allowedIPs: 0.0.0.0/0, ::/0` is IPv6 + CIDR, so validating it with
`isValidIPv4` alone would reject the default and lock admins out of saving.
Add `isValidCIDR` and an IPv6-aware IP check (in `serverSettings.js`, or extend
`Util`); listed in §"Files touched". Do **not** describe these as "reuse existing."

| Field | Rule |
|---|---|
| `host` | non-empty hostname or IP |
| `port` | integer 1–65535 |
| `mtu` | `null`/empty, or integer 576–1500 |
| `dns` | comma-separated list of valid IPs (IPv4 **and** IPv6) |
| `defaultAddress` | valid `x`-template (e.g. `10.8.0.x`) **and same /24 as `currentServer.address`** (host-octet template only) |
| `allowedIPs` | comma-separated list of valid CIDRs (IPv4 **and** IPv6, incl. `0.0.0.0/0`, `::/0`) |
| `persistentKeepalive` | integer ≥ 0 |
| `jc`,`jmin`,`jmax` | integers, `jmin ≤ jmax` |
| `s1–s4` | integers in their respective valid ranges |
| `h1–h4` | `{min,max}` integers, `min ≤ max`, within the H space |
| `i1–i5` | string or `null` (lenient) |

Invalid patch → HTTP 400 `{ errors }`, **no write**.

## 7. Error handling

- **Rollback on apply failure** (step 8): if `wg-quick up` fails with the new
  config, restore the snapshotted `config.server`, rewrite, bring the interface
  back up, and return 500 — a bad value must never strand the server offline.
- **Validation failure** → 400 before any write.
- **Concurrency**: settings saves are rare and admin-only; rely on the existing
  cached-promise `getConfig()` and sequential `await`s. No new locking. Two
  overlapping saves still read-modify-write the same `config.server` object —
  accepted last-writer-wins window, not worth locking for an admin-rare action.

## 8. Testing

Existing `node:test` pattern in `src/lib/__tests__/` (cf.
`stripImitationKeys.test.js`, `awgShareString.test.js`; `package.json` already
defines `"test": "node --test"` — CLAUDE.md's "no test suite" line is stale and
should be corrected as part of this work). Pure units only; the
`wg-quick`/`wg syncconf` shell side stays manual.

- `validateServerSettings` — valid/invalid per field, boundary values; include
  IPv6/CIDR `allowedIPs` (incl. the default `0.0.0.0/0, ::/0`) **passing**, and a
  `defaultAddress` subnet-base change (`10.8.0.x` → `10.9.0.x`) **rejected** while
  a same-/24 host-octet change passes.
- `classify(prev, next)` — correct `needsRestart` / `mustReimport` flags for each
  field and combinations; assert a same-/24 `defaultAddress` change is save-only.
- Generation from `config.server` — fixture config → expected `wg0.conf` and
  client-config strings (proves env was fully replaced by `config.server`).
- Migration backfill — a `wg0.json` missing the new fields → `getConfig()` seeds
  them from env/defaults.

## Files touched

- `src/lib/WireGuard.js` — new `updateServerSettings()` and
  `regenerateKeypair()` (both call the pure `classify()`/`validateServerSettings()`
  from `serverSettings.js`); `getConfig()` migration backfill; `__saveConfig()` and
  `getClientConfiguration()` read `config.server.*` + render default
  PreUp/PostUp from `config.server.port`/`defaultAddress`.
- `src/lib/serverSettings.js` — new: `validateServerSettings(patch, current)`,
  `classify(prev, next)`, and new validation helpers `isValidCIDR` +
  IPv6-aware IP check (pure, unit-tested). May instead extend `Util` with the
  IP/CIDR helpers if preferred.
- `src/lib/Server.js` — three new routes.
- `src/config.js` — unchanged role (still the env seed source); no new exports
  required, but the H-space constants may move/share with `serverSettings.js`.
- `src/lib/__tests__/serverSettings.test.js` — new test suite.
- `CLAUDE.md` — correct the stale "no test suite" sentence (the Node app does
  have `node:test` suites).
