# Web UI Restyle — Design Brief (for an external design pass)

> **Purpose:** Hand this to a design assistant ("Claude design") to get back **implementable web
> artifacts** (design tokens, component styles, static HTML/CSS mockups) for restyling the
> AmneziaWG Easy admin web UI — not generic mockups, and **not** Android XML/React.
>
> **Status:** Subsystem #2 (UI restyle). Scope decided in brainstorming: **visual language only**
> (adopt the look onto existing web screens; skip Android-only features), depth = **theme + card
> anatomy** on the existing Vue 2 SPA (keep single-page + modal flow).

---

## a) Stack & desired output artifacts

### Target stack (the design must be implementable in this)
- **Vue.js 2 SPA**, vendored (no bundler). The whole app is one `src/www/index.html` with inline
  Vue directives. **No build step except Tailwind CSS.**
- **Tailwind CSS** — config in `tailwind.config.js`; source compiled `app.css` → `www/css/app.css`.
- **Theming via CSS custom properties** declared inline in `index.html`'s `<style>` block;
  **light/dark switch via a `.dark` class on the root** (already implemented).
- **Fonts:** JetBrains Mono (keep — keys/IPs/stats) + the sans face moves to **Manrope**.
- Charts via **ApexCharts** (already vendored).

### Design language to adopt (already decided)
Material 3 **"Network Teal"** — primary `#0D9488` (light) / `#2DD4BF` (dark), **Manrope** +
**JetBrains Mono**, the **Signal logo mark** (SVGs provided). The current accent is *already*
`#0d9488`, so this is a **refinement, not a from-scratch rebrand**. Source assets (M3 `colors.xml`
light/night values, Signal mark SVGs, anatomy specs) are attached.

### What to hand back, in priority order
1. **A complete design-token set as CSS custom properties** — full M3 role set (`--primary`,
   `--primary-container`, `--on-primary`, `--surface`, `--surface-container`, `--on-surface`,
   `--outline`, `--secondary`, plus status colors connected/connecting/disconnected, danger) for
   **both light and dark**, structured to live in a `:root` / `.dark` block.
2. **A typography scale** — Manrope weights + JetBrains Mono usage rules (which elements are mono:
   keys, IPs, addresses, stat numbers).
3. **Component styles** (CSS, Tailwind-compatible, no framework lock-in) for: cards
   (surfaceContainer, ~16px radius, 1px outline), buttons (filled / tonal / text), text inputs
   (outlined, focused primary stroke), modals/dialogs, status dots + pills/chips, the **AMNEZIA
   obfuscation badge** and obfuscation **param chips**, and a **field-row** pattern (caps label
   above, mono value below, divider between rows).
4. **Annotated static HTML/CSS mockups** of each screen — self-contained `.html`, **no React/Vue,
   no Android XML** — that can be ported into the Vue template. One per screen in section (b).

### Hard constraints / please avoid
- No Android XML, no React/JSX, no SCSS needing a new build, no heavy CSS frameworks.
- Plain **HTML + CSS variables + Tailwind utility classes** only.
- Must degrade gracefully across **10 UI languages** (variable text length).
- **Desktop-first** but usable on mobile.

---

## b) Functionality — current and planned

**This is a server-side admin panel** that creates and hands out VPN client configs. It does **not**
run on phones. So **do not design**: QR *scanning*, clipboard *import*, per-app split tunneling, or a
TV layout — those came from the Android client design and don't apply here.

### Current screens/features (screenshots attached)
- **Login** — single password field (when a password is set).
- **Header toolbar** — light/dark toggle, charts toggle, logout, "new version available" banner.
- **Client list** — a card containing one row per client. Each row: avatar/Gravatar, online status
  dot, **inline-editable name**, **inline-editable address**, traffic **TX/RX** (inline values, or
  expanded stats with optional live ApexCharts), **last-handshake** age, **enable/disable** toggle,
  **download config**, **show QR code**.
- **Create-client modal**, **delete-confirmation modal**, **QR-code modal**.
- Backend supports AmneziaWG 2.0 obfuscation params (S1–S4, H1–H4 ranges, I1–I5) — not currently
  surfaced richly in the UI.

### Planned in this restyle (design these)
- Apply the token system, Manrope, and Signal mark throughout.
- **Client-list cards** in the new anatomy: disconnected = plain surfaceContainer; **connected =
  teal-tinted card** with a **live stats strip** (DOWNLOAD / UPLOAD rates + HANDSHAKE age, mono).
- **Config / detail view** in field-row anatomy with collapsible **Interface / Obfuscation / Peer**
  groupings, an **AMNEZIA badge**, and obfuscation params shown as **chips**.
- **Create/edit client** form with outlined inputs; obfuscation params as grouped number fields.
- **Settings/about** content grouped into cards (Signal mark tile, version pill).
- **Empty state** (no clients): centered Signal mark + hint.

### Planned later (leave room for; light touch)
- **Config share-string** (`awg://v1/…`) — a copy-to-clipboard "share" affordance on a client.
- **Dual config export** — export a client config in old vs new format (a small choice/dialog).

Design these two as optional/forward-looking components if convenient; not the focus.

---

## Attachments checklist (what to upload alongside this brief)

**Screenshots of the current UI (light + dark if possible):**
- [ ] Login screen
- [ ] Client list — empty state (no clients)
- [ ] Client list — with several clients (one connected, one disconnected)
- [ ] Client list — with charts toggled on (ApexCharts overlay)
- [ ] Create-client modal
- [ ] QR-code modal
- [ ] Delete-confirmation modal

**Source design assets** (from `~/Documents/amneziawg-refresh-assets/`):
- [ ] `logo/signal-mark.svg`, `signal-mark-light.svg`, `signal-mark-mono.svg`, `signal-mark-on-dark.svg`
- [ ] `res/values/colors.xml` (light M3 tokens) and `res/values-night/` (dark tokens)
- [ ] `res/font/` (Manrope / JetBrains Mono references) — or just note the family names
- [ ] The anatomy SPECs for visual reference only (treat as Android, translate to web):
      `SPEC-detail-anatomy.md`, `SPEC-detail-editor-split-v2.md`, `SPEC-settings.md`
- [ ] This brief

**Current code (optional, for exact token/class names):**
- [ ] `src/www/index.html` (the `<style>` block has the current `:root`/`.dark` CSS variables)
- [ ] `tailwind.config.js`
