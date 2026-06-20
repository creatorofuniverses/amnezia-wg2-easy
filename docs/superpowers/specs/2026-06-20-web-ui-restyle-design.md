# Web UI Restyle — Design Spec (Subsystem #2)

**Date:** 2026-06-20
**Branch:** `feat/ui-restyle`
**Status:** Design — pending user review, then `writing-plans`.

## Summary

Restyle the AmneziaWG Easy admin web UI to the **"Network Teal" (Material 3)** design
language. **Visual language only — no IA or behavior changes.** Keep the existing Vue 2
single-page + modal flow; apply the new tokens, fonts, logo, and card/field-row anatomy to
the screens and components we already have.

This wraps an external design pass whose output is vendored at
`docs/superpowers/specs/web-ui-restyle/`:
- `tokens.css` — full M3 token set, light (`:root`) + dark (`.dark`).
- `SPEC.md` — conventions, component table, 7 per-screen specs (the authoritative visual spec).
- `visual-board.dc.html` — annotated pixel reference for all screens (open in a browser).
- `assets/signal-mark.svg`, `assets/signal-mark-light.svg` — the Signal logo mark.
- `fonts/` — self-hosted Manrope (400/500/600/700) + JetBrains Mono (400/500).

`SPEC.md` + `visual-board.dc.html` are the source of truth for pixel detail; this document
maps that design onto **our codebase** and defines scope, work units, and verification.

## Scope decisions (from brainstorming)

- **Scope:** visual language only. The refresh assets originated as an *Android client*
  design; Android-only features (QR scanning, clipboard import, per-app split tunneling, TV)
  are **out of scope** — this is a server admin panel that creates configs.
- **Depth:** theme + card anatomy on the existing single-page + modal SPA. No new
  navigation/screens.
- **Approach:** extend the existing inline CSS-custom-property token system (tokens already
  live in `src/www/index.html`'s `<style>` `:root`/`.dark`). Self-host fonts (drop Google
  Fonts). Remap component CSS to the new tokens and retire the legacy red→teal Tailwind
  override hacks.
- **Deferred (NOT in this subsystem):**
  - Per-client **obfuscation editor** (S1–S4 / H1–H4 / I1–I5) — net-new feature + backend
    (`wg0.json` schema + config generation); today these are server-wide env vars. The
    AMNEZIA badge and obfuscation **chips** are styled and may render **read-only** where
    obfuscation is already shown, but no per-client editing.
  - Standalone Detail/Edit *screens* — the design's screen #5/#6 visuals are applied to our
    existing **QR/config modal** and **create/inline-edit** affordances instead.
  - Subsystem #3 (`awg://v1` share-string) and #4 (dual config export) — tokens/components
    leave room (copy affordance, choice dialog) but neither is built here.

## Current UI inventory (what changes)

Single `src/www/index.html` (~49k, inline Vue 2 template) + `src/www/js/app.js` + Tailwind
(`tailwind.config.js` → `www/css/app.css`). Theme tokens are inline in the `<style>` block;
light/dark via a `.dark` root class (already present).

| Current element (index.html) | Maps to SPEC.md |
|---|---|
| Login view (`v-if="authenticated"` gate; logo at `:950`) | Screen 1 — Login |
| Header toolbar (logo `:499`, theme/charts/logout buttons, update banner `:546`) | Screen 2 — Header toolbar |
| "Clients" `awg-card` + per-client rows (`:562`–) | Screen 3 — Client list |
| (no clients) | Screen 4 — Empty state (net layout, but no behavior) |
| QR-code modal (`:421` styles) showing config/QR | Screen 5 — Detail/config (applied to modal, read-only) |
| Create-client modal | Screen 6 + Screen 7 (Create) — styled as modal, no obfuscation editor |
| Delete-confirmation modal | Screen 7 — Delete |

Legacy quirk to remove: this fork remapped wg-easy's red accent to teal via CSS overrides
(`.dark .dark\:bg-red-800 { background-color: var(--accent) }`, index.html ~113–147). The
restyle migrates these elements to real token classes and deletes the override hacks.

## Token migration

Replace the current `:root`/`.dark` variable block with `tokens.css`. The current code uses
legacy names (`--accent`, `--accent-hover`, `--surface-light`, `--bg-light`, `--online`,
`--text-light*`, etc.); the new set uses M3 role names (`--primary`, `--surface-container`,
`--on-surface`, `--outline-variant`, `--status-connected`, …).

