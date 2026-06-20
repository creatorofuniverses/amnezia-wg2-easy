# AmneziaWG Web Admin — Restyle Spec

Implementable artifacts for restyling the **AmneziaWG Easy** admin web UI to the
"Network Teal" (Material 3) language. Visual language only — no IA / behavior changes.

**Files in this folder**
- `tokens.css` — full M3 token set, both themes (`:root` light / `.dark` dark). Drop into `index.html`'s `<style>`.
- `fonts/` — Manrope (400/500/600/700) + JetBrains Mono (400/500). Self-host; no Google Fonts.
- `assets/signal-mark*.svg` — the Signal logo mark (on-light / mono variants).
- `../AmneziaWG Web Admin.dc.html` — the annotated visual board (all screens + components). Open in browser; use as the pixel reference.

## Conventions
- **Theme:** light is default (`:root`); add class `dark` on the root element to switch. Tokens flip; status-color *meaning* stays constant.
- **Fonts:** Manrope for all proportional text. JetBrains Mono ONLY for keys, IPs, addresses, ports, and stat values (digits/hex align).
- **Shape:** 12px inputs/chips · 16px cards · 18px list rows · 999px pills/toggles. **Filled buttons are 12px rounded rect — never a pill.**
- **Elevation:** flat. Cards = `surface-container` fill + 1px `outline-variant` stroke, no shadow. Only modals lift (`--elevation-modal`).
- **Spacing:** 4px grid (4/8/12/16/24); 16px card padding; 12px gap between cards.
- **Casing:** Title case for titles/names; ALL-CAPS only for section labels (INTERFACE/OBFUSCATION/PEER) and stat labels (DOWNLOAD/UPLOAD/HANDSHAKE); sentence case for body.

## Components
| Component | Spec |
|---|---|
| **Filled button** | `--primary` bg / `--on-primary` text · 12px radius · 600 weight · 14px. Disabled → `surface-variant`/grey text. |
| **Tonal button** | `--primary-container` bg / `--on-primary-container` text. |
| **Text button** | transparent · `--on-surface-variant`. |
| **Danger button** | `--error` bg / white text (destructive only). |
| **Text input** | `surface-container` fill · 1px `outline-variant` · 12px radius · 12px pad. Focus → **2px `--primary`** stroke. Invalid → 1px `--error`. |
| **Status dot** | 8px circle: connected `--status-connected`, connecting `--status-connecting`, disconnected `--status-disconnected`. |
| **AMNEZIA badge** | `--primary-container` bg / `--on-primary-container` text · 9–10px / 700 / caps / ls .04–.06em · full radius. Static (not tappable). OBFUSCATION section only. |
| **Obfuscation chip** | mono 12px · `surface-container-highest` fill · 1px `outline-variant` · full radius. Label (`S1`) muted `on-surface-variant`, value `on-surface`. |
| **Field row** | label (12px `on-surface-variant`) over value (15px/600 `on-surface`, mono if key/IP). 9px vertical pad. 1px `outline-variant` divider between rows, **none after the last**. Section label sits OUTSIDE/above the card: 11px/700/caps `--primary`, ls .12em, 4px from edge, 7px above card. |
| **Switch** | ON → `--primary` track, white thumb; OFF → `surface-variant` track. 44×26 / 18–20px thumb. |
| **Card** | `surface-container` · 1px `outline-variant` · 16px radius · flat. |
| **Connected client row** | `--connected-fill` bg + **1.5px `--connected-stroke`** · 18px radius. Green dot by name. Stats strip below a 1px divider: DOWNLOAD/UPLOAD in mono `--primary`, HANDSHAKE in mono `--on-surface`. |
| **Modal** | `surface-container-lowest` card · 18px radius · `--elevation-modal` shadow · scrim `rgba(16,24,40,.45)`. Footer on `surface-container-low` with a top 1px `outline-variant` divider. |

## Screens (see the board)
1. **Login** — centered card, 16px radius (drop the neumorphic glow). Signal mark + wordmark above. Avatar well in solid `--primary`. Outlined password input → 2px focus. Filled Sign In (12px radius), disabled until non-empty.
2. **Header toolbar** — flat on `--surface`. Logo + wordmark left; 38px circular icon buttons right (theme toggle, charts toggle, logout). Update banner = `--primary-container` tonal fill (not red), mono version, filled "Update" pill.
3. **Client list** — "Clients" card; New button (filled, leading +) in header. Connected rows tinted + stats strip; disconnected rows plain, grey dot, single mono meta line. Row actions: enable toggle · download · QR · delete. Name & address inline-editable.
4. **Empty state** — centered Signal mark in a 24px `surface-container-highest` well + short hint. Keep New in the card header.
5. **Detail / config** — back button + name + Edit. Teal connection summary card (status + switch + 3-col stats); OFF → drops tint, hides stats, shows "Tap to connect". Sections INTERFACE / OBFUSCATION (+ AMNEZIA badge) / PEER as field-row cards. Obfuscation params behind an "Advanced" expander → chips.
6. **Create / edit form** — close + title + Save (filled, 12px). Outlined Name (focus state) + Address (mono). Obfuscation behind an "Advanced parameters" expander → grouped mono number fields (S1–S4, H1–H4, I1–I5) in a 4-up grid; blank = inherit server default.
7. **Modals** — Create (teal + tile, disabled Create until named); Delete (danger well in `error-container`, mono client name, filled `--error`); QR (white card on scrim, 14px quiet-zone pad, name caption, ✕ close).

## Forward-looking (not built; tokens cover them)
- Config **share-string** (`awg://v1/…`) → a copy affordance on a client row (reuse the copy icon + tonal button).
- **Dual export** (old vs new format) → a small choice dialog reusing the modal + tonal-button patterns.

## Notes
- Degrades across 10 locales: buttons/labels size to content, no fixed-width text. Desktop-first, content column ~680px, usable down to mobile (cards go full-width, stats strip wraps).
- ApexCharts (already vendored) can mount inside the connected summary card / expanded row when the charts toggle is on — keep it inside the teal card, sparkline style, `--primary` stroke.
