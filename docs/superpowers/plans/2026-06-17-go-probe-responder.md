# Go Probe-Responder — Implementation Plan (Plan 2 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Go probe-responder (`responder/`) that sits as an NFQUEUE ingress filter on `WG_PORT`, answering active DPI scanners with protocol-valid **DNS / STUN / QUIC-Version-Negotiation** replies while leaving real AmneziaWG traffic on the kernel fast path — gated behind a new `RESPONDER` env toggle, off by default.

**Architecture:** Native AWG (`awg-quick`, Plan 1) owns `WG_PORT/udp`. When `RESPONDER=true`, the entrypoint inserts an iptables rule that queues only conntrack-`NEW` inbound UDP to NFQUEUE 0; a supervised Go side-process reads each queued datagram and, per the **correctness-critical order**, (1) ACCEPTs anything that classifies as a real AWG handshake **or transport** packet, (2) else, if the datagram matches `IMITATE_PROTOCOL`'s wire signature, builds a single-shot reply, injects it via a **raw socket** (forged `sport=WG_PORT`, v4+v6), and DROPs the queued packet, (3) else ACCEPTs (kernel `awg0` silently drops genuine junk). Established/unmatched flows never reach userspace. The responder is crash-isolated: it runs as a side process under `dumb-init`, the NFQUEUE rule carries `--queue-bypass` (fails open to `awg0` if no listener), and on responder exit the entrypoint tears the rule down — connectivity is never affected, only active-probe defense.

**Tech Stack:** Go 1.24 (module `awg-responder`, package `main`), `github.com/florianl/go-nfqueue/v2` (userspace NFQUEUE), `golang.org/x/sys/unix` (raw sockets, `IP_HDRINCL`), Alpine runtime, POSIX `sh` entrypoint, `iptables` + `conntrack`. No QUIC stack and no connmark in this plan — those are Plan 3.

## Global Constraints

- **Scope cut — this is the NON-QUIC-handshake responder.** Plan 2 ships **QUIC Version-Negotiation only** (single-shot), **DNS SERVFAIL**, and **STUN Binding-Success** (single-shot). The full TLS-1.3 QUIC handshake continuation, the connmark flow-claim (Review R2-1), and `QUIC_HANDSHAKE`/`QUIC_CERT_DOMAIN` are **Plan 3** — do **not** wire them here.
- **SIP is shaping-only (Decision 8).** No SIP responder. `IMITATE_PROTOCOL=sip` with `RESPONDER=true` runs only the classify→ACCEPT path and the entrypoint logs a warning that SIP probes are not answered. Document it as the *least-protected* combination (Review R2-5), not a peer of QUIC/DNS/STUN.
- **Verdict order is correctness-critical (design §"Per-packet verdict"):** `classifyAwgPacket` runs **first**, before any protocol detection, because client→server shaping can make a real handshake-init resemble the answered protocol (e.g. `IMITATE=dns`). The classifier must cover **transport** packets (S4/H4), not just handshakes, so a mid-stream packet re-entering as `NEW` after the conntrack idle-timeout is recognized as real (Review F6).
- **Responder answers only as `IMITATE_PROTOCOL`** and ignores probes of other protocols, the way a real single-protocol server does.
- **iptables ordering (verified against `src/config.js:25`):** the Node app's `WG_POST_UP` appends `iptables -A INPUT -p udp --dport ${WG_PORT} -j ACCEPT`. An *appended* NFQUEUE rule would be shadowed by it. The entrypoint must **insert** the NFQUEUE rule at the head: `iptables -I INPUT 1 ...`, and include `--queue-bypass` so a dead/unattached responder fails open.
- **Capabilities:** `CAP_NET_ADMIN` (NFQUEUE + iptables) **and** `CAP_NET_RAW` (raw-socket reply injection). The IPv6 reply path **must** set the UDP checksum (mandatory in v6; optional in v4) — a missing v6 checksum is silently dropped (Review R2-4).
- **Config source of truth:** the responder reads `${WG_PATH}/wg0.conf` (default `/etc/amnezia/amneziawg/wg0.conf`) **once at startup** for `S1–S4`/`H1–H4` (params are generated once and stable). Both responder and datapath shaping derive from the one `IMITATE_PROTOCOL` env var, so they cannot drift.
- **All multi-byte protocol fields are big-endian** (DNS/STUN/QUIC). **The AWG obfuscated 4-byte header is little-endian** (`u32::from_le_bytes`). Do not mix these up.
- **No automated suite exists** project-wide (`CLAUDE.md`). Pure functions (parser, classifier, builders, checksums) get real `go test` unit cycles; the NFQUEUE loop, raw-socket send, Dockerfile, and entrypoint are build- and manually-verified. Run unit tests with the local Go toolchain: `cd responder && go test ./...`.
- **Commits:** conventional-commit prefixes; end every commit message with the trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

---

## File Structure

All new code lives in `responder/` (repo root), one Go module, package `main`, split by responsibility:

- `responder/go.mod` — module `awg-responder`, Go 1.24, deps `go-nfqueue/v2` + `golang.org/x/sys`.
- `responder/awg.go` — `AwgParams`, `HRange`, `classifyAwgPacket` (handshake + transport, all 4 S/H pairs).
- `responder/config.go` — `ParseConfig(path)` → `AwgParams` from `wg0.conf` `[Interface]`.
- `responder/detect.go` — `detectQUIC` / `detectDNS` / `detectSTUN`, `isQUICVersion`, `dnsQnameEnd`.
- `responder/dns.go` — `buildDNSServfail(incoming)`.
- `responder/stun.go` — `buildSTUNBindingSuccess(incoming, clientIP, clientPort)` (v4 + v6).
- `responder/quicvn.go` — `buildQUICVersionNegotiation(incoming)`.
- `responder/egress.go` — `udpChecksum`, `ipv4Checksum`, `buildIPv4UDP`, `sendRawUDP` (v4 + v6).
- `responder/packet.go` — `parseIPv4UDP` / `parseIPv6UDP`: split an NFQUEUE L3 packet into addrs/ports/payload.
- `responder/responder.go` — `Verdict` enum + `decide(payload, cfg)`: the classify→detect→respond decision (pure; no I/O).
- `responder/nfqueue.go` — the `go-nfqueue` loop wiring `decide` to verdicts + raw egress.
- `responder/main.go` — env parsing (`WG_PORT`, `IMITATE_PROTOCOL`, `WG_PATH`), startup, `ParseConfig`, run loop.
- `responder/*_test.go` — unit tests per pure-function file.

Modified outside `responder/`:
- `Dockerfile` — add a Go build stage for the responder; copy the binary into runtime; add the entrypoint.
- `docker-entrypoint.sh` — **new**: supervise Node UI + (optionally) responder, render/tear-down iptables.
- `docker-compose.yml` — add `RESPONDER` env + `NET_RAW` cap.
- `.env.example` — document `RESPONDER`.
- `README.md` — "Active-probe responder" section.

---

## Task 1: Go module scaffold + `wg0.conf` S/H parser

**Files:**
- Create: `responder/go.mod`, `responder/awg.go` (types only), `responder/config.go`
- Test: `responder/config_test.go`

**Interfaces:**
- Produces: `type HRange struct { Min, Max uint32 }` with `func (r HRange) Contains(v uint32) bool`.
- Produces: `type AwgParams struct { S1,S2,S3,S4 uint32; H1,H2,H3,H4 HRange }`.
- Produces: `func ParseConfig(path string) (AwgParams, error)` — reads an `[Interface]` block, parses `S1..S4` (uint32) and `H1..H4` (`"min-max"` or single value → point range). Keys are case-insensitive. Returns an error if any of `S1..S4`/`H1..H4` is missing.

- [ ] **Step 1: Create the module and the types file**

`responder/go.mod`:
```
module awg-responder

go 1.24
```

`responder/awg.go` (types only for now; `classifyAwgPacket` lands in Task 2):
```go
package main

// HRange is an inclusive AmneziaWG header magic range [Min, Max].
type HRange struct {
	Min, Max uint32
}

// Contains reports whether v falls within the inclusive range.
func (r HRange) Contains(v uint32) bool {
	return v >= r.Min && v <= r.Max
}

// AwgParams holds the S-padding offsets and H header ranges read from wg0.conf.
// S1/H1 = handshake-init, S2/H2 = handshake-response, S3/H3 = cookie-reply,
// S4/H4 = transport-data.
type AwgParams struct {
	S1, S2, S3, S4 uint32
	H1, H2, H3, H4 HRange
}
```

- [ ] **Step 2: Write the failing test**

`responder/config_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleConf = `[Interface]
Address = 10.8.0.1/24
ListenPort = 51820
PrivateKey = aGVsbG8=
Jc = 5
Jmin = 50
Jmax = 1000
S1 = 42
S2 = 88
S3 = 33
S4 = 120
H1 = 100-200
H2 = 300-400
H3 = 500-600
H4 = 700

