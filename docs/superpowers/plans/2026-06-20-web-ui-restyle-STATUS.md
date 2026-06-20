# Web UI Restyle — Status / Resume Note

**Branch:** `feat/ui-restyle` (off `master`)
**Last updated:** 2026-06-20
**State:** Planned. **Execution NOT started.**

## What's done
- Brainstorm → spec → plan complete and committed:
  - `86190cd` — design brief (`docs/superpowers/specs/web-ui-restyle-design-brief.md`)
  - `f665865` — design spec + vendored design package
    (`docs/superpowers/specs/2026-06-20-web-ui-restyle-design.md` +
    `docs/superpowers/specs/web-ui-restyle/`: `tokens.css`, `SPEC.md`,
    `visual-board.dc.html`, `assets/signal-mark*.svg`, `fonts/`)
  - `7d65c14` — implementation plan (`docs/superpowers/plans/2026-06-20-web-ui-restyle.md`)
- No application code changed yet: `src/www/index.html` still has the old
  Plus-Jakarta / `--accent` theme.

## Chosen execution mode
**Subagent-driven-development, with visual reviews BATCHED to the user at checkpoints.**
There is no automated UI test harness, so each task's machine-checkable gate is
`cd src && npm run lint` + `npm run buildcss`; the light/dark visual check against
`docs/superpowers/specs/web-ui-restyle/visual-board.dc.html` is reviewed by a human
(subagents can't see rendered UI).

## How to resume
1. `git switch feat/ui-restyle`
2. Invoke `superpowers:subagent-driven-development`.
3. Execute `docs/superpowers/plans/2026-06-20-web-ui-restyle.md` from **Task 1**, one
   subagent per task, lint+buildcss gate per task, batch the visual checks for the user.

Dev server for visual checks: `cd src && npm run serve` (no password) or
`npm run serve-with-password` (to see the login screen).

## Scope reminder
Visual language only — no IA/behavior changes. **Deferred:** per-client obfuscation
editor (+backend), new screens, `awg://v1` share-string (#3), dual export (#4). AMNEZIA
badge + obfuscation chips are read-only.
