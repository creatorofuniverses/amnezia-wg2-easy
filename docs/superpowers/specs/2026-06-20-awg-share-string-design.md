# `awg://v1` Share-String (Export) — Design

**Date:** 2026-06-20
**Subsystem:** #3 of the proxy-redesign split (after #1 native datapath/responder, #2 web UI restyle).
**Status:** Approved (brainstorm complete) — ready for implementation plan.

## Goal

Let the web UI produce an `awg://v1/` **config share-string** for an existing client, so a user can
import that client into the AmneziaWG **Android** app via paste, deep-link, or share-sheet. The
string encodes the same client `.conf` the app already generates for download/QR, plus a friendly
tunnel-name comment.

## Scope

**In scope (export / generate only):**
- A backend codec for the `awg://v1/` wire format.
- A backend method + HTTP endpoint that returns the share-string for a client.
- A "Copy share link" button in the client config/QR modal, with a non-secure-context clipboard
  fallback.
- A `# Name = <client name>` comment in the encoded conf so the imported tunnel is labeled.
- A `node:test` unit suite for the codec (the first Node-side test), pinned to the Android
  reference vectors.

**Out of scope (explicitly deferred / not built):**
- **Import / decode in the web UI** (pasting an `awg://v1/` string to create a server-side
  client). The server remains the source of truth for clients. The `decode()` function IS
  implemented in the codec module, but only to enable cross-compat testing — it is not wired to
  any route or UI.
- A second / replacement QR encoding the `awg://` string (the existing raw-`.conf` QR is
  untouched).
- Subsystem #4 (dual old/new config export) — separate spec.
- Any encryption of the share-string (see Security).

## Wire format (`awg://v1`)

Reference implementation: `org.amnezia.awg.config.ConfigShare` in the AmneziaWG Android repo
(`tunnel/src/main/java/org/amnezia/awg/config/ConfigShare.java`), with test vectors at
`tunnel/src/test/resources/share-vector.{awg,conf}`.

```
awg://v1/<base64url( zlib( utf8(conf) ) )>
```

- **Inner payload:** UTF-8 bytes of a WireGuard `.conf` (INI-style `key = value`, `[Interface]` /
  `[Peer]` sections), carrying both standard WireGuard fields and the AmneziaWG obfuscation params
  (`Jc/Jmin/Jmax`, `S1–S4`, `H1–H4`, `I1–I5`, `ImitateProtocol`). An optional leading
  `# Name = <name>` comment supplies the tunnel name.
- **Compression:** RFC 1950 **zlib** (Java `Deflater(BEST_COMPRESSION, nowrap=false)`). Node
  equivalent: `zlib.deflateSync(buf, { level: zlib.constants.Z_BEST_COMPRESSION })` /
  `zlib.inflateSync`.
- **Encoding:** RFC 4648 §5 **base64url**, **no padding** on output. Decode is tolerant: it accepts
  the URL-safe alphabet (`-_`), the standard alphabet (`+/`), and optional `=` padding.
- **Prefix:** literal `awg://v1/`.
- No magic bytes, no length prefix, no checksum (the zlib trailer provides integrity), no
  encryption.

**Cross-compat is verified by `decode`, not by byte-exact `encode`.** Java's and Node's deflate
emit different compressed bytes for the same input; both inflate to identical plaintext. Therefore
the test suite asserts `decode(android_vector) === expected_plaintext` and round-trip equality —
**never** `encode(plaintext) === android_vector` (that would be wrong and would fail).

## Architecture & components

Four small, independently-testable units.

### 1. `src/lib/awgShareString.js` — pure codec (no app dependencies)

- `encode(confText: string): string`
  → `'awg://v1/' + base64urlNoPad(zlib.deflateSync(Buffer.from(confText, 'utf8'), { level: 9 }))`.
- `decode(shareString: string): string`
  → validate `awg://v1/` prefix; normalize + base64-decode the suffix (tolerant alphabet/padding);
  `zlib.inflateSync`; return UTF-8 string. Throws `Error` with a clear message on: missing/wrong
  prefix, malformed base64, truncated/corrupt zlib.
- Node stdlib only (`zlib`, `Buffer`). No export to the API layer; `decode` exists for tests and as
  living documentation of the format.

### 2. `WireGuard.getClientShareString({ clientId }): Promise<string>`

- `conf = await getClientConfiguration({ clientId })` (reuses existing config generation — all AWG
  params, PrivateKey, Endpoint, etc.).
- `name = (client.name).replace(/[\r\n]+/g, ' ').trim()` — strip CR/LF to prevent comment/line
  injection from a user-controlled name.
- Return `awgShareString.encode('# Name = ' + name + '\n' + conf)`. (`getClientConfiguration`
  output begins with a newline, so the comment sits cleanly above `[Interface]`.)