[Peer]
PublicKey = d29ybGQ=
AllowedIPs = 10.8.0.2/32
`

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "wg0.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseConfig(t *testing.T) {
	p, err := ParseConfig(writeTemp(t, sampleConf))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if p.S1 != 42 || p.S2 != 88 || p.S3 != 33 || p.S4 != 120 {
		t.Errorf("S mismatch: %+v", p)
	}
	if p.H1 != (HRange{100, 200}) || p.H2 != (HRange{300, 400}) {
		t.Errorf("H1/H2 mismatch: %+v", p)
	}
	// Single value parses to a point range.
	if p.H4 != (HRange{700, 700}) {
		t.Errorf("H4 point range mismatch: %+v", p.H4)
	}
}

func TestParseConfigMissingKey(t *testing.T) {
	// Drop S4 -> must error.
	body := "[Interface]\nS1 = 1\nS2 = 2\nS3 = 3\nH1 = 1-2\nH2 = 3-4\nH3 = 5-6\nH4 = 7-8\n"
	if _, err := ParseConfig(writeTemp(t, body)); err == nil {
		t.Fatal("expected error for missing S4")
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `cd responder && go test -run TestParseConfig ./...`
Expected: FAIL — `undefined: ParseConfig`.

- [ ] **Step 4: Implement `ParseConfig`**

`responder/config.go`:
```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ParseConfig reads the [Interface] block of a wg0.conf and extracts the
// S1..S4 padding offsets and H1..H4 header ranges. Keys are case-insensitive.
func ParseConfig(path string) (AwgParams, error) {
	f, err := os.Open(path)
	if err != nil {
		return AwgParams{}, err
	}
	defer f.Close()

	kv := map[string]string{}
	inInterface := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inInterface = strings.EqualFold(line, "[Interface]")
			continue
		}
		if !inInterface {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])
		kv[key] = val
	}
	if err := sc.Err(); err != nil {
		return AwgParams{}, err
	}

	u32 := func(name string) (uint32, error) {
		v, ok := kv[name]
		if !ok {
			return 0, fmt.Errorf("missing %q in [Interface]", name)
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("bad %q: %w", name, err)
		}
		return uint32(n), nil
	}
	hrange := func(name string) (HRange, error) {
		v, ok := kv[name]
		if !ok {
			return HRange{}, fmt.Errorf("missing %q in [Interface]", name)
		}
		if dash := strings.IndexByte(v, '-'); dash > 0 {
			lo, err := strconv.ParseUint(strings.TrimSpace(v[:dash]), 10, 32)
			if err != nil {
				return HRange{}, fmt.Errorf("bad %q min: %w", name, err)
			}
			hi, err := strconv.ParseUint(strings.TrimSpace(v[dash+1:]), 10, 32)
			if err != nil {
				return HRange{}, fmt.Errorf("bad %q max: %w", name, err)
			}
			return HRange{uint32(lo), uint32(hi)}, nil
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return HRange{}, fmt.Errorf("bad %q: %w", name, err)
		}
		return HRange{uint32(n), uint32(n)}, nil
	}

	var p AwgParams
	var err2 error
	set := func(dst *uint32, name string) {
		if err2 != nil {
			return
		}
		*dst, err2 = u32(name)
	}
	setH := func(dst *HRange, name string) {
		if err2 != nil {
			return
		}
		*dst, err2 = hrange(name)
	}
	set(&p.S1, "s1")
	set(&p.S2, "s2")
	set(&p.S3, "s3")
	set(&p.S4, "s4")
	setH(&p.H1, "h1")
	setH(&p.H2, "h2")
	setH(&p.H3, "h3")
	setH(&p.H4, "h4")
	if err2 != nil {
		return AwgParams{}, err2
	}
	return p, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd responder && go test ./...`
Expected: PASS (`ok  awg-responder`).

- [ ] **Step 6: Vet**

Run: `cd responder && go vet ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add responder/go.mod responder/awg.go responder/config.go responder/config_test.go
git commit -m "feat(responder): scaffold Go module + wg0.conf S/H parser

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `classifyAwgPacket` (handshake + transport, all 4 S/H pairs)

**Files:**
- Modify: `responder/awg.go` (append `classifyAwgPacket`)
- Test: `responder/awg_test.go`

**Interfaces:**
- Consumes: `AwgParams`, `HRange` (Task 1).
- Produces: `func classifyAwgPacket(data []byte, p AwgParams) bool` — true iff `data` is a genuine AWG handshake-init/response, cookie-reply, **or transport-data** packet for these params. WireGuard payload sizes excluding S-padding: init=148, response=92, cookie=64, transport≥32. For each candidate the 4-byte header at offset `S` (little-endian u32) must fall in the matching `H` range, and the datagram length must be exactly `S + size` for the three handshake/cookie types, or **at least** `S + 32` for transport.

- [ ] **Step 1: Write the failing test**

`responder/awg_test.go`:
```go
package main

import (
	"encoding/binary"
	"testing"
)

// makeAwg builds a synthetic AWG datagram: sOff random bytes, then a 4-byte
// LE header equal to hdr, then enough trailer to reach totalLen.
func makeAwg(sOff int, hdr uint32, totalLen int) []byte {
	d := make([]byte, totalLen)
	for i := 0; i < sOff && i < totalLen; i++ {
		d[i] = byte(0x40 + i) // arbitrary non-zero padding
	}
	if sOff+4 <= totalLen {
		binary.LittleEndian.PutUint32(d[sOff:], hdr)
	}
	return d
}

var testParams = AwgParams{
	S1: 8, S2: 12, S3: 16, S4: 20,
	H1: HRange{100, 200}, H2: HRange{300, 400},
	H3: HRange{500, 600}, H4: HRange{700, 800},
}

func TestClassifyHandshakeInit(t *testing.T) {
	// init = 148 payload, so totalLen = S1 + 148.
	d := makeAwg(8, 150, 8+148)
	if !classifyAwgPacket(d, testParams) {
		t.Fatal("valid handshake-init not classified")
	}
}

func TestClassifyTransportMinAndLarger(t *testing.T) {
	// transport: header in H4, len >= S4 + 32. Test exact-min and larger.
	for _, n := range []int{20 + 32, 20 + 1400} {
		d := makeAwg(20, 750, n)
		if !classifyAwgPacket(d, testParams) {
			t.Fatalf("valid transport len=%d not classified", n)
		}
	}
}

func TestClassifyRejectsWrongHeader(t *testing.T) {
	// init size but header outside H1.
	d := makeAwg(8, 999, 8+148)
	if classifyAwgPacket(d, testParams) {
		t.Fatal("header outside H1 must not classify as init")
	}
}

func TestClassifyRejectsWrongSize(t *testing.T) {
	// header in H1 but length is not S1+148 (and not any other exact size).
	d := makeAwg(8, 150, 8+100)
	if classifyAwgPacket(d, testParams) {
		t.Fatal("wrong handshake size must not classify")
	}
}

func TestClassifyRejectsJunk(t *testing.T) {
	if classifyAwgPacket([]byte{1, 2, 3}, testParams) {
		t.Fatal("short junk must not classify")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd responder && go test -run TestClassify ./...`
Expected: FAIL — `undefined: classifyAwgPacket`.

- [ ] **Step 3: Implement `classifyAwgPacket`**

First add the import at the **top** of `responder/awg.go`, immediately under the `package main` line (imports must precede declarations):
```go
package main

import "encoding/binary"
```

Then append the rest to the end of `responder/awg.go`:
```go
// WireGuard message sizes (S-padding excluded).
const (
	wgHandshakeInitSize     = 148
	wgHandshakeResponseSize = 92
	wgCookieReplySize       = 64
	wgTransportMinSize      = 32
)

// classifyAwgPacket reports whether data is a genuine AmneziaWG packet for the
// given params. It tries all four (S-offset, H-range, size) candidates in
// order. The obfuscated 4-byte header at the S-offset is read little-endian and
// must fall in the matching H-range. Handshake/cookie types require an exact
// length (S + size); transport requires at least S + 32.
func classifyAwgPacket(data []byte, p AwgParams) bool {
	type cand struct {
		off   uint32
		rng   HRange
		size  int
		exact bool
	}
	cands := []cand{
		{p.S1, p.H1, wgHandshakeInitSize, true},
		{p.S2, p.H2, wgHandshakeResponseSize, true},
		{p.S3, p.H3, wgCookieReplySize, true},
		{p.S4, p.H4, wgTransportMinSize, false},
	}
	for _, c := range cands {
		off := int(c.off)
		if len(data) < off+4 {
			continue
		}
		if c.exact {
			if len(data) != off+c.size {
				continue
			}
		} else if len(data) < off+c.size {
			continue
		}
		hdr := binary.LittleEndian.Uint32(data[off : off+4])
		if c.rng.Contains(hdr) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd responder && go test ./...`
Expected: PASS.

- [ ] **Step 5: Vet**

