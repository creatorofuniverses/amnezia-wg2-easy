# Round 1: MTU-apply fix + legacy-I1 default — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement task-by-task. Steps use checkbox (`- [ ]`) syntax.

Parent roadmap: `docs/superpowers/plans/2026-06-23-fork-notes-roadmap.md`
Source notes: `docs/server-mtu-not-applied.md` (#1), this roadmap (#2).

**Goal:** (1) make a web-UI MTU change actually apply to the live server `wg0`
interface, not just to client configs; (2) add an `I1_COMPAT` env flag that seeds
a baked-in legacy `I1` CPS value when no explicit `I1` is set, so the operator
stops pasting it by hand.

**Tech Stack:** Node 18+ (CommonJS, `node:test`), H3. No new dependencies.

## Decisions (locked)
- **I1 flag name:** `I1_COMPAT` (truthy env → enable).
- **Precedence:** an explicit `I1` always wins; `I1_COMPAT` only fills when `I1`
  is empty. Resolved in `config.js` so the rest of the app sees a single effective
  `I1`.
- **MTU fix:** classification approach (add `mtu` to `RESTART_FIELDS`) so the
  existing `wg-quick down/up` path re-applies the interface MTU. Brief tunnel
  bounce on MTU change is accepted (no separate live-`ip link` path in Round 1).
- Default off: `I1_COMPAT` unset → `I1` stays `null`; no behavior change for
  existing deploys.

## ⚠️ Blocking input
- **`LEGACY_I1_VALUE`** — the exact legacy `I1` string the operator keeps pasting
  (e.g. `<b 0x...>` / `<qinit ...>`). Must be filled into `config.js` before
  Task 2 ships. Tracked as a placeholder constant until provided.

## Global Constraints
- Server `wg0.conf`/runtime: MTU change must end with `ip link show wg0`
  reflecting the new MTU (server == clients).
- `I1_COMPAT` changes only the **seeded** server config (`I1`), exactly like an
  explicit `I1` env today — same code path (`WireGuard.js:114` →
  `serverSettings.seedServerDefaults`). No new render/strip surface; the existing
  per-client legacy strip (`stripImitationKeys`) already removes tagged `I1`.
- No new dependencies. `npm run lint` stays clean.
- Tests: `node:test`, scoped to pure/classify logic per app convention.

## File Structure
- **Modify:** `src/lib/serverSettings.js` — add `'mtu'` (and `'address'` if it is a
  settable key — verify) to `RESTART_FIELDS` (`:99`).
- **Modify:** `src/lib/config.js` — add `I1_COMPAT` parse + `LEGACY_I1_VALUE`
  constant; make `module.exports.I1 = process.env.I1 || (I1_COMPAT ? LEGACY_I1_VALUE : null)` (`:80`).
- **Modify (maybe):** `src/lib/WireGuard.js` — none expected; it already seeds `i1: I1`
  (`:114`). Confirm no destructure change needed (I1_COMPAT need not be imported there).
- **Create:** `src/lib/__tests__/serverSettings.classify.test.js` (or extend the
  existing `serverSettings.test.js`) — assert `mtu` change ⇒ `needsRestart: true`.
- **Modify:** `CLAUDE.md` + README/env table — document `I1_COMPAT`.

## Verification commands
```bash
cd src && node --test       # classify MTU test
cd src && npm run lint      # ESLint clean
cd src && npm run serve     # manual: change MTU in UI, then `ip link show wg0`
```

---

### Task 1: MTU change forces restart (the #1 bug fix)

**Files:**
- Modify: `src/lib/serverSettings.js:99`
- Create/extend: `src/lib/__tests__/serverSettings.test.js`

**Interfaces:** `classify(prev, next)` must return `needsRestart: true` when `mtu` differs.

- [ ] **Step 1: Failing test.** Add a case: `classify({mtu: 1420}, {mtu: 1280})` ⇒
  `needsRestart === true`. (Run; it fails today because `mtu ∉ RESTART_FIELDS`.)
- [ ] **Step 2: Fix.** Add `'mtu'` to `RESTART_FIELDS` (`serverSettings.js:99`).
  Verify whether the server's own interface `address` is a settable field that
  flows through `classify`; if so, add `'address'` too (same interface-level class)
  with its own test case. If `address` is not settings-editable, note that and skip.
- [ ] **Step 3: Confirm** `REIMPORT_FIELDS = ['host', ...RESTART_FIELDS]` — adding
  `mtu` also marks it reimport-affecting. Check that's desired (MTU is in the
  client template / share-string reimport set). If not, decouple instead of
  spreading. Decide and note.
- [ ] **Step 4: Manual** — `npm run serve`, change MTU in UI, observe
  `wg-quick down/up`, then `ip link show wg0` shows the new MTU. Acceptance met.
- [ ] **Step 5: lint + test green.**

### Task 2: `I1_COMPAT` env flag → seeded legacy `I1` (the #2 feature)

**Files:**
- Modify: `src/lib/config.js:80`
- Modify: `CLAUDE.md` env table + README

**Interfaces:** `require('../config').I1` resolves to the effective value
(explicit env > compat default > null). Downstream seed at `WireGuard.js:114`
is unchanged.

- [ ] **Step 1: Fill `LEGACY_I1_VALUE`** with the operator's real string (blocking
  input above). Until provided, use a clearly-marked placeholder + `TODO`.
- [ ] **Step 2: config.js.** Add
  `const I1_COMPAT = /^(1|true|yes)$/i.test(process.env.I1_COMPAT || '');`
  and change `module.exports.I1 = process.env.I1 || (I1_COMPAT ? LEGACY_I1_VALUE : null);`.
  Leave `I2`–`I5` untouched.
- [ ] **Step 3: Precedence check.** Confirm explicit `I1` env still wins (string
  truthiness short-circuits before the compat branch). With both set, `I1` env value is used.
- [ ] **Step 4: Persisted-config check.** `seedServerDefaults` only fills when
  `server.i1 === undefined`, so an existing `wg0.json` with an `i1` (incl. `null`
  from a prior boot) is NOT overwritten — document this: `I1_COMPAT` affects
  **fresh** server config seeding, matching how `I1` env behaves today. (If we want
  it to retrofit existing deploys, that's a separate decision — out of scope.)
- [ ] **Step 5: Docs.** Add `I1_COMPAT` row to CLAUDE.md env table + README,
  noting it's overridden by an explicit `I1` and only seeds new configs.
- [ ] **Step 6: lint clean.**

### Task 3: Wrap-up
- [ ] Update parent roadmap: mark Round 1 done, fill the resolved `LEGACY_I1_VALUE`.
- [ ] Update `docs/server-mtu-not-applied.md` status → fixed (commit ref).
- [ ] Single commit or two (`fix(server-settings): apply MTU to live interface`,
  `feat(config): I1_COMPAT legacy CPS default`).

## Out of scope (Round 1)
- No-bounce live `ip link set mtu` path (deferred; bounce accepted).
- Retrofitting `I1_COMPAT` onto already-seeded `wg0.json`.
- Custom AllowedIPs (#3, Round 2); responder hardening (#4, Round 3).
