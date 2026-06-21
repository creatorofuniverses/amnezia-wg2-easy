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
  existing clients keep their addresses.
- `i1–i5` are already emitted to **every** client config today (from env
  constants), so they are de-facto server-wide already — making them editable is
  consistent, not new behavior.

## 2. Migration

In `getConfig()`'s load block (alongside the existing `s3/s4` backfill): for each
new field, `if (config.server.<field> === undefined) config.server.<field> =
<env-or-default>`. Existing deployments adopt their current env values as the
persisted starting point — **zero behavior change** until someone edits in the UI.

## 3. Generation reads from `config.server`, not env

`__saveConfig()` and `getClientConfiguration()` switch their direct env-constant
reads (`WG_HOST`, `WG_PORT`, `WG_MTU`, `WG_DEFAULT_DNS`, `WG_ALLOWED_IPS`,
`WG_PERSISTENT_KEEPALIVE`, `I1–I5`) to `config.server.*`. The `config.js` env
constants remain — **only as the seed source** for first boot / migration.

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

```
1. config = await getConfig()
2. errors = validateServerSettings(patch)
     if errors → throw HTTP 400 { errors }            // nothing written
3. prev = snapshot(config.server)                       // for rollback
4. diff = classify(prev, patch)                         // pure → {changed[], needsRestart, mustReimport}
5. Object.assign(config.server, patch)
6. await __saveConfig(config)                           // writes wg0.json + wg0.conf
7. if diff.needsRestart:
     try   { wg-quick down wg0 ; wg-quick up wg0 }
     catch { restore prev → __saveConfig → wg-quick up ; throw HTTP 500 }
8. return { settings, restarted: diff.needsRestart, mustReimport: diff.mustReimport }
```

### Classification (`classify(prev, next)` — pure function)

- **`needsRestart`** = any of `{ port, jc, jmin, jmax, s1, s2, s3, s4,
  h1, h2, h3, h4 }` changed → `wg-quick down/up`.
  - `port` rebinds `ListenPort`; obfuscation params are AWG interface-level and
    are only safely re-applied via a full interface bounce (there is no existing
    runtime-change path for them — first boot already does down/up).
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
  Confirm-gated in the UI (reuses the destructive Delete pattern).

## 6. Validation — `lib/serverSettings.js` (backend-authoritative)

Pure `validateServerSettings(patch) → { <field>: message }` (empty object = valid).
The frontend mirrors these rules for inline UX, but the backend is the gate.

| Field | Rule |
|---|---|
| `host` | non-empty hostname or IP |
| `port` | integer 1–65535 |
| `mtu` | `null`/empty, or integer 576–1500 |
| `dns` | comma-separated list of valid IPs (reuse `Util` IP validation) |
| `defaultAddress` | valid `x`-template (e.g. `10.8.0.x`) |
| `allowedIPs` | comma-separated list of valid CIDRs |
| `persistentKeepalive` | integer ≥ 0 |
| `jc`,`jmin`,`jmax` | integers, `jmin ≤ jmax` |
| `s1–s4` | integers in their respective valid ranges |
| `h1–h4` | `{min,max}` integers, `min ≤ max`, within the H space |
| `i1–i5` | string or `null` (lenient) |

Invalid patch → HTTP 400 `{ errors }`, **no write**.

## 7. Error handling

- **Rollback on apply failure** (step 7): if `wg-quick up` fails with the new
  config, restore the snapshotted `config.server`, rewrite, bring the interface
  back up, and return 500 — a bad value must never strand the server offline.
- **Validation failure** → 400 before any write.
- **Concurrency**: settings saves are rare and admin-only; rely on the existing
  cached-promise `getConfig()` and sequential `await`s. No new locking.

## 8. Testing

Existing `node:test` pattern in `src/lib/__tests__/` (cf.
`stripImitationKeys.test.js`, `awgShareString.test.js`). Pure units only; the
`wg-quick`/`wg syncconf` shell side stays manual per CLAUDE.md.

- `validateServerSettings` — valid/invalid per field, boundary values.
- `classify(prev, next)` — correct `needsRestart` / `mustReimport` flags for each
  field and combinations.
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
- `src/lib/serverSettings.js` — new: `validateServerSettings()`, `classify()`
  (pure, unit-tested).
- `src/lib/Server.js` — three new routes.
- `src/config.js` — unchanged role (still the env seed source); no new exports
  required, but the H-space constants may move/share with `serverSettings.js`.
- `src/lib/__tests__/serverSettings.test.js` — new test suite.