Run: `cd responder && go vet ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add responder/awg.go responder/awg_test.go
git commit -m "feat(responder): classifyAwgPacket for handshake and transport, all S/H pairs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Protocol detection (`detectQUIC` / `detectDNS` / `detectSTUN`)

**Files:**
- Create: `responder/detect.go`
- Test: `responder/detect_test.go`

**Interfaces:**
- Produces: `func detectQUIC(data []byte) bool`, `func detectDNS(data []byte) bool`, `func detectSTUN(data []byte) bool`. Each is the strict wire discriminator ported from `responder.rs:163–332`.
- Produces (internal, used by detectors and Task 4): `func isQUICVersion(v uint32) bool`, `func dnsQnameEnd(data []byte, start int) (int, bool)`.
- Constants produced for reuse: `stunMagicCookie uint32 = 0x2112A442`, `stunBindingRequest uint16 = 0x0001`.

- [ ] **Step 1: Write the failing test**

`responder/detect_test.go`:
```go
package main

import (
	"encoding/binary"
	"testing"
)

func TestIsQUICVersion(t *testing.T) {
	ok := []uint32{0x00000001, 0x6b3343cf, 0xff000001, 0x0a0a0a0a, 0x1a2a3a4a}
	bad := []uint32{0x00000000, 0xff000000, 0x12345678}
	for _, v := range ok {
		if !isQUICVersion(v) {
			t.Errorf("0x%08x should be a QUIC version", v)
		}
	}
	for _, v := range bad {
		if isQUICVersion(v) {
			t.Errorf("0x%08x should NOT be a QUIC version", v)
		}
	}
}

func TestDetectQUIC(t *testing.T) {
	// long header (0xC0 bits) + v1 + dcid_len + scid_len.
	d := []byte{0xC3, 0, 0, 0, 1, 0x04, 1, 2, 3, 4, 0x03, 9, 9, 9}
	if !detectQUIC(d) {
		t.Fatal("well-formed QUIC Initial not detected")
	}
	// fixed bit clear -> not QUIC.
	bad := append([]byte{}, d...)
	bad[0] = 0x00
	if detectQUIC(bad) {
		t.Fatal("short-header/no-fixed-bit must not detect as QUIC")
	}
}

func TestDetectSTUN(t *testing.T) {
	d := make([]byte, 20)
	binary.BigEndian.PutUint16(d[0:], 0x0001) // Binding Request
	binary.BigEndian.PutUint16(d[2:], 0)      // length 0
	binary.BigEndian.PutUint32(d[4:], 0x2112A442)
	if !detectSTUN(d) {
		t.Fatal("STUN binding request not detected")
	}
	d[4] = 0 // break magic cookie
	if detectSTUN(d) {
		t.Fatal("bad magic cookie must not detect as STUN")
	}
}