- Sensitivity and the missing-key case match `getClientConfiguration` exactly (no new server-side
  gate; the UI disables the button when `downloadableConfig` is false).

### 3. Route: `GET /api/wireguard/client/:clientId/share-string`

- Near-copy of the `configuration` route (`src/lib/Server.js:153`): same session auth, same
  prototype-pollution param guard, `Content-Type: text/plain`, body = the `awg://v1/…` string.
- A not-found client surfaces the same error as the `configuration` route.

### 4. Frontend — `src/www/js/api.js` + the config modal in `src/www/index.html`

- `api.getClientShareString(clientId)` — fetch the endpoint, return the text body.
- A **"Copy share link"** button in the config/QR modal, styled with the subsystem-#2 button
  classes, gated on `downloadableConfig` exactly like the existing QR/download controls.
- On click: fetch the string, copy it, show a brief "Copied!" confirmation.

## Data flow

```
[config modal] "Copy share link"
   → api.getClientShareString(id)
   → GET /api/wireguard/client/:id/share-string
   → WireGuard.getClientShareString({id})
        → getClientConfiguration({id})            (.conf text)
        → prepend "# Name = <sanitized name>\n"
        → awgShareString.encode(conf)             (awg://v1/…)
   → text/plain response
   → clipboard.writeText(string)  (+ fallback)    → "Copied!"
```

## Error handling

- **Client not found:** route surfaces the existing not-found error (mirrors `configuration`).
- **No private key (`downloadableConfig === false`):** button disabled in the UI, identical to the
  current QR/download buttons; endpoint behavior mirrors `configuration` (no new server gate) for
  consistency across the three export paths.
- **Name with CR/LF:** stripped in `getClientShareString` before embedding in the `# Name` comment
  (single, contained sanitization point).
- **Clipboard unavailable (non-secure context):** wg-easy is frequently served over plain HTTP on a
  LAN/VPN IP, where `navigator.clipboard` is `undefined`. Fallback chain: `navigator.clipboard.
  writeText` → hidden-`textarea` + `document.execCommand('copy')` → on failure, reveal the string
  in a selectable read-only field so the user can copy manually. The button never silently no-ops.
- **Decode errors (tests only):** wrong prefix, truncated base64, corrupt zlib each throw a clear,
  distinct message.

## Testing

First Node-side test suite — `node:test` (built into Node 18+, **no new dependency**), run via a
new `npm test` script (`node --test`). Scoped to the codec.

- **Cross-compatibility (primary):** vendor the Android vectors into the repo (e.g.
  `src/lib/__tests__/fixtures/share-vector.{awg,conf}`); assert
  `decode(read(share-vector.awg)) === read(share-vector.conf)`.
- **Round-trip:** `decode(encode(sampleAwgConf)) === sampleAwgConf` for a conf containing the AWG
  params.
- **Format invariants:** `encode(x)` starts with `awg://v1/`; the body contains no `=`, `+`, or `/`.
- **Decode tolerance:** decode succeeds on input using the standard `+/` alphabet and/or `=`
  padding.
- **Error cases:** wrong prefix / truncated base64 / corrupt zlib each throw.
- **Explicit non-assertion:** no test compares `encode(...)` bytes to the Android vector (see Wire
  format).

ESLint remains the lint gate; `npm run lint` must stay clean.

## Security / trust

The share-string contains the client **PrivateKey**, exactly like the existing QR image and the
`.conf` download. It carries no *additional* sensitivity beyond those paths and is served behind the
same session auth. It is **not** encrypted — consistent with the Android reference (sharing is the
user's responsibility; the secret is the same one already exposed by QR/download). The "Copy share
link" button therefore sits with, and is gated identically to, the existing QR/download controls.

## File structure

**Create:**
- `src/lib/awgShareString.js` — codec (`encode`/`decode`).
- `src/lib/__tests__/awgShareString.test.js` — `node:test` suite.
- `src/lib/__tests__/fixtures/share-vector.awg`, `share-vector.conf` — vendored Android vectors.

**Modify:**
- `src/lib/WireGuard.js` — add `getClientShareString({ clientId })`.
- `src/lib/Server.js` — add the `share-string` route.
- `src/www/js/api.js` — add `getClientShareString`.
- `src/www/index.html` — "Copy share link" button + copy/fallback logic (+ i18n key for the label;
  English plus a sensible fallback, not all locales).
- `package.json` — add `"test": "node --test"`.

## Out-of-scope / deferred (recap)

Import/decode UI, a second/replacement QR, encryption, and subsystem #4 (dual export) are not part
of this work. The codec's `decode()` exists for tests only.