Migration rule: **migrate component CSS to the new role names** (don't keep two systems). A
short compatibility alias block (e.g. `--accent: var(--primary)`) is permitted only as a
temporary shim during the migration and must be removed before the unit is considered done —
no legacy token names remain at the end.

Light primary `#0D9488` is unchanged from today, so the palette shift is a refinement, not a
hard cutover; most regressions will be in surfaces/lines/dark-mode, not the accent.

## Font migration

- Vendor the 6 TTFs to `src/www/fonts/`.
- Add `@font-face` rules (Manrope 400/500/600/700, JetBrains Mono 400/500) in the `<style>`
  block; set `--font-sans: "Manrope", …` (already `--font-mono` JetBrains Mono).
- **Remove** the Google Fonts `<link>`s (index.html:13–15). Self-hosted only.

## Logo migration

- Vendor `signal-mark.svg` (+ `signal-mark-light.svg` for dark) to `src/www/img/`.
- Replace `img/logo.svg` usages at index.html:499 (header, 44px) and :950 (login, 60px),
  swapping the dark-mode variant via existing `.dark` styling.
- Favicon/`manifest.json`/apple-touch-icon: optional regeneration from the mark; lowest
  priority, can stay as-is if time-boxed.

## Component anatomy (per SPEC.md "Components")

Apply: filled/tonal/text/danger buttons (12px filled, never pill); outlined text inputs
(2px `--primary` focus); 8px status dots; **AMNEZIA badge**; obfuscation **chips** (mono,
read-only); **field-row** (caps label / value, divider between rows, section label outside
card); switches; cards (`surface-container`, 1px `outline-variant`, 16px, flat); **connected
client row** (`--connected-fill` + 1.5px `--connected-stroke`, 18px, stats strip:
DOWNLOAD/UPLOAD mono `--primary`, HANDSHAKE mono `--on-surface`); modals
(`surface-container-lowest`, 18px, `--elevation-modal`, scrim).

The connected-row **stats strip** is a re-presentation of data the UI already polls (TX/RX +
latest handshake) — restyle, not new data. ApexCharts (already vendored, gated by the charts
toggle) mounts inside the connected card, sparkline style, `--primary` stroke.

## Work units (for the implementation plan)

1. **Foundations** — vendor fonts + marks; `@font-face`; fold `tokens.css` into `:root`/`.dark`;
   remove Google Fonts link; remove red→teal override hacks; add alias shim if needed.
   *Gate:* page renders in both themes with no missing-token fallbacks; `npm run buildcss` clean.
2. **Global chrome** — header toolbar (flat `--surface`, circular icon buttons, tonal update
   banner) + login card (drop neumorphic glow) + Signal mark swap.
3. **Client list** — "Clients" card; connected rows (tint + stroke + stats strip) vs
   disconnected (plain, grey dot, single mono meta line); inline name/address edit restyled;
   row actions (enable toggle · download · QR · delete); **empty state** (centered mark + hint).
4. **Modals** — Create (teal tile, disabled until named), Delete (`error-container` well, mono
   name, `--error` button), QR/config (white card on scrim, quiet-zone pad, name caption);
   field-row anatomy + read-only AMNEZIA badge/obfuscation chips where obfuscation is shown.
5. **Polish** — full dark-mode pass; 10-locale overflow check (no fixed-width text); responsive
   (content column ~680px, full-width cards + wrapping stats on mobile); final board diff.

## Testing & verification

- No automated UI tests exist (project relies on ESLint + manual testing).
- Each unit: `cd src && npm run lint` clean; `npm run buildcss` regenerates `www/css/app.css`
  without errors; **manual visual check in light AND dark**; compare against
  `visual-board.dc.html`.
- Tailwind purge: confirm classes still in use after markup changes aren't purged (content
  glob is `./www/**/*.{html,js}`).
- Regression watch: removing the red→teal overrides must not leave stray red; auth flow,
  create/delete/QR, charts toggle, and theme toggle all still work (behavior unchanged).

## Risks

- Large class churn in the 49k `index.html`; mitigate by working unit-by-unit and visually
  verifying each.
- Legacy override removal could regress accent colors; covered by the regression watch above.
- Self-hosted fonts increase image size slightly (~900KB of TTF); acceptable, and removes the
  external Google Fonts dependency (a privacy + offline win for a self-hosted VPN admin).

## Out of scope (explicit)

Per-client obfuscation editing + its backend; new navigation/screens; QR scanning; clipboard
import; split tunneling; `awg://v1` share-string (#3); dual config export (#4).