func TestDetectDNS(t *testing.T) {
	// txid, flags=0x0100 (RD), qd=1, then qname "a" + root, QTYPE A, QCLASS IN.
	d := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
	if !detectDNS(d) {
		t.Fatal("valid DNS query not detected")
	}
	// QR=1 (response) must be rejected.
	bad := append([]byte{}, d...)
	bad[2] = 0x80
	if detectDNS(bad) {
		t.Fatal("DNS response (QR=1) must not detect as query")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd responder && go test -run "TestIsQUIC|TestDetect" ./...`
Expected: FAIL — `undefined: isQUICVersion` (etc.).

- [ ] **Step 3: Implement the detectors**

`responder/detect.go`:
```go
package main

import "encoding/binary"

const (
	stunMagicCookie    uint32 = 0x2112A442
	stunBindingRequest uint16 = 0x0001
)

// isQUICVersion matches QUIC v1 (RFC 9000), v2 (RFC 9369), IETF drafts
// (0xff0000xx, xx != 0), and GREASE values (0x?a?a?a?a). It deliberately does
// not treat 0 as a version (0 is the Version-Negotiation marker).
func isQUICVersion(v uint32) bool {
	switch {
	case v == 0x00000001:
		return true
	case v == 0x6b3343cf:
		return true
	case v&0xffffff00 == 0xff000000 && v&0xff != 0:
		return true
	case v&0x0f0f0f0f == 0x0a0a0a0a:
		return true
	default:
		return false
	}
}

// detectQUIC reports whether data looks like a QUIC long-header Initial.
func detectQUIC(data []byte) bool {
	if len(data) < 7 {
		return false
	}
	if data[0]&0xC0 != 0xC0 { // long header + fixed bit
		return false
	}
	if !isQUICVersion(binary.BigEndian.Uint32(data[1:5])) {
		return false
	}
	dcidLen := int(data[5])
	if dcidLen > 20 {
		return false
	}
	scidOff := 6 + dcidLen
	if scidOff >= len(data) {
		return false
	}
	scidLen := int(data[scidOff])
	if scidLen > 20 {
		return false
	}
	return scidOff+1+scidLen <= len(data)
}

// detectSTUN reports whether data is a STUN Binding Request.
func detectSTUN(data []byte) bool {
	if len(data) < 20 {
		return false
	}
	if binary.BigEndian.Uint32(data[4:8]) != stunMagicCookie {
		return false
	}
	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != stunBindingRequest || msgType&0xC000 != 0 {
		return false
	}
	msgLen := binary.BigEndian.Uint16(data[2:4])
	if msgLen%4 != 0 {
		return false
	}
	return len(data) == 20+int(msgLen)
}

// dnsQnameEnd walks an uncompressed QNAME starting at start and returns the
// index just past the terminating root label, plus ok=false on any malformed
// label (compression pointer, label > 63, name > 255, truncation).
func dnsQnameEnd(data []byte, start int) (int, bool) {
	pos := start
	total := 0
	for {
		if pos >= len(data) {
			return 0, false
		}
		l := int(data[pos])
		if l&0xC0 != 0 { // compression/reserved bits not allowed
			return 0, false
		}
		if l == 0 {
			return pos + 1, true
		}
		if l > 63 {
			return 0, false
		}
		total += l + 1
		if total > 255 {
			return 0, false
		}
		pos += 1 + l
	}
}

// detectDNS reports whether data is a plausible uncompressed DNS query.
func detectDNS(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	// Not STUN (cookie at 4..8).
	if binary.BigEndian.Uint32(data[4:8]) == stunMagicCookie {
		return false
	}
	flags := binary.BigEndian.Uint16(data[2:4])
	if flags&0xF800 != 0 { // QR + Opcode must be 0 (standard query)
		return false
	}
	if binary.BigEndian.Uint16(data[4:6]) != 1 { // exactly one question
		return false
	}
	end, ok := dnsQnameEnd(data, 12)
	if !ok || end+4 > len(data) {
		return false
	}
	qclass := binary.BigEndian.Uint16(data[end+2 : end+4])
	switch qclass {
	case 1, 3, 4, 255: // IN, CH, HS, ANY
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd responder && go test ./...`
Expected: PASS.

- [ ] **Step 5: Vet, then commit**

```bash
cd responder && go vet ./... && cd ..
git add responder/detect.go responder/detect_test.go
git commit -m "feat(responder): QUIC/DNS/STUN wire discriminators

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: DNS SERVFAIL builder

**Files:**
- Create: `responder/dns.go`
- Test: `responder/dns_test.go`

**Interfaces:**
- Consumes: `dnsQnameEnd` (Task 3).
- Produces: `func buildDNSServfail(incoming []byte) []byte` — a DNS response echoing the transaction ID, flags `0x8082` (QR=1, RA=1, RCODE=2/SERVFAIL) with the RD bit copied from the query, and echoing the question section when present and within the 512-byte cap (then QDCOUNT=1, else QDCOUNT=0).

- [ ] **Step 1: Write the failing test**

`responder/dns_test.go`:
```go
package main

import (
	"encoding/binary"
	"testing"
)

func TestBuildDNSServfailEchoesQuestion(t *testing.T) {
	q := []byte{
		0xAB, 0xCD, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x03, 'w', 'w', 'w', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
	r := buildDNSServfail(q)
	if binary.BigEndian.Uint16(r[0:2]) != 0xABCD {
		t.Error("txid not echoed")
	}
	flags := binary.BigEndian.Uint16(r[2:4])
	if flags&0x8000 == 0 || flags&0x000F != 2 || flags&0x0080 == 0 {
		t.Errorf("flags wrong: 0x%04x (want QR=1, RA=1, RCODE=2)", flags)
	}
	if flags&0x0100 == 0 {
		t.Error("RD bit should be copied from query")
	}
	if binary.BigEndian.Uint16(r[4:6]) != 1 {
		t.Error("QDCOUNT should be 1 when question echoed")
	}
	// Question bytes echoed verbatim.
	if string(r[12:]) != string(q[12:]) {
		t.Error("question section not echoed verbatim")
	}
}

func TestBuildDNSServfailHeaderOnly(t *testing.T) {
	// Header-only input (12 bytes) -> QDCOUNT 0, no question.
	q := make([]byte, 12)
	q[0], q[1] = 0x00, 0x05
	r := buildDNSServfail(q)
	if len(r) != 12 {
		t.Errorf("want 12-byte header-only response, got %d", len(r))
	}
	if binary.BigEndian.Uint16(r[4:6]) != 0 {
		t.Error("QDCOUNT should be 0 with no question")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd responder && go test -run TestBuildDNS ./...`
Expected: FAIL — `undefined: buildDNSServfail`.

- [ ] **Step 3: Implement `buildDNSServfail`**

`responder/dns.go`:
```go
package main

import "encoding/binary"

const dnsMaxResponse = 512 // RFC 1035 §2.3.4

// buildDNSServfail builds a SERVFAIL response (RCODE=2) echoing the query's
// transaction ID, RD bit, and question section when present.
func buildDNSServfail(incoming []byte) []byte {
	resp := make([]byte, 12)

	// Transaction ID (echo, or 0 if too short).
	if len(incoming) >= 2 {
		copy(resp[0:2], incoming[0:2])
	}

	// Flags: QR=1, Opcode=0, AA=0, TC=0, RD=copy, RA=1, RCODE=2.
	flags := uint16(0x8082)
	if len(incoming) >= 4 {
		if binary.BigEndian.Uint16(incoming[2:4])&0x0100 != 0 {
			flags |= 0x0100
		}
	}
	binary.BigEndian.PutUint16(resp[2:4], flags)
	// QDCOUNT/ANCOUNT/NSCOUNT/ARCOUNT already zero.

	// Echo the question section if we can parse a valid uncompressed QNAME.
	if len(incoming) > 12 {
		if end, ok := dnsQnameEnd(incoming, 12); ok {
			questionEnd := end + 4 // + QTYPE + QCLASS
			if questionEnd <= len(incoming) && len(resp)+(questionEnd-12) <= dnsMaxResponse {
				resp = append(resp, incoming[12:questionEnd]...)
				binary.BigEndian.PutUint16(resp[4:6], 1) // QDCOUNT = 1
			}
		}
	}
	return resp
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd responder && go test ./...`
Expected: PASS.

- [ ] **Step 5: Vet, then commit**

```bash
cd responder && go vet ./... && cd ..
git add responder/dns.go responder/dns_test.go
git commit -m "feat(responder): DNS SERVFAIL builder

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: STUN Binding-Success builder (v4 + v6)

**Files:**
- Create: `responder/stun.go`
- Test: `responder/stun_test.go`

**Interfaces:**
- Consumes: `stunMagicCookie` (Task 3).
- Produces: `func buildSTUNBindingSuccess(incoming []byte, clientIP net.IP, clientPort uint16) []byte` — a Binding-Success-Response (`0x0101`) echoing the transaction ID with a single `XOR-MAPPED-ADDRESS` (`0x0020`) attribute. IPv4: family `0x01`, addr XORed with the magic cookie. IPv6: family `0x02`, addr XORed with `cookie(4) || txid(12)`. Port XORed with the high 16 bits of the magic cookie in both cases.

- [ ] **Step 1: Write the failing test**

`responder/stun_test.go`:
```go
package main

import (
	"encoding/binary"
	"net"
	"testing"
)

func stunReq(txid []byte) []byte {
	d := make([]byte, 20)
	binary.BigEndian.PutUint16(d[0:], 0x0001)
	binary.BigEndian.PutUint32(d[4:], 0x2112A442)
	copy(d[8:20], txid)
	return d
}

func TestBuildSTUNv4(t *testing.T) {
	txid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	r := buildSTUNBindingSuccess(stunReq(txid), net.IPv4(203, 0, 113, 5), 40000)

	if binary.BigEndian.Uint16(r[0:2]) != 0x0101 {
		t.Error("not a binding-success response")
	}
	if binary.BigEndian.Uint16(r[2:4]) != 12 { // attr header 4 + value 8
		t.Errorf("message length wrong: %d", binary.BigEndian.Uint16(r[2:4]))
	}
	if string(r[8:20]) != string(txid) {
		t.Error("txid not echoed")
	}
	if binary.BigEndian.Uint16(r[20:22]) != 0x0020 { // XOR-MAPPED-ADDRESS
		t.Error("attr type not XOR-MAPPED-ADDRESS")
	}
	if r[25] != 0x01 { // family IPv4
		t.Errorf("family wrong: 0x%02x", r[25])
	}
	// XOR-decode and confirm round-trip.
	gotPort := binary.BigEndian.Uint16(r[26:28]) ^ uint16(0x2112)
	if gotPort != 40000 {
		t.Errorf("port decode wrong: %d", gotPort)
	}
	var key [4]byte
	binary.BigEndian.PutUint32(key[:], 0x2112A442)
	gotIP := net.IPv4(r[28]^key[0], r[29]^key[1], r[30]^key[2], r[31]^key[3])
	if !gotIP.Equal(net.IPv4(203, 0, 113, 5)) {
		t.Errorf("ip decode wrong: %v", gotIP)
	}
}

func TestBuildSTUNv6(t *testing.T) {
	txid := []byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2}
	ip := net.ParseIP("2001:db8::1")
	r := buildSTUNBindingSuccess(stunReq(txid), ip, 1234)
	if binary.BigEndian.Uint16(r[2:4]) != 24 { // attr header 4 + value 20
		t.Errorf("v6 message length wrong: %d", binary.BigEndian.Uint16(r[2:4]))
	}
	if r[25] != 0x02 {
		t.Errorf("v6 family wrong: 0x%02x", r[25])
	}
	// Reconstruct key = cookie(4) || txid(12) and decode.
	key := make([]byte, 16)
	binary.BigEndian.PutUint32(key[0:4], 0x2112A442)
	copy(key[4:16], txid)
	dec := make([]byte, 16)
	for i := 0; i < 16; i++ {
		dec[i] = r[28+i] ^ key[i]
	}
	if !net.IP(dec).Equal(ip) {
		t.Errorf("v6 ip decode wrong: %v", net.IP(dec))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd responder && go test -run TestBuildSTUN ./...`
Expected: FAIL — `undefined: buildSTUNBindingSuccess`.

- [ ] **Step 3: Implement `buildSTUNBindingSuccess`**

`responder/stun.go`:
```go
package main

import (
	"encoding/binary"
	"net"
)

const (
	stunBindingSuccess  uint16 = 0x0101
	stunAttrXorMapped   uint16 = 0x0020
)

// buildSTUNBindingSuccess builds a Binding-Success-Response with a single
// XOR-MAPPED-ADDRESS attribute for the observed client address.
func buildSTUNBindingSuccess(incoming []byte, clientIP net.IP, clientPort uint16) []byte {
	v4 := clientIP.To4()
	addrLen := 16
	family := byte(0x02)
	if v4 != nil {
		addrLen = 4
		family = 0x01
	}
	valueLen := 4 + addrLen      // reserved+family+port (4) + addr
	msgLen := 4 + valueLen       // attr header (type+len) + value

	resp := make([]byte, 20+4+valueLen)
	binary.BigEndian.PutUint16(resp[0:2], stunBindingSuccess)
	binary.BigEndian.PutUint16(resp[2:4], uint16(msgLen))
	binary.BigEndian.PutUint32(resp[4:8], stunMagicCookie)
	if len(incoming) >= 20 {
		copy(resp[8:20], incoming[8:20]) // echo transaction ID
	}

	// Attribute header.
	binary.BigEndian.PutUint16(resp[20:22], stunAttrXorMapped)
	binary.BigEndian.PutUint16(resp[22:24], uint16(valueLen))
	resp[24] = 0x00 // reserved
	resp[25] = family

	// XOR port with high 16 bits of the magic cookie.
	binary.BigEndian.PutUint16(resp[26:28], clientPort^uint16(stunMagicCookie>>16))

	// XOR address.
	if v4 != nil {
		var cookie [4]byte
		binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
		for i := 0; i < 4; i++ {
			resp[28+i] = v4[i] ^ cookie[i]
		}
	} else {
		key := make([]byte, 16)
		binary.BigEndian.PutUint32(key[0:4], stunMagicCookie)
		if len(incoming) >= 20 {
			copy(key[4:16], incoming[8:20]) // transaction ID
		}
		ip16 := clientIP.To16()
		for i := 0; i < 16; i++ {
			resp[28+i] = ip16[i] ^ key[i]
		}
	}
	return resp
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd responder && go test ./...`
Expected: PASS.

- [ ] **Step 5: Vet, then commit**

```bash
cd responder && go vet ./... && cd ..
git add responder/stun.go responder/stun_test.go
git commit -m "feat(responder): STUN Binding-Success builder (v4 + v6 XOR-MAPPED-ADDRESS)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: QUIC Version-Negotiation builder

**Files:**
- Create: `responder/quicvn.go`
- Test: `responder/quicvn_test.go`

**Interfaces:**
- Produces: `func buildQUICVersionNegotiation(incoming []byte) []byte` — a VN packet (RFC 9000 §17.2.1): first byte `incoming[0] | 0xC0`, version `0x00000000`, DCID = incoming **SCID**, SCID = incoming **DCID** (swapped), then one supported version = GREASE `0x0a0a0a0a`. **Never advertises `0x00000001`** (RFC 9000 §6.2 + fingerprint avoidance). On any CID parse failure both response CIDs are zero-length.

- [ ] **Step 1: Write the failing test**

`responder/quicvn_test.go`:
```go
package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildQUICVersionNegotiation(t *testing.T) {
	// dcid = {1,2,3,4} (len 4), scid = {9,9,9} (len 3).
	in := []byte{0xC3, 0, 0, 0, 1, 0x04, 1, 2, 3, 4, 0x03, 9, 9, 9}
	r := buildQUICVersionNegotiation(in)

	if r[0]&0xC0 != 0xC0 {
		t.Error("long-header/fixed bit not set")
	}
	if binary.BigEndian.Uint32(r[1:5]) != 0 {
		t.Error("version field must be 0 (VN marker)")
	}
	// Response DCID = incoming SCID.
	if r[5] != 3 || !bytes.Equal(r[6:9], []byte{9, 9, 9}) {
		t.Errorf("response DCID should be incoming SCID, got len=%d %v", r[5], r[6:9])
	}
	// Response SCID = incoming DCID.
	if r[9] != 4 || !bytes.Equal(r[10:14], []byte{1, 2, 3, 4}) {
		t.Errorf("response SCID should be incoming DCID, got len=%d %v", r[9], r[10:14])
	}
	// Supported version = GREASE, never v1.
	sv := binary.BigEndian.Uint32(r[14:18])
	if sv != 0x0a0a0a0a {
		t.Errorf("supported version should be GREASE 0x0a0a0a0a, got 0x%08x", sv)
	}
	if sv == 0x00000001 {
		t.Fatal("MUST NOT advertise v1")
	}
}

func TestBuildQUICVNMalformed(t *testing.T) {
	// Truncated: claims dcid_len=20 but no bytes follow -> both CIDs zero-length.
	in := []byte{0xC0, 0, 0, 0, 1, 0x14}
	r := buildQUICVersionNegotiation(in)
	if r[5] != 0 { // response DCID length
		t.Errorf("malformed input should yield zero-length DCID, got %d", r[5])
	}
	if r[6] != 0 { // response SCID length
		t.Errorf("malformed input should yield zero-length SCID, got %d", r[6])
	}
	if binary.BigEndian.Uint32(r[7:11]) != 0x0a0a0a0a {
		t.Error("supported version should still be GREASE")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd responder && go test -run TestBuildQUIC ./...`
Expected: FAIL — `undefined: buildQUICVersionNegotiation`.

- [ ] **Step 3: Implement `buildQUICVersionNegotiation`**

`responder/quicvn.go`:
```go
package main

import "encoding/binary"

// quicGREASEVersion is advertised as the sole "supported" version. It signals
// "no version in common" without ever claiming v1 (RFC 9000 §6.2), which would
// both violate the spec and be a fingerprint.
const quicGREASEVersion uint32 = 0x0a0a0a0a

// buildQUICVersionNegotiation builds a Version-Negotiation packet that swaps the
// incoming DCID/SCID and advertises only a GREASE version.
func buildQUICVersionNegotiation(incoming []byte) []byte {
	var inDCID, inSCID []byte
	if len(incoming) >= 6 {
		dcidLen := int(incoming[5])
		dcidEnd := 6 + dcidLen
		if dcidLen <= 20 && len(incoming) > dcidEnd {
			scidLen := int(incoming[dcidEnd])
			scidEnd := dcidEnd + 1 + scidLen
			if scidLen <= 20 && len(incoming) >= scidEnd {
				inDCID = incoming[6:dcidEnd]
				inSCID = incoming[dcidEnd+1 : scidEnd]
			}
		}
	}

	first := byte(0xC0)
	if len(incoming) > 0 {
		first = incoming[0] | 0xC0
	}

	resp := make([]byte, 0, 7+len(inDCID)+len(inSCID)+4)
	resp = append(resp, first)
	resp = append(resp, 0, 0, 0, 0)          // version = 0 (VN marker)
	resp = append(resp, byte(len(inSCID)))   // response DCID len = incoming SCID len
	resp = append(resp, inSCID...)           // response DCID = incoming SCID
	resp = append(resp, byte(len(inDCID)))   // response SCID len = incoming DCID len
	resp = append(resp, inDCID...)           // response SCID = incoming DCID
	var gv [4]byte
	binary.BigEndian.PutUint32(gv[:], quicGREASEVersion)
	resp = append(resp, gv[:]...)
	return resp
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd responder && go test ./...`
Expected: PASS.

- [ ] **Step 5: Vet, then commit**

```bash
cd responder && go vet ./... && cd ..
git add responder/quicvn.go responder/quicvn_test.go
git commit -m "feat(responder): QUIC Version-Negotiation builder (GREASE, never v1)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Raw-socket egress (checksums + v4/v6 senders)

**Files:**
- Create: `responder/egress.go`
- Test: `responder/egress_test.go`

**Interfaces:**
- Produces (pure, tested): `func onesComplementChecksum(b []byte) uint16`, `func udpChecksum(src, dst net.IP, udp []byte) uint16`, `func buildIPv4UDP(src, dst net.IP, sport, dport uint16, payload []byte) []byte`, `func buildUDPDatagram(src, dst net.IP, sport, dport uint16, payload []byte) []byte`.
- Produces (privileged, build-only): `func sendRawUDP(src, dst net.IP, sport, dport uint16, payload []byte) error` — injects a forged-source UDP reply. v4: `AF_INET/SOCK_RAW/IPPROTO_RAW` with a hand-built IP+UDP datagram (`IP_HDRINCL`). v6: `AF_INET6/SOCK_RAW/IPPROTO_UDP` bound to `src` so the kernel's source address matches the pseudo-header checksum we compute.
- Note: `udpChecksum` returns `0xffff` when the computed sum is zero (mandatory non-zero v6 checksum, Review R2-4); the UDP checksum field in `udp` must be zero before calling.

- [ ] **Step 1: Write the failing test**

`responder/egress_test.go`:
```go
package main

import (
	"encoding/binary"
	"net"
	"testing"
)

// foldedSum verifies the standard internet-checksum invariant: summing all
// 16-bit words of a buffer whose checksum field is already filled yields 0xffff.
func foldedSum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return uint16(sum)
}

func TestIPv4HeaderChecksumValid(t *testing.T) {
	pkt := buildIPv4UDP(net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 51820, 5000, []byte("hi"))
	// IHL words 0..20 must checksum to 0xffff.
	if got := foldedSum(pkt[0:20]); got != 0xffff {
		t.Errorf("IPv4 header checksum invalid: 0x%04x", got)
	}
	if pkt[9] != 17 { // protocol UDP
		t.Errorf("IP proto not UDP: %d", pkt[9])
	}
	if int(binary.BigEndian.Uint16(pkt[2:4])) != len(pkt) {
		t.Error("IP total length mismatch")
	}
}

func TestUDPChecksumNonZeroAndValidV6(t *testing.T) {
	src := net.ParseIP("2001:db8::1")
	dst := net.ParseIP("2001:db8::2")
	udp := buildUDPDatagram(src, dst, 51820, 5000, []byte("probe-reply"))
	// Recompute over pseudo-header + udp (with checksum in place) -> 0xffff.
	pseudo := make([]byte, 40)
	copy(pseudo[0:16], src.To16())
	copy(pseudo[16:32], dst.To16())
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(udp)))
	pseudo[39] = 17
	if got := foldedSum(append(pseudo, udp...)); got != 0xffff {
		t.Errorf("v6 UDP checksum invalid: 0x%04x", got)
	}
	if binary.BigEndian.Uint16(udp[6:8]) == 0 {
		t.Error("v6 UDP checksum must not be zero")
	}
	if binary.BigEndian.Uint16(udp[0:2]) != 51820 {
		t.Error("source port not forged to 51820")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd responder && go test -run "TestIPv4|TestUDPChecksum" ./...`
Expected: FAIL — `undefined: buildIPv4UDP`.

- [ ] **Step 3: Implement the egress functions**

`responder/egress.go`:
```go
package main

import (
	"encoding/binary"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// onesComplementChecksum computes the 16-bit one's-complement checksum.
func onesComplementChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// udpChecksum computes the UDP checksum over the IPv4/IPv6 pseudo-header. The
// udp slice's checksum field (bytes 6:8) must be zero on entry. A zero result is
// returned as 0xffff (mandatory for IPv6).
func udpChecksum(src, dst net.IP, udp []byte) uint16 {
	var pseudo []byte
	if v4 := src.To4(); v4 != nil {
		pseudo = make([]byte, 12)
		copy(pseudo[0:4], v4)
		copy(pseudo[4:8], dst.To4())
		pseudo[9] = 17 // UDP
		binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(udp)))
	} else {
		pseudo = make([]byte, 40)
		copy(pseudo[0:16], src.To16())
		copy(pseudo[16:32], dst.To16())
		binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(udp)))
		pseudo[39] = 17
	}
	cs := onesComplementChecksum(append(pseudo, udp...))
	if cs == 0 {
		return 0xffff
	}
	return cs
}

// buildUDPDatagram builds a UDP header + payload with a filled-in checksum.
func buildUDPDatagram(src, dst net.IP, sport, dport uint16, payload []byte) []byte {
	udp := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(udp[0:2], sport)
	binary.BigEndian.PutUint16(udp[2:4], dport)
	binary.BigEndian.PutUint16(udp[4:6], uint16(8+len(payload)))
	copy(udp[8:], payload)
	binary.BigEndian.PutUint16(udp[6:8], udpChecksum(src, dst, udp))
	return udp
}

// buildIPv4UDP builds a complete IPv4 packet (20-byte header, no options) + UDP.
func buildIPv4UDP(src, dst net.IP, sport, dport uint16, payload []byte) []byte {
	udp := buildUDPDatagram(src, dst, sport, dport, payload)
	total := 20 + len(udp)
	ip := make([]byte, 20)
	ip[0] = 0x45 // version 4, IHL 5
	binary.BigEndian.PutUint16(ip[2:4], uint16(total))
	ip[8] = 64 // TTL
	ip[9] = 17 // UDP
	copy(ip[12:16], src.To4())
	copy(ip[16:20], dst.To4())
	binary.BigEndian.PutUint16(ip[10:12], onesComplementChecksum(ip))
	return append(ip, udp...)
}

// sendRawUDP injects a forged-source UDP reply to dst. Requires CAP_NET_RAW.
func sendRawUDP(src, dst net.IP, sport, dport uint16, payload []byte) error {
	if dst.To4() != nil {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_RAW)
		if err != nil {
			return fmt.Errorf("v4 socket: %w", err)
		}
		defer unix.Close(fd)
		pkt := buildIPv4UDP(src, dst, sport, dport, payload)
		var sa unix.SockaddrInet4
		copy(sa.Addr[:], dst.To4())
		sa.Port = int(dport)
		return unix.Sendto(fd, pkt, 0, &sa)
	}

	// IPv6: SOCK_RAW/IPPROTO_UDP. Bind to src so the kernel's chosen source
	// address matches the pseudo-header we used for the (mandatory) checksum.
	fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_RAW, unix.IPPROTO_UDP)
	if err != nil {
		return fmt.Errorf("v6 socket: %w", err)
	}
	defer unix.Close(fd)
	var bsa unix.SockaddrInet6
	copy(bsa.Addr[:], src.To16())
	if err := unix.Bind(fd, &bsa); err != nil {
		return fmt.Errorf("v6 bind src: %w", err)
	}
	udp := buildUDPDatagram(src, dst, sport, dport, payload)
	var sa unix.SockaddrInet6
	copy(sa.Addr[:], dst.To16())
	sa.Port = int(dport)
	return unix.Sendto(fd, udp, 0, &sa)
}
```

- [ ] **Step 4: Add the dependency and run the tests**

```bash
cd responder
go get golang.org/x/sys/unix
go mod tidy
go test ./...
```
Expected: `go get`/`tidy` resolve `golang.org/x/sys`; tests PASS.

- [ ] **Step 5: Vet, then commit**

```bash
cd responder && go vet ./... && cd ..
git add responder/egress.go responder/egress_test.go responder/go.mod responder/go.sum
git commit -m "feat(responder): raw-socket egress with v4/v6 UDP checksums

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Decision logic, packet parsing, NFQUEUE loop + `main`

**Files:**
- Create: `responder/packet.go`, `responder/responder.go`, `responder/nfqueue.go`, `responder/main.go`
- Test: `responder/responder_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: `type Verdict int` (`VerdictAccept`, `VerdictDrop`); `type respKind int` (`respNone`, `respBytes`, `respSTUN`); `type Config struct { Params AwgParams; Protocol string }`.
- Produces: `func decide(payload []byte, cfg Config) (Verdict, respKind, []byte)` — the pure, addr-free classify→detect→respond decision. It never does I/O. For `respBytes` the third value is the ready-to-inject reply (DNS/QUIC-VN); for `respSTUN` the nfqueue loop builds the reply via `buildSTUNBindingSuccess` using the observed client addr (which `decide` doesn't have); for `respNone` there is no reply (ACCEPT).
- Produces: `type udpFlow` + `func parseL3UDP(pkt []byte) (udpFlow, bool)` (L3 → addrs/ports/payload); `func runQueue(ctx, queueNum, cfg) error` (the go-nfqueue loop).

- [ ] **Step 1: Write the failing test for `decide`**

`responder/responder_test.go`:
```go
package main

import (
	"encoding/binary"
	"testing"
)

func dnsQuery() []byte {
	return []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
}

func TestDecideRealAwgAccepted(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "dns"}
	// A real transport packet (classify wins even though protocol=dns).
	real := makeAwg(20, 750, 20+200)
	v, kind, _ := decide(real, cfg)
	if v != VerdictAccept || kind != respNone {
		t.Fatalf("real AWG must be ACCEPTed, got v=%v kind=%v", v, kind)
	}
}

func TestDecideDNSProbeAnswered(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "dns"}
	v, kind, resp := decide(dnsQuery(), cfg)
	if v != VerdictDrop || kind != respBytes {
		t.Fatalf("DNS probe must be answered+DROP, got v=%v kind=%v", v, kind)
	}
	if binary.BigEndian.Uint16(resp[2:4])&0x000F != 2 {
		t.Error("expected SERVFAIL response bytes")
	}
}

func TestDecideOtherProtocolIgnored(t *testing.T) {
	// Configured for STUN; a DNS probe must be ignored (ACCEPT, no reply).
	cfg := Config{Params: testParams, Protocol: "stun"}
	v, kind, _ := decide(dnsQuery(), cfg)
	if v != VerdictAccept || kind != respNone {
		t.Fatalf("off-protocol probe must be ACCEPTed, got v=%v kind=%v", v, kind)
	}
}

func TestDecideSipNeverAnswers(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "sip"}
	v, kind, _ := decide(dnsQuery(), cfg)
	if v != VerdictAccept || kind != respNone {
		t.Fatalf("sip must never answer, got v=%v kind=%v", v, kind)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd responder && go test -run TestDecide ./...`
Expected: FAIL — `undefined: decide`.

- [ ] **Step 3: Implement `decide`**

`responder/responder.go`:
```go
package main

// Verdict is the NFQUEUE disposition for a queued packet.
type Verdict int

const (
	VerdictAccept Verdict = iota
	VerdictDrop
)

// respKind tells the loop how to turn decide()'s result into a wire reply.
type respKind int

const (
	respNone  respKind = iota // no reply (ACCEPT)
	respBytes                 // reply is the returned bytes (DNS/QUIC-VN)
	respSTUN                  // loop builds the STUN reply using the client addr
)

// Config is the responder's startup configuration.
type Config struct {
	Params   AwgParams
	Protocol string // none|quic|dns|stun|sip
}

// decide runs the correctness-critical order: classify real AWG first, then —
// only for the configured protocol — detect a probe and choose a reply. It is
// pure: it never touches a socket and is addr-free (STUN's addr is applied by
// the caller via respSTUN).
func decide(payload []byte, cfg Config) (Verdict, respKind, []byte) {
	// 1. Genuine AWG handshake or transport -> ACCEPT (kernel fast path).
	if classifyAwgPacket(payload, cfg.Params) {
		return VerdictAccept, respNone, nil
	}
	// 2. Probe matching the configured protocol -> answer + DROP.
	switch cfg.Protocol {
	case "dns":
		if detectDNS(payload) {
			return VerdictDrop, respBytes, buildDNSServfail(payload)
		}
	case "stun":
		if detectSTUN(payload) {
			return VerdictDrop, respSTUN, nil
		}
	case "quic":
		if detectQUIC(payload) {
			return VerdictDrop, respBytes, buildQUICVersionNegotiation(payload)
		}
	// "sip": shaping only, never answered (Decision 8). "none": unreachable.
	}
	// 3. Genuine junk -> ACCEPT, let awg0 silently drop it.
	return VerdictAccept, respNone, nil
}
```

- [ ] **Step 4: Run the `decide` tests to verify they pass**

Run: `cd responder && go test -run TestDecide ./...`
Expected: PASS.

- [ ] **Step 5: Implement L3 packet parsing**

`responder/packet.go`:
```go
package main

import (
	"encoding/binary"
	"net"
)

// udpFlow holds the addressing extracted from an NFQUEUE L3 packet plus the
// UDP application payload.
type udpFlow struct {
	srcIP, dstIP     net.IP // srcIP = client, dstIP = us (WG_PORT side)
	srcPort, dstPort uint16
	payload          []byte
}

// parseL3UDP parses an IPv4 or IPv6 packet (as delivered by NFQUEUE) that
// carries UDP, returning the flow or ok=false if it is not parseable UDP.
func parseL3UDP(pkt []byte) (udpFlow, bool) {
	if len(pkt) < 1 {
		return udpFlow{}, false
	}
	switch pkt[0] >> 4 {
	case 4:
		if len(pkt) < 20 {
			return udpFlow{}, false
		}
		ihl := int(pkt[0]&0x0f) * 4
		if pkt[9] != 17 || len(pkt) < ihl+8 {
			return udpFlow{}, false
		}
		udp := pkt[ihl:]
		return udpFlow{
			srcIP:   net.IP(pkt[12:16]),
			dstIP:   net.IP(pkt[16:20]),
			srcPort: binary.BigEndian.Uint16(udp[0:2]),
			dstPort: binary.BigEndian.Uint16(udp[2:4]),
			payload: udp[8:],
		}, true
	case 6:
		if len(pkt) < 48 || pkt[6] != 17 { // 40-byte v6 header, Next Header = UDP
			return udpFlow{}, false
		}
		udp := pkt[40:]
		return udpFlow{
			srcIP:   net.IP(pkt[8:24]),
			dstIP:   net.IP(pkt[24:40]),
			srcPort: binary.BigEndian.Uint16(udp[0:2]),
			dstPort: binary.BigEndian.Uint16(udp[2:4]),
			payload: udp[8:],
		}, true
	default:
		return udpFlow{}, false
	}
}
```

- [ ] **Step 6: Implement the NFQUEUE loop**

`responder/nfqueue.go`:
```go
package main

import (
	"context"
	"log"

	nfqueue "github.com/florianl/go-nfqueue/v2"
)

// runQueue attaches to NFQUEUE queueNum and applies decide() to each packet,
// injecting replies via raw socket. It blocks until ctx is cancelled.
func runQueue(ctx context.Context, queueNum uint16, cfg Config) error {
	nf, err := nfqueue.Open(&nfqueue.Config{
		NfQueue:      queueNum,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  0xff,
		Copymode:     nfqueue.NfQnlCopyPacket,
	})
	if err != nil {
		return err
	}
	defer nf.Close()

	fn := func(a nfqueue.Attribute) int {
		if a.PacketID == nil {
			return 0
		}
		id := *a.PacketID
		if a.Payload == nil {
			_ = nf.SetVerdict(id, nfqueue.NfAccept)
			return 0
		}
		flow, ok := parseL3UDP(*a.Payload)
		if !ok {
			_ = nf.SetVerdict(id, nfqueue.NfAccept)
			return 0
		}
		verdict, kind, resp := decide(flow.payload, cfg)
		if verdict == VerdictDrop {
			switch kind {
			case respBytes:
				// reply from us (dstIP, WG_PORT) to the client (srcIP:srcPort).
				if err := sendRawUDP(flow.dstIP, flow.srcIP, flow.dstPort, flow.srcPort, resp); err != nil {
					log.Printf("responder: egress error: %v", err)
				}
			case respSTUN:
				reply := buildSTUNBindingSuccess(flow.payload, flow.srcIP, flow.srcPort)
				if err := sendRawUDP(flow.dstIP, flow.srcIP, flow.dstPort, flow.srcPort, reply); err != nil {
					log.Printf("responder: stun egress error: %v", err)
				}
			}
			_ = nf.SetVerdict(id, nfqueue.NfDrop)
			return 0
		}
		_ = nf.SetVerdict(id, nfqueue.NfAccept)
		return 0
	}
	errFn := func(e error) int {
		log.Printf("responder: nfqueue error: %v", e)
		return 0
	}
	if err := nf.RegisterWithErrorFunc(ctx, fn, errFn); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
```

- [ ] **Step 7: Implement `main`**

`responder/main.go`:
```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	proto := strings.ToLower(os.Getenv("IMITATE_PROTOCOL"))
	if proto == "" {
		proto = "none"
	}
	if proto == "none" {
		log.Fatal("responder: IMITATE_PROTOCOL=none — nothing to answer; exiting")
	}

	wgPath := os.Getenv("WG_PATH")
	if wgPath == "" {
		wgPath = "/etc/amnezia/amneziawg/"
	}
	params, err := ParseConfig(filepath.Join(wgPath, "wg0.conf"))
	if err != nil {
		log.Fatalf("responder: reading S/H params: %v", err)
	}

	queueNum := uint16(0)
	if q := os.Getenv("RESPONDER_QUEUE"); q != "" {
		n, err := strconv.ParseUint(q, 10, 16)
		if err != nil {
			log.Fatalf("responder: bad RESPONDER_QUEUE: %v", err)
		}
		queueNum = uint16(n)
	}

	cfg := Config{Params: params, Protocol: proto}
	if proto == "sip" {
		log.Println("responder: IMITATE_PROTOCOL=sip — shaping only; SIP probes are NOT answered")
	}
	log.Printf("responder: protocol=%s queue=%d — answering probes on WG_PORT", proto, queueNum)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runQueue(ctx, queueNum, cfg); err != nil {
		log.Fatalf("responder: queue: %v", err)
	}
}
```

- [ ] **Step 8: Resolve deps, build, vet, full test**

```bash
cd responder
go get github.com/florianl/go-nfqueue/v2
go mod tidy
go build ./...
go vet ./...
go test ./...
```
Expected: dependency resolves; `go build` produces no errors; vet clean; all unit tests PASS. (`runQueue`/`sendRawUDP`/`main` are compiled but not unit-run — they need NFQUEUE + `CAP_NET_RAW`, exercised in Task 9/10 manual tests.)

- [ ] **Step 9: Commit**

```bash
git add responder/packet.go responder/responder.go responder/nfqueue.go responder/main.go responder/responder_test.go responder/go.mod responder/go.sum
git commit -m "feat(responder): decision logic, L3 parsing, NFQUEUE loop and main

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Dockerfile responder build stage + supervising entrypoint

**Files:**
- Modify: `Dockerfile`
- Create: `docker-entrypoint.sh`

**Interfaces:**
- Consumes: the `responder/` module (Tasks 1–8), the Plan-1 runtime image.
- Produces: a runtime image carrying `/usr/bin/awg-responder` and `/usr/bin/docker-entrypoint.sh`; the entrypoint launches the Node UI/tunnel and, when `RESPONDER=true`, inserts the NEW-only NFQUEUE rule (`-I INPUT 1 ... --queue-bypass`) and launches the responder as a crash-isolated side process, tearing the rule down on responder exit.

- [ ] **Step 1: Add the responder build stage to `Dockerfile`**

Insert this stage after the `build_awg_tools` stage (before `# ── Runtime ──`):
```dockerfile
# ── Go probe-responder ──
FROM golang:1.24-alpine AS build_responder
RUN apk add --no-cache linux-headers
COPY responder /src
WORKDIR /src
RUN go build -o /awg-responder .
# binary: /awg-responder
```

- [ ] **Step 2: Copy the binary + entrypoint into the runtime stage**

In the `# ── Runtime ──` stage of `Dockerfile`:

Add `conntrack-tools` to the runtime `apk add` line so the `conntrack` ctstate match is available (iptables `conntrack` module is in-kernel, but install the userspace tools for diagnostics):
```dockerfile
RUN apk add --no-cache nodejs npm bash iproute2 iptables conntrack-tools dumb-init
```

After the existing `COPY --from=build_node_modules ...` lines, add:
```dockerfile
COPY --from=build_responder /awg-responder /usr/bin/awg-responder
COPY docker-entrypoint.sh /usr/bin/docker-entrypoint.sh
RUN chmod +x /usr/bin/awg-responder /usr/bin/docker-entrypoint.sh
```

Change the final `CMD` from the direct node invocation to the entrypoint:
```dockerfile
CMD ["/usr/bin/dumb-init", "/usr/bin/docker-entrypoint.sh"]
```
(Leave the `HEALTHCHECK` line unchanged.)

- [ ] **Step 3: Write the entrypoint**

`docker-entrypoint.sh`:
```sh
#!/bin/sh
# Supervises the Node UI/tunnel and the optional Go probe-responder.
# Node is the primary process (its exit ends the container); the responder is a
# crash-isolated side filter whose death never affects connectivity.
set -e

WG_PORT="${WG_PORT:-51820}"
QUEUE_NUM="${RESPONDER_QUEUE:-0}"

insert_nfqueue_rule() {
  # Insert at the HEAD of INPUT so it precedes the PostUp ACCEPT that the Node
  # app appends (src/config.js). NEW-only: established flows bypass userspace.
  # --queue-bypass: if no process is attached to the queue, ACCEPT (fail open).
  iptables -I INPUT 1 -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass
}

remove_nfqueue_rule() {
  iptables -D INPUT -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass 2>/dev/null || true
}

run_responder() {
  # Wait for the datapath to come up (wg0 present) before touching netfilter.
  i=0
  while [ "$i" -lt 30 ]; do
    if ip link show wg0 >/dev/null 2>&1; then
      break
    fi
    i=$((i + 1))
    sleep 1
  done

  insert_nfqueue_rule
  echo "responder: NFQUEUE rule installed on udp/${WG_PORT} (queue ${QUEUE_NUM})"
  /usr/bin/awg-responder &
  RESP_PID=$!
  # On responder exit, remove the rule so traffic falls through to awg0.
  wait "${RESP_PID}" || true
  echo "responder: exited; removing NFQUEUE rule (active-probe defense off, tunnel unaffected)"
  remove_nfqueue_rule
}

if [ "${RESPONDER:-false}" = "true" ]; then
  # Validate synchronously (fail-fast) BEFORE launching anything.
  case "${IMITATE_PROTOCOL:-none}" in
    none)
      echo "ERROR: RESPONDER=true requires IMITATE_PROTOCOL != none" >&2
      exit 1
      ;;
    sip)
      echo "WARN: IMITATE_PROTOCOL=sip is shaping-only; SIP probes are NOT answered." >&2
      echo "WARN: sip + RESPONDER=true is the least-protected setting (silence is a fingerprint)." >&2
      ;;
  esac
  run_responder &
fi

# Node brings up the tunnel and serves the UI; it is the primary process.
exec node server.js
```

- [ ] **Step 4: Build the image**

Run: `docker build --tag amnezia-wg-easy:plan2 .`
Expected: build succeeds through all stages, including `build_responder` (the Go binary compiles in-image) and the runtime `COPY` of the binary + entrypoint.

- [ ] **Step 5: Verify the binary and entrypoint are present and the responder guard works**

```bash
# Binary present and runnable.
docker run --rm amnezia-wg-easy:plan2 sh -c "test -x /usr/bin/awg-responder && test -x /usr/bin/docker-entrypoint.sh && echo 'artifacts ok'"
# Guard: RESPONDER=true + IMITATE_PROTOCOL=none must error out before launching node.
docker run --rm -e RESPONDER=true -e IMITATE_PROTOCOL=none amnezia-wg-easy:plan2 2>&1 \
  | grep -q "requires IMITATE_PROTOCOL != none" && echo 'guard ok'
```
Expected: `artifacts ok` and `guard ok`. The entrypoint validates synchronously and `exit 1`s before `exec node server.js`, so the container fails fast with the error on stderr.

- [ ] **Step 6: Commit**

```bash
git add Dockerfile docker-entrypoint.sh
git commit -m "build: responder Go build stage + supervising entrypoint with NFQUEUE rule

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Compose / env / README + manual test matrix

**Files:**
- Modify: `docker-compose.yml`, `.env.example`, `README.md`

**Interfaces:**
- Consumes: the `amnezia-wg-easy:plan2` image and the `RESPONDER` toggle.

- [ ] **Step 1: Add `RESPONDER` env + `NET_RAW` cap to `docker-compose.yml`**

In `docker-compose.yml`, add `RESPONDER` to `environment` directly under the `IMITATE_PROTOCOL` line:
```yaml
      # Native obfuscation imitation (none|quic|dns|stun|sip):
      - IMITATE_PROTOCOL=${IMITATE_PROTOCOL:-none}
      # Active-probe responder (answers DNS/STUN/QUIC-VN scanners). Off by
      # default. Requires IMITATE_PROTOCOL != none and CAP_NET_RAW.
      - RESPONDER=${RESPONDER:-false}
```

And add `NET_RAW` to `cap_add`:
```yaml
    cap_add:
      - NET_ADMIN
      - NET_RAW
```

- [ ] **Step 2: Document `RESPONDER` in `.env.example`**

In `.env.example`, append after the `IMITATE_PROTOCOL` block:
```bash
# ── Active-probe responder ────────────────────────────────────────
# When true, answers active DPI probes on WG_PORT with protocol-valid
# replies (DNS SERVFAIL / STUN Binding-Success / QUIC Version-Negotiation),
# matching IMITATE_PROTOCOL. Off by default. Requires IMITATE_PROTOCOL != none
# and the NET_RAW capability (already set in docker-compose.yml).
# NOTE: IMITATE_PROTOCOL=sip shapes traffic but does NOT answer SIP probes yet
# (shaping-only — the least-protected setting when probing is a concern).
RESPONDER=false
```

- [ ] **Step 3: Add the README section**

In `README.md`, after the "Native traffic imitation" section (added in Plan 1), add:
```markdown
### Active-probe responder (`RESPONDER`)

A patched datapath silently drops unauthenticated packets — and that silence is
itself a fingerprint. With `RESPONDER=true`, the container runs a Go side-filter
on `WG_PORT` that answers active DPI probes with protocol-valid replies, matching
`IMITATE_PROTOCOL`:

| `IMITATE_PROTOCOL` | Probe answered with |
|--------------------|---------------------|
| `dns`  | DNS `SERVFAIL` echoing the query's question |
| `stun` | STUN Binding-Success with `XOR-MAPPED-ADDRESS` |
| `quic` | QUIC Version-Negotiation (GREASE; never claims v1) |
| `sip`  | **Nothing** — SIP is shaping-only for now (least-protected setting) |

Only first-contact packets (conntrack `NEW`) reach the responder; established
tunnels and bulk throughput stay entirely in the kernel. The responder is
crash-isolated: if it dies, the tunnel keeps serving and only active-probe
defense is lost.

**Requirements:** `IMITATE_PROTOCOL != none`, and the `NET_ADMIN` + `NET_RAW`
capabilities (both set in `docker-compose.yml`). The host needs the
`nfnetlink_queue` and `nf_conntrack` modules (standard on mainstream distros).

> The current QUIC answer is Version-Negotiation only; the full TLS-1.3 handshake
> continuation is a later phase.
```

- [ ] **Step 4: Validate compose parses**

Run: `RESPONDER=true IMITATE_PROTOCOL=quic docker compose config >/dev/null && echo 'compose ok'`
Expected: `compose ok` (env interpolation + `NET_RAW` cap parse).

- [ ] **Step 5: Manual integration test matrix**

These require a host with the AWG kernel module (or the go fallback), `nfnetlink_queue`, and a way to send probes. Record results; they are the acceptance gate (no automated equivalent exists).

1. **Regression (`RESPONDER=false`):** `IMITATE_PROTOCOL=none RESPONDER=false` → a client connects on `WG_PORT`, UI on `PORT`, traffic flows; `iptables -S INPUT` shows **no** NFQUEUE rule.
2. **Rule installed:** `IMITATE_PROTOCOL=quic RESPONDER=true` → `iptables -S INPUT | head -1` shows the `NFQUEUE --queue-num 0 --queue-bypass` rule at position 1 (ahead of the PostUp ACCEPT); responder log prints `protocol=quic queue=0`.
3. **QUIC VN probe:** send a QUIC Initial with an **unsupported** version to `WG_HOST:WG_PORT` → tcpdump shows a Version-Negotiation reply (`sport=WG_PORT`) advertising GREASE `0x0a0a0a0a`, never `0x00000001`.
4. **DNS probe (`IMITATE_PROTOCOL=dns`):** `dig @WG_HOST -p WG_PORT example.com` → a `SERVFAIL` echoing the question; a STUN/QUIC probe is **ignored**.
5. **STUN probe (`IMITATE_PROTOCOL=stun`):** send a Binding-Request → Binding-Success with `XOR-MAPPED-ADDRESS` decoding to the client's observed `addr:port`.
6. **Real client wins over detection (adversarial):** with `IMITATE_PROTOCOL=dns`, a **real** client still connects — its shaped handshake is ACCEPTed by `classifyAwgPacket` before the DNS arm runs.
7. **Fast path:** `iperf` over an established tunnel → responder CPU stays ~0 (ESTABLISHED bypasses userspace). After the conntrack UDP idle-timeout, a mid-stream transport packet re-enters as `NEW` and is still ACCEPTed (transport classify, Review F6).
8. **Crash isolation:** `kill` the `awg-responder` process → established tunnels keep flowing, new clients still connect (rule torn down via the entrypoint's `wait`), only active-probe defense is lost.
9. **SIP warning:** `IMITATE_PROTOCOL=sip RESPONDER=true` → entrypoint logs the shaping-only warning; a SIP probe is not answered; the tunnel still works.

- [ ] **Step 6: Commit**

```bash
git add docker-compose.yml .env.example README.md
git commit -m "feat: wire RESPONDER toggle into compose/env/README

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Notes for Plan 3 (do not implement here)

- **R2-1 connmark-claim prototype FIRST.** Set the conntrack mark on the entry via the nfqueue verdict's CT facility (`go-nfqueue` `NFQA_CT`) or `libnetfilter_conntrack` — **not** an iptables `--save-mark` rule (a DROPped probe is never confirmed, and the raw reply confirms the reverse tuple, flipping the prober's 2nd packet to `ESTABLISHED`→`awg0` stall). Masked bit `0x1/0x1`, disjoint from any `awg-quick`/`wg0.conf` fwmark (R2-2). Add the second iptables rule (`-m connmark --mark 0x1/0x1 -j NFQUEUE`) ahead of the NEW rule, and a per-flow idle TTL that evicts abandoned probe state + clears the mark. Gate everything below on this prototype passing.
- **Full QUIC handshake continuation** via embedded `quic-go` (pinned) over a custom `net.PacketConn` (`ReadFrom` from the queue, `WriteTo` → the Task-7 raw injector), with a `tls.Config.GetCertificate` dynamic SNI resolver minting per-ClientHello self-signed certs. New env `QUIC_HANDSHAKE` (default true) + `QUIC_CERT_DOMAIN` (default `cloudflare.com`); VN (this plan) is retained for the unsupported-version case. Self-signed cert remains a known weaker fingerprint (R2-6).
```
