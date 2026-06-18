# QUIC TLS-1.3 Handshake Responder — Implementation Plan (Plan 3 of 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the Go probe-responder's QUIC arm from Version-Negotiation-only (Plan 2) to a full **TLS-1.3 handshake continuation** using an embedded `quic-go` server endpoint, made multi-RTT-safe under NFQUEUE by a **conntrack-mark flow-claim** set on the verdict (Review R2-1) — proven first by a standalone prototype that gates the rest of the plan.

**Architecture:** A well-formed QUIC **v1** Initial (probe) is fed to an embedded `quic-go` server over a custom `net.PacketConn` whose `WriteTo` injects via the existing Plan-2 raw socket (`sport=WG_PORT`). The server emits a real `ServerHello`/Certificate flight with a per-SNI self-signed cert (`tls.Config.GetCertificate` resolver). To survive the multi-RTT handshake despite "ESTABLISHED bypasses userspace", the responder **sets the conntrack mark `0x1` on the verdict** (`go-nfqueue` `SetVerdictWithConnMark` → `NFQA_CT`/`CTA_MARK`) and **ACCEPTs** the claimed packet (not DROP — see Global Constraints), so the now-confirmed flow's later packets re-enter userspace via a second iptables rule (`-m connmark --mark 0x1/0x1`). Unsupported-version Initials still get the Plan-2 GREASE Version-Negotiation (single-shot DROP). DNS/STUN/SIP are unchanged.

**Tech Stack:** Go 1.25 (module `awg-responder`, package `main`), `github.com/quic-go/quic-go` **v0.60.0** (pinned; requires go 1.25.0), `crypto/tls` + `crypto/x509` + `crypto/ecdsa` (self-signed certs), `github.com/florianl/go-nfqueue/v2` v2.0.4 (`SetVerdictWithConnMark`), the Plan-2 raw-socket egress, POSIX `sh` entrypoint, `iptables` connmark match.

## Global Constraints

- **GATE: the connmark prototype (Task 1) must pass before any of Tasks 2–7 is merged.** Review R2-1 flagged the claim mechanism as "broken as first drawn" (iptables `--save-mark`). Task 1 proves the *correct* mechanism empirically on a real Linux host. Do not build the QUIC handshake on an unproven claim. (Same class of manual, host-required gate as Plan 2's test matrix — there is no automated nfqueue/conntrack equivalent.)
- **ACCEPT-with-connmark, NOT DROP, for a claimed QUIC flow (correctness, supersedes the design's literal step-2 "DROP").** A `NEW` packet's conntrack entry is *unconfirmed* during the nfqueue verdict; it is only persisted by `nf_conntrack_confirm()` at the end of hook traversal, which runs **only on ACCEPT**. A DROP frees the skb and destroys the unconfirmed entry — the mark set via `NFQA_CT` is lost, exactly the R2-1 failure. So the claimed packet is **ACCEPTed** (carrying mark `0x1`): the entry confirms with the mark, and the packet is delivered to the port-bound wg socket (kernel module or `amneziawg-go`) which silently discards it as non-AWG (no ICMP port-unreachable, because the port *is* bound). The real QUIC server flight is injected separately via the raw socket. DNS/STUN/VN remain single-shot **DROP** (no claim needed).
- **Claim mechanism = `nf.SetVerdictWithConnMark(id, nfqueue.NfAccept, connMarkClaim)`** (verified present in `go-nfqueue/v2` v2.0.4: nests `CTA_MARK` under `NFQA_CT` in the verdict message). **No cgo, no `libnetfilter_conntrack`, no `conntrack` CLI.** `connMarkClaim = 0x1`. Clearing uses `...WithConnMark(id, NfAccept, 0x0)`.
- **The conntrack mark is ours alone.** `awg-quick`/`wg0.conf` use the **packet fwmark** (`FwMark`/`Table` → `ip rule fwmark` policy routing) — a *different* field from the conntrack `mark`. Nothing else in this stack writes the conntrack mark, so setting it whole to `0x1` cannot collide with the datapath's routing fwmark (Review R2-2 satisfied by field-disjointness). The iptables match still uses the masked form `--mark 0x1/0x1` defensively (only tests bit 0).
- **Verdict order is correctness-critical (unchanged from Plan 2):** `classifyAwgPacket` runs **first**, before any protocol detection, because client→server shaping can make a real handshake-init resemble the answered protocol. Must also classify **transport** packets (S4/H4), so a mid-stream packet re-entering as `NEW` after conntrack idle-timeout is recognized as real (Review F6). The QUIC handshake branch lives strictly *after* `classifyAwgPacket`.
- **Responder answers only as `IMITATE_PROTOCOL`.** The handshake is reachable only when `IMITATE_PROTOCOL=quic`. The full handshake applies only to **v1** (`0x00000001`) Initials; any other (still QUIC-shaped) version gets the Plan-2 GREASE Version-Negotiation, never quic-go's own VN (which would fingerprint quic-go's supported-version list).
- **`QUIC_HANDSHAKE` (default `true`)** gates the full handshake. `false` ⇒ VN-only (exact Plan-2 behavior; no quic-go endpoint, no connmark rule). **`QUIC_CERT_DOMAIN` (default `cloudflare.com`)** is the SNI/cert domain for the default self-signed cert; must be non-empty when `QUIC_HANDSHAKE=true`. Both are only meaningful with `IMITATE_PROTOCOL=quic` + `RESPONDER=true`.
- **Self-signed cert is a known residual fingerprint (Review R2-6, inherited from the Rust original).** A prober that validates the chain sees a self-signed cert for `QUIC_CERT_DOMAIN`. Out of scope to fix; documented, not solved.
- **`quic-go` runs over a custom `net.PacketConn` (Review R2-3):** `quic.Transport{Conn: pc}` — `ReadFrom` returns queued probe packets `(n, clientAddr)`, `WriteTo` routes to the raw injector, `tls.Config.GetCertificate` is the SNI hook. quic-go logs a one-time warning when the conn isn't `OOBCapablePacketConn` (no ECN/GSO) and degrades cleanly — harmless. **Pin quic-go to v0.60.0** (it requires `go 1.25.0`, matching `responder/go.mod` and the `golang:1.25-alpine` build stage).
- **All multi-byte protocol fields are big-endian** (QUIC version, DNS/STUN). The AWG obfuscated 4-byte header is little-endian. Do not mix these up.
- **Pure functions get real `go test` cycles; the nfqueue/connmark loop, raw-socket send, entrypoint, and Dockerfile are build- and manually-verified** (project norm — no automated suite; `CLAUDE.md`). The embedded handshake additionally gets an **automated loopback integration test** (a quic-go client dials our server over an in-memory `net.PacketConn` pair — no root, no nfqueue), which proves the endpoint + cert resolver complete a real TLS-1.3 handshake. Run: `cd responder && go test ./...`.
- **Commits:** conventional-commit prefixes; end every commit message with the trailer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

---

## File Structure

New code in `responder/` (module `awg-responder`, package `main`), split by responsibility:

- `responder/cmd/markspike/main.go` — **prototype only** (Task 1). A standalone `package main` that attaches to a test NFQUEUE and `SetVerdictWithConnMark(id, ACCEPT, 0x1)`s UDP packets, for proving the claim persists. Builds with `go build ./cmd/markspike`; never shipped in the image.
- `responder/quiccert.go` — `certResolver`: per-SNI self-signed cert cache + `getCertificate(*tls.ClientHelloInfo)`.
- `responder/quicconn.go` — `packetConn`: a `net.PacketConn` whose `ReadFrom` drains an inbound queue and `WriteTo` calls a pluggable injector (raw socket in prod, in-memory in tests).
- `responder/quichs.go` — `quicManager`: owns a `quic.Transport`/`quic.Listener` over the `packetConn`, an accept-and-close goroutine, and the per-client `serverIP` map for the injector.
- `responder/*_test.go` — `quiccert_test.go`, `quicconn_test.go`, `quichs_test.go` (loopback handshake).

Modified in `responder/`:
- `responder/go.mod` / `responder/go.sum` — add `github.com/quic-go/quic-go v0.60.0` (+ its transitive deps).
- `responder/responder.go` — `Config` gains `QUICHandshake bool`, `CertDomain string`, `WGPort uint16`; `decide` gains the v1-vs-VN QUIC branch + new `respQUICClaim` kind.
- `responder/nfqueue.go` — construct the `quicManager` when applicable; handle `respQUICClaim` via `mgr.handle(...)` + `SetVerdictWithConnMark`.
- `responder/main.go` — parse `QUIC_HANDSHAKE`, `QUIC_CERT_DOMAIN`, `WG_PORT` into `Config`.

Modified outside `responder/`:
- `docker-entrypoint.sh` — when QUIC handshake is active, also insert/remove the `-m connmark --mark 0x1/0x1` NFQUEUE rule (ahead of the NEW rule); symmetric teardown.
- `Dockerfile` — drop the now-confirmed-unused `conntrack-tools` from the runtime apk line (Plan-2 carry-forward).
- `docker-compose.yml`, `.env.example`, `README.md` — document `QUIC_HANDSHAKE` + `QUIC_CERT_DOMAIN`; update the README QUIC row from "VN only" to the full handshake.

---

## Task 1: Connmark-claim prototype (the GATE)

**Goal:** Prove on a real Linux host that `SetVerdictWithConnMark(id, ACCEPT, 0x1)` persists a conntrack mark that a second packet then matches via `-m connmark --mark 0x1/0x1`, and that DROP does **not** persist it. This validates Review R2-1's fix before any QUIC code is written.

**Files:**
- Create: `responder/cmd/markspike/main.go`

**Interfaces:**
- Consumes: `github.com/florianl/go-nfqueue/v2` (already in `go.mod`).
- Produces: a throwaway binary; nothing depends on it.

- [ ] **Step 1: Write the prototype**

`responder/cmd/markspike/main.go`:
```go
// Command markspike is a throwaway prototype (Plan 3, Task 1) that proves the
// conntrack-mark flow-claim of Review R2-1: SetVerdictWithConnMark(ACCEPT, 0x1)
// persists a mark that a second packet matches via `-m connmark --mark 0x1/0x1`,
// whereas DROP does not. NOT shipped in the image.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	nfqueue "github.com/florianl/go-nfqueue/v2"
)

func main() {
	queue := flag.Uint("queue", 0, "nfqueue number")
	drop := flag.Bool("drop", false, "DROP instead of ACCEPT (to show the mark is lost)")
	mark := flag.Uint("mark", 0x1, "connmark to set on the verdict")
	flag.Parse()

	nf, err := nfqueue.Open(&nfqueue.Config{
		NfQueue:      uint16(*queue),
		MaxPacketLen: 0xffff,
		MaxQueueLen:  0xff,
		Copymode:     nfqueue.NfQnlCopyPacket,
	})
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer nf.Close()

	verdict := nfqueue.NfAccept
	if *drop {
		verdict = nfqueue.NfDrop
	}

	// NOTE: go-nfqueue/v2 v2.0.4's Attribute has no decoded conntrack-mark field
	// (only the raw Ct *[]byte blob and Mark = the packet nfmark). We do NOT read
	// the connmark in-process; the authoritative observation is `conntrack -L`
	// (Step 3) and the re-queue count (a 2nd "pkt id" line for the same flow).
	pktCount := 0
	fn := func(a nfqueue.Attribute) int {
		if a.PacketID == nil {
			return 0
		}
		id := *a.PacketID
		pktCount++
		log.Printf("pkt #%d id=%d -> verdict=%d set-connmark=0x%x", pktCount, id, verdict, *mark)
		if err := nf.SetVerdictWithConnMark(id, verdict, int(*mark)); err != nil {
			log.Printf("verdict error: %v", err)
		}
		return 0
	}
	errFn := func(e error) int { log.Printf("nfqueue error: %v", e); return 0 }

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := nf.RegisterWithErrorFunc(ctx, fn, errFn); err != nil {
		log.Fatalf("register: %v", err)
	}
	log.Printf("markspike: queue=%d verdict-on-match=%d connmark=0x%x — waiting", *queue, verdict, *mark)
	<-ctx.Done()
	_ = os.Stdout.Sync()
}
```

- [ ] **Step 2: Build the prototype**

Run: `cd responder && go build ./cmd/markspike && echo BUILD_OK`
Expected: `BUILD_OK` (confirms `SetVerdictWithConnMark` and `Attribute.CtMark` exist in the pinned go-nfqueue v2.0.4).

- [ ] **Step 3: Run the manual validation procedure (the GATE — needs a real Linux host with `nfnetlink_queue` + `nf_conntrack`, root)**

Pick an unused UDP test port (e.g. `9999`). Run the spike, install two iptables rules, send two packets, and read the conntrack mark. Record every result.

```sh
# Terminal A: the spike (ACCEPT + set mark 0x1)
sudo ./responder/cmd/markspike/markspike --queue 0 --mark 0x1

# Terminal B: install the claim rule (queue connmarked) AHEAD of the NEW rule,
# both on the test port. Order matters: connmark rule first.
sudo iptables -I INPUT 1 -p udp --dport 9999 -m conntrack --ctstate NEW \
  -j NFQUEUE --queue-num 0 --queue-bypass
sudo iptables -I INPUT 1 -p udp --dport 9999 -m connmark --mark 0x1/0x1 \
  -j NFQUEUE --queue-num 0 --queue-bypass

# Send packet #1 (NEW) from a FIXED source port so packet #2 reuses the exact
# same 5-tuple/flow (two bare `nc` calls would use different ephemeral ports =
# different flows = a false pass). The spike ACCEPTs it and sets connmark 0x1.
echo p1 | sudo nc -u -p 55555 -w1 127.0.0.1 9999

# Read the conntrack entry's mark — MUST show mark=1.
sudo conntrack -L -p udp --dport 9999

# Remove the NEW rule so the ONLY way packet #2 can reach the spike is the
# connmark rule. This makes the re-queue an unambiguous proof of the claim.
sudo iptables -D INPUT -p udp --dport 9999 -m conntrack --ctstate NEW -j NFQUEUE --queue-num 0 --queue-bypass

# Send packet #2 on the SAME flow — with the NEW rule gone, a re-queue can only
# come from `-m connmark --mark 0x1/0x1`.
echo p2 | sudo nc -u -p 55555 -w1 127.0.0.1 9999
```

Expected (PASS):
- `conntrack -L` after packet #1 shows the `udp ... dport=9999 ... mark=1` entry.
- The spike logs **two** `pkt #N id=...` lines for the flow — and critically, the *second* arrives **after the NEW rule was deleted**, so it was re-queued by the connmark rule alone. That is the proof the mark persisted and the claim works.

- [ ] **Step 4: Run the DROP control (proves the R2-1 failure mode)**

```sh
# Reset: flush the rules and the conntrack table for the port.
sudo iptables -D INPUT -p udp --dport 9999 -m connmark --mark 0x1/0x1 -j NFQUEUE --queue-num 0 --queue-bypass
sudo iptables -D INPUT -p udp --dport 9999 -m conntrack --ctstate NEW -j NFQUEUE --queue-num 0 --queue-bypass
sudo conntrack -D -p udp --dport 9999 2>/dev/null || true

# Re-run the spike with --drop and re-install rules, then send packet #1.
sudo ./responder/cmd/markspike/markspike --queue 0 --drop --mark 0x1
# (re-install both rules as in Step 3, then: echo p1 | sudo nc -u -w1 127.0.0.1 9999)
sudo conntrack -L -p udp --dport 9999
```

Expected (control, demonstrating the bug Task 1 avoids): after the DROP path there is **no** confirmed entry with `mark=1` for the flow (the unconfirmed entry was destroyed). This is *why* the production code ACCEPTs claimed packets.

- [ ] **Step 5: Record the gate verdict + clean up**

Append the observed `conntrack -L` output and the PASS/FAIL verdict to `.git/sdd/progress.md` under a "Task 1 — connmark prototype" heading. Remove the test rules:
```sh
sudo iptables -D INPUT -p udp --dport 9999 -m connmark --mark 0x1/0x1 -j NFQUEUE --queue-num 0 --queue-bypass 2>/dev/null || true
sudo iptables -D INPUT -p udp --dport 9999 -m conntrack --ctstate NEW -j NFQUEUE --queue-num 0 --queue-bypass 2>/dev/null || true
```

⛔ **GATE:** If Step 3 does not show the mark persisting and packet #2 re-queueing, **STOP** and reassess the mechanism (fallback options, in order: `SetVerdictWithConnMark` is correct but verify kernel `nf_conntrack` is loaded and the rule order; only if the verdict-CT path is genuinely unsupported on the target kernel, fall back to `libnetfilter_conntrack`/`conntrack -U` after ACCEPT — re-add `conntrack-tools` to the image in that case). Do not proceed to Task 2 until this passes.

- [ ] **Step 6: Commit**

```bash
git add responder/cmd/markspike/main.go
git commit -m "feat(responder): connmark-claim prototype proving R2-1 verdict-CT mechanism

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Pin quic-go + per-SNI self-signed cert resolver

**Files:**
- Modify: `responder/go.mod`, `responder/go.sum`
- Create: `responder/quiccert.go`
- Test: `responder/quiccert_test.go`

**Interfaces:**
- Produces: `func newCertResolver(domain string) *certResolver` — `domain` is the fallback SNI (`QUIC_CERT_DOMAIN`) used when a ClientHello carries no SNI.
- Produces: `func (r *certResolver) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)` — returns a cached, per-server-name self-signed cert (ECDSA P-256); mints on first use, caches by name. Suitable for `tls.Config.GetCertificate`.

- [ ] **Step 1: Add the quic-go dependency (pinned)**

Run:
```bash
cd responder && go get github.com/quic-go/quic-go@v0.60.0 && go mod tidy
```
Expected: `go.mod` now requires `github.com/quic-go/quic-go v0.60.0`; `go.sum` updated. Verify the module's Go floor matches ours:
```bash
go list -m -f '{{.GoVersion}}' github.com/quic-go/quic-go
```
Expected: `1.25.0` (consistent with `responder/go.mod`'s `go 1.25.0` and the `golang:1.25-alpine` build stage — no toolchain bump needed).

- [ ] **Step 2: Write the failing test**

`responder/quiccert_test.go`:
```go
package main

import (
	"crypto/tls"
	"testing"
)

func TestCertResolverMintsForSNI(t *testing.T) {
	r := newCertResolver("cloudflare.com")
	cert, err := r.getCertificate(&tls.ClientHelloInfo{ServerName: "example.org"})
	if err != nil {
		t.Fatalf("getCertificate: %v", err)
	}
	if cert == nil || cert.Leaf == nil {
		t.Fatal("nil cert/leaf")
	}
	found := false
	for _, n := range cert.Leaf.DNSNames {
		if n == "example.org" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cert DNSNames %v missing requested SNI", cert.Leaf.DNSNames)
	}
}

func TestCertResolverFallbackAndCaches(t *testing.T) {
	r := newCertResolver("cloudflare.com")
	// Empty SNI -> fallback domain.
	c1, err := r.getCertificate(&tls.ClientHelloInfo{ServerName: ""})
	if err != nil {
		t.Fatalf("getCertificate: %v", err)
	}
	if len(c1.Leaf.DNSNames) == 0 || c1.Leaf.DNSNames[0] != "cloudflare.com" {
		t.Fatalf("fallback DNSNames = %v, want [cloudflare.com]", c1.Leaf.DNSNames)
	}
	// Same name -> same cached *tls.Certificate pointer.
	c2, _ := r.getCertificate(&tls.ClientHelloInfo{ServerName: ""})
	if c1 != c2 {
		t.Fatal("expected cached cert to be reused")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd responder && go test -run TestCertResolver ./...`
Expected: FAIL — `undefined: newCertResolver`.

- [ ] **Step 4: Write the implementation**

`responder/quiccert.go`:
```go
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"sync"
	"time"
)

// certResolver mints and caches one self-signed ECDSA cert per requested SNI.
// It backs tls.Config.GetCertificate for the embedded QUIC handshake endpoint.
// The self-signed cert is a known weaker fingerprint (Review R2-6), inherited
// from the Rust original; a chain-validating prober still sees it is self-signed.
type certResolver struct {
	mu       sync.Mutex
	cache    map[string]*tls.Certificate
	fallback string // QUIC_CERT_DOMAIN, used when the ClientHello has no SNI
}

func newCertResolver(domain string) *certResolver {
	return &certResolver{cache: make(map[string]*tls.Certificate), fallback: domain}
}

func (r *certResolver) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := hello.ServerName
	if name == "" {
		name = r.fallback
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[name]; ok {
		return c, nil
	}
	c, err := selfSignedCert(name)
	if err != nil {
		return nil, err
	}
	r.cache[name] = c
	return c, nil
}

// selfSignedCert builds a 1-year self-signed ECDSA P-256 leaf for domain, with
// Leaf populated (so callers can read DNSNames without re-parsing).
func selfSignedCert(domain string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd responder && go test -run TestCertResolver ./...`
Expected: PASS (both tests).

- [ ] **Step 6: Commit**

```bash
git add responder/go.mod responder/go.sum responder/quiccert.go responder/quiccert_test.go
git commit -m "feat(responder): pin quic-go v0.60.0 + per-SNI self-signed cert resolver

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Custom `net.PacketConn` for the embedded endpoint

**Files:**
- Create: `responder/quicconn.go`
- Test: `responder/quicconn_test.go`

**Interfaces:**
- Produces: `type packetConn` implementing `net.PacketConn`.
- Produces: `func newPacketConn(local net.Addr, inject func(p []byte, addr net.Addr) error) *packetConn` — `inject` is called by `WriteTo` (raw socket in prod, in-memory delivery in tests).
- Produces: `func (c *packetConn) push(data []byte, addr net.Addr)` — enqueues an inbound packet that a subsequent `ReadFrom` will return. Non-blocking; drops if the queue is full or the conn is closed.
- Consumed by: `quicManager` (Task 4) which hands the conn to `quic.Transport{Conn: ...}`.

- [ ] **Step 1: Write the failing test**

`responder/quicconn_test.go`:
```go
package main

import (
	"net"
	"testing"
	"time"
)

func TestPacketConnPushReadAndWrite(t *testing.T) {
	var got []byte
	var gotAddr net.Addr
	inject := func(p []byte, addr net.Addr) error {
		got = append([]byte(nil), p...)
		gotAddr = addr
		return nil
	}
	local := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 51820}
	c := newPacketConn(local, inject)

	client := &net.UDPAddr{IP: net.ParseIP("203.0.113.9"), Port: 40000}
	c.push([]byte("probe"), client)

	buf := make([]byte, 1500)
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	n, addr, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if string(buf[:n]) != "probe" || addr.String() != client.String() {
		t.Fatalf("ReadFrom = %q from %v", buf[:n], addr)
	}

	if _, err := c.WriteTo([]byte("reply"), client); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if string(got) != "reply" || gotAddr.String() != client.String() {
		t.Fatalf("inject got %q to %v", got, gotAddr)
	}
	if c.LocalAddr().String() != local.String() {
		t.Fatalf("LocalAddr = %v", c.LocalAddr())
	}
}

func TestPacketConnReadDeadline(t *testing.T) {
	c := newPacketConn(&net.UDPAddr{}, func([]byte, net.Addr) error { return nil })
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, _, err := c.ReadFrom(make([]byte, 16)); err == nil {
		t.Fatal("expected timeout error on idle ReadFrom")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd responder && go test -run TestPacketConn ./...`
Expected: FAIL — `undefined: newPacketConn`.

- [ ] **Step 3: Write the implementation**

`responder/quicconn.go`:
```go
package main

import (
	"net"
	"os"
	"sync"
	"time"
)

type inPkt struct {
	data []byte
	addr net.Addr
}

// packetConn is a net.PacketConn that quic-go reads/writes. Inbound probe
// packets are fed in via push() (from the NFQUEUE loop); outbound packets are
// handed to inject (the raw-socket egress in prod, in-memory in tests).
type packetConn struct {
	inbound  chan inPkt
	inject   func(p []byte, addr net.Addr) error
	local    net.Addr
	closed   chan struct{}
	closeOne sync.Once

	mu       sync.Mutex
	deadline time.Time
}

func newPacketConn(local net.Addr, inject func(p []byte, addr net.Addr) error) *packetConn {
	return &packetConn{
		inbound: make(chan inPkt, 256),
		inject:  inject,
		local:   local,
		closed:  make(chan struct{}),
	}
}

// push enqueues an inbound packet. Non-blocking: drops on a full queue or after
// Close (a probe responder must never block the NFQUEUE receive loop).
func (c *packetConn) push(data []byte, addr net.Addr) {
	pkt := inPkt{data: append([]byte(nil), data...), addr: addr}
	select {
	case <-c.closed:
	case c.inbound <- pkt:
	default:
	}
}

func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.mu.Lock()
	dl := c.deadline
	c.mu.Unlock()
	var timer <-chan time.Time
	if !dl.IsZero() {
		t := time.NewTimer(time.Until(dl))
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-c.closed:
		return 0, nil, net.ErrClosed
	case <-timer:
		return 0, nil, os.ErrDeadlineExceeded
	case pkt := <-c.inbound:
		n := copy(p, pkt.data)
		return n, pkt.addr, nil
	}
}

func (c *packetConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	if err := c.inject(p, addr); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *packetConn) Close() error {
	c.closeOne.Do(func() { close(c.closed) })
	return nil
}

func (c *packetConn) LocalAddr() net.Addr { return c.local }

func (c *packetConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}

func (c *packetConn) SetWriteDeadline(time.Time) error { return nil }

func (c *packetConn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd responder && go test -run TestPacketConn ./...`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add responder/quicconn.go responder/quicconn_test.go
git commit -m "feat(responder): custom net.PacketConn bridging NFQUEUE to quic-go

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Embedded quic-go handshake endpoint (`quicManager`) + loopback test

**Files:**
- Create: `responder/quichs.go`
- Test: `responder/quichs_test.go`

**Interfaces:**
- Consumes: `packetConn` (Task 3), `certResolver` (Task 2), `sendRawUDP` (Plan-2 `egress.go`).
- Produces: `func newQUICManager(certDomain string, wgPort uint16) (*quicManager, error)` — builds the cert resolver, `tls.Config` (GetCertificate + ALPN), `packetConn` (prod injector = raw socket), `quic.Transport`, listener, and starts the accept-and-close goroutine.
- Produces: `func (m *quicManager) handle(payload []byte, client *net.UDPAddr, serverIP net.IP)` — records the per-client server IP and pushes the probe payload into the endpoint.
- Produces: `func (m *quicManager) Close() error`.

- [ ] **Step 1: Write the failing loopback integration test**

This wires two `packetConn`s back-to-back: our server endpoint on one, a quic-go **client** on the other. No root, no NFQUEUE — it proves the embedded endpoint + cert resolver complete a real TLS-1.3 handshake.

`responder/quichs_test.go`:
```go
package main

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	quic "github.com/quic-go/quic-go"
)

func TestQUICHandshakeCompletesLoopback(t *testing.T) {
	serverAddr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 51820}
	clientAddr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 40000}

	// Build the server endpoint with an in-memory injector that delivers to the
	// client conn (instead of the raw socket).
	resolver := newCertResolver("cloudflare.com")
	var clientConn *packetConn // set below; closure captures it
	srvConn := newPacketConn(serverAddr, func(p []byte, _ net.Addr) error {
		clientConn.push(p, serverAddr)
		return nil
	})
	tlsConf := &tls.Config{GetCertificate: resolver.getCertificate, NextProtos: []string{"h3"}}
	srvTr := &quic.Transport{Conn: srvConn}
	ln, err := srvTr.Listen(tlsConf, &quic.Config{})
	if err != nil {
		t.Fatalf("server Listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept(context.Background())
		if err == nil {
			c.CloseWithError(0, "")
		}
	}()

	// Build the client conn whose injector feeds the server endpoint.
	clientConn = newPacketConn(clientAddr, func(p []byte, _ net.Addr) error {
		srvConn.push(p, clientAddr)
		return nil
	})
	cliTr := &quic.Transport{Conn: clientConn}
	defer cliTr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := cliTr.Dial(ctx, serverAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "example.org",
		NextProtos:         []string{"h3"},
	}, &quic.Config{})
	if err != nil {
		t.Fatalf("client Dial (handshake) failed: %v", err)
	}
	defer conn.CloseWithError(0, "")

	state := conn.ConnectionState().TLS
	if !state.HandshakeComplete {
		t.Fatal("TLS handshake did not complete")
	}
	if state.ServerName != "example.org" {
		t.Fatalf("negotiated ServerName = %q, want example.org", state.ServerName)
	}
}
```

> **quic-go v0.60.0 API note for the implementer:** `Listener.Accept(ctx)` returns `(*quic.Conn, error)`; the connection type is `*quic.Conn` (renamed from the old `Connection` interface). `Transport.Dial(ctx, addr, tlsConf, quicConf)` returns `(*quic.Conn, error)`. `ConnectionState().TLS` is a `tls.ConnectionState`. Confirm against `go doc github.com/quic-go/quic-go.Transport` after the dependency is added; adjust only if the pinned version's signatures differ.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd responder && go test -run TestQUICHandshakeCompletesLoopback ./...`
Expected: FAIL — `undefined: newQUICManager` is not referenced yet, but the file `quichs.go` does not exist, so the package may still build (the test only uses Task-2/3 symbols + quic-go). If it builds, it should still drive you to add `quichs.go` in Step 3; if quic-go isn't wired it fails to compile referencing nothing new — in that case proceed to Step 3 which adds the production manager the loop needs. (The authoritative pass criterion is Step 4.)

- [ ] **Step 3: Write the production manager**

`responder/quichs.go`:
```go
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	quic "github.com/quic-go/quic-go"
)

// quicALPN lists the ALPNs the embedded endpoint will accept. A prober's
// ClientHello must offer one of these for the handshake to complete past
// EncryptedExtensions; ServerHello is emitted regardless, which already defeats
// a cheap "does it speak QUIC/TLS" prober. h3 covers curl --http3 / browsers.
var quicALPN = []string{"h3", "h3-29", "h3-32", "hq-interop", "doq"}

// quicManager runs an embedded quic-go server over a packetConn. The NFQUEUE
// loop feeds probe packets via handle(); replies are injected by the raw socket
// with sport=WG_PORT. Probe flows are kept off the kernel fast path by the
// caller's connmark claim; abandoned flows are reclaimed by quic-go's idle
// timeout and the conntrack UDP idle-timeout (no explicit ct delete needed).
type quicManager struct {
	conn   *packetConn
	tr     *quic.Transport
	ln     *quic.Listener
	cancel context.CancelFunc
	wgPort uint16

	mu       sync.Mutex
	srcByCli map[string]net.IP // client addr string -> server src IP for the reply
}

func newQUICManager(certDomain string, wgPort uint16) (*quicManager, error) {
	if certDomain == "" {
		return nil, fmt.Errorf("QUIC_CERT_DOMAIN must be non-empty when QUIC_HANDSHAKE=true")
	}
	m := &quicManager{
		wgPort:   wgPort,
		srcByCli: make(map[string]net.IP),
	}
	// Inject replies via the raw socket: src = the server IP the probe targeted,
	// sport = WG_PORT, dst = the client addr quic-go is writing to.
	inject := func(p []byte, addr net.Addr) error {
		ua, ok := addr.(*net.UDPAddr)
		if !ok {
			return fmt.Errorf("quic inject: non-UDP addr %T", addr)
		}
		m.mu.Lock()
		src := m.srcByCli[ua.String()]
		m.mu.Unlock()
		if src == nil {
			return fmt.Errorf("quic inject: no server IP for client %s", ua)
		}
		return sendRawUDP(src, ua.IP, m.wgPort, uint16(ua.Port), p)
	}

	resolver := newCertResolver(certDomain)
	tlsConf := &tls.Config{
		GetCertificate: resolver.getCertificate,
		NextProtos:     quicALPN,
		MinVersion:     tls.VersionTLS13,
	}
	m.conn = newPacketConn(&net.UDPAddr{Port: int(wgPort)}, inject)
	m.tr = &quic.Transport{Conn: m.conn}
	ln, err := m.tr.Listen(tlsConf, &quic.Config{MaxIdleTimeout: 30 * time.Second})
	if err != nil {
		_ = m.conn.Close()
		return nil, fmt.Errorf("quic listen: %w", err)
	}
	m.ln = ln

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.acceptLoop(ctx)
	return m, nil
}

// acceptLoop accepts completed handshakes and closes them immediately: a probe
// responder only needs the handshake to complete, not to serve streams.
func (m *quicManager) acceptLoop(ctx context.Context) {
	for {
		conn, err := m.ln.Accept(ctx)
		if err != nil {
			return // listener/transport closed
		}
		conn.CloseWithError(0, "")
	}
}

func (m *quicManager) handle(payload []byte, client *net.UDPAddr, serverIP net.IP) {
	m.mu.Lock()
	m.srcByCli[client.String()] = serverIP
	m.mu.Unlock()
	m.conn.push(payload, client)
}

func (m *quicManager) Close() error {
	if m.cancel != nil {
		m.cancel()
	}
	if m.ln != nil {
		_ = m.ln.Close()
	}
	if m.tr != nil {
		_ = m.tr.Close()
	}
	return m.conn.Close()
}
```

- [ ] **Step 4: Run the loopback test to verify it passes**

Run: `cd responder && go test -run TestQUICHandshakeCompletesLoopback ./...`
Expected: PASS — `HandshakeComplete` true, negotiated `ServerName == example.org` (proves the cert resolver served a per-SNI cert and the TLS-1.3 flight completed end-to-end).

- [ ] **Step 5: Run the full package test + vet**

Run: `cd responder && go test ./... && go vet ./...`
Expected: all PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add responder/quichs.go responder/quichs_test.go
git commit -m "feat(responder): embedded quic-go handshake endpoint + loopback test

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Wire the handshake + connmark claim into `decide` and the NFQUEUE loop

**Files:**
- Modify: `responder/responder.go`, `responder/nfqueue.go`
- Test: `responder/responder_test.go` (extend)

**Interfaces:**
- Consumes: `quicManager.handle` (Task 4), `nfqueue.SetVerdictWithConnMark` (go-nfqueue v2.0.4).
- Produces: `Config` gains `QUICHandshake bool`, `CertDomain string`, `WGPort uint16`.
- Produces: new `respKind` value `respQUICClaim`; `decide` returns `(VerdictAccept, respQUICClaim, nil)` for a well-formed **v1** Initial when `QUICHandshake` is true, else the existing VN `(VerdictDrop, respBytes, vn)`.
- Produces: `const connMarkClaim = 0x1`.

- [ ] **Step 1: Write the failing decision tests**

Add to `responder/responder_test.go`:
```go
func quicInitialV1() []byte {
	// long header + fixed bit, version=1, dcidLen=0, scidLen=0, + a little body.
	return []byte{0xC0, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0xAA, 0xBB}
}

func quicInitialUnsupported() []byte {
	// same shape but an unsupported (draft-style) version 0xff000099.
	return []byte{0xC0, 0xff, 0x00, 0x00, 0x99, 0x00, 0x00, 0xAA, 0xBB}
}

func TestDecideQUICHandshakeClaimsV1(t *testing.T) {
	cfg := Config{Protocol: "quic", QUICHandshake: true}
	v, k, b := decide(quicInitialV1(), cfg)
	if v != VerdictAccept || k != respQUICClaim || b != nil {
		t.Fatalf("v1 handshake: got (%v,%v,%v), want (Accept, respQUICClaim, nil)", v, k, b)
	}
}

func TestDecideQUICUnsupportedVersionGetsVN(t *testing.T) {
	cfg := Config{Protocol: "quic", QUICHandshake: true}
	v, k, b := decide(quicInitialUnsupported(), cfg)
	if v != VerdictDrop || k != respBytes || len(b) == 0 {
		t.Fatalf("unsupported version: got (%v,%v,%d bytes), want (Drop, respBytes, VN)", v, k, len(b))
	}
}

func TestDecideQUICHandshakeDisabledGetsVN(t *testing.T) {
	cfg := Config{Protocol: "quic", QUICHandshake: false}
	v, k, _ := decide(quicInitialV1(), cfg)
	if v != VerdictDrop || k != respBytes {
		t.Fatalf("QUIC_HANDSHAKE=false: got (%v,%v), want (Drop, respBytes=VN)", v, k)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd responder && go test -run TestDecideQUIC ./...`
Expected: FAIL — `undefined: respQUICClaim` and the v1 branch not implemented.

- [ ] **Step 3: Update `responder.go`**

In `responder/responder.go`, add the import and the new kind/constant, extend `Config`, and rewrite the `"quic"` case.

Add `"encoding/binary"` to the imports (the file currently has none — add an import block).

Add after the existing `respSTUN` line in the `respKind` const block:
```go
	respQUICClaim             // feed flow to the embedded quic-go endpoint; ACCEPT + connmark
```

Add the connmark constant near the top (after the `respKind` consts):
```go
// connMarkClaim is the conntrack mark the responder sets (via the verdict's
// NFQA_CT facility) to keep a multi-RTT QUIC probe flow queued to userspace.
// We own the conntrack mark entirely (awg-quick uses the packet fwmark, a
// different field), so the whole-value set is collision-free; the iptables
// match still uses the masked form 0x1/0x1 defensively.
const connMarkClaim = 0x1
```

Extend `Config`:
```go
type Config struct {
	Params        AwgParams
	Protocol      string // none|quic|dns|stun|sip
	QUICHandshake bool   // quic only: full TLS-1.3 handshake (true) vs VN-only (false)
	CertDomain    string // quic only: default SNI/cert domain (QUIC_CERT_DOMAIN)
	WGPort        uint16 // reply source port for injected handshake packets
}
```

Replace the existing `case "quic":` block in `decide` with:
```go
	case "quic":
		if detectQUIC(payload) {
			// Full handshake only for a well-formed v1 Initial; any other
			// (still QUIC-shaped) version gets our GREASE Version-Negotiation,
			// never quic-go's own VN (which would fingerprint its version list).
			if cfg.QUICHandshake && binary.BigEndian.Uint32(payload[1:5]) == 0x00000001 {
				return VerdictAccept, respQUICClaim, nil
			}
			return VerdictDrop, respBytes, buildQUICVersionNegotiation(payload)
		}
```
(`detectQUIC` guarantees `len(payload) >= 7`, so `payload[1:5]` is in bounds.)

- [ ] **Step 4: Update the NFQUEUE loop**

In `responder/nfqueue.go`, construct the manager and handle `respQUICClaim`.

After computing `cfg` is available (it's a `runQueue` parameter). Before `defer nf.Close()`'s `fn`, add manager construction:
```go
	var qmgr *quicManager
	if cfg.Protocol == "quic" && cfg.QUICHandshake {
		qmgr, err = newQUICManager(cfg.CertDomain, cfg.WGPort)
		if err != nil {
			return err
		}
		defer qmgr.Close()
	}
```

In the callback `fn`, after `verdict, kind, resp := decide(flow.payload, cfg)` and **before** the `if verdict == VerdictDrop` block, add:
```go
		if kind == respQUICClaim {
			// Feed the probe to the embedded endpoint (it injects the server
			// flight via raw socket), then ACCEPT *with* the connmark so the
			// now-confirmed flow's later packets re-enter userspace via the
			// connmark iptables rule. ACCEPT (not DROP) is required: a DROP
			// destroys the still-unconfirmed conntrack entry and the mark with
			// it (Review R2-1 — see the plan's Global Constraints).
			if qmgr != nil {
				qmgr.handle(flow.payload, &net.UDPAddr{IP: flow.srcIP, Port: int(flow.srcPort)}, flow.dstIP)
			}
			_ = nf.SetVerdictWithConnMark(id, nfqueue.NfAccept, connMarkClaim)
			return 0
		}
```

Add `"net"` to `nfqueue.go`'s imports.

- [ ] **Step 5: Run the tests + build + vet**

Run: `cd responder && go test ./... && go vet ./... && go build -o /dev/null .`
Expected: all PASS, vet clean, binary builds (the loop now references `newQUICManager`, `SetVerdictWithConnMark`, `net.UDPAddr`).

- [ ] **Step 6: Commit**

```bash
git add responder/responder.go responder/nfqueue.go responder/responder_test.go
git commit -m "feat(responder): route v1 QUIC Initials to the handshake endpoint + connmark claim

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Env parsing, entrypoint connmark rule, Dockerfile, compose/docs

**Files:**
- Modify: `responder/main.go`, `docker-entrypoint.sh`, `Dockerfile`, `docker-compose.yml`, `.env.example`, `README.md`

**Interfaces:**
- Consumes: `Config{QUICHandshake, CertDomain, WGPort}` (Task 5).

- [ ] **Step 1: Parse the new env in `main.go`**

In `responder/main.go`, after the `proto` resolution and before building `cfg`, add:
```go
	wgPort := uint16(51820)
	if p := os.Getenv("WG_PORT"); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			log.Fatalf("responder: bad WG_PORT: %v", err)
		}
		wgPort = uint16(n)
	}

	quicHandshake := strings.ToLower(os.Getenv("QUIC_HANDSHAKE")) != "false" // default true
	certDomain := os.Getenv("QUIC_CERT_DOMAIN")
	if certDomain == "" {
		certDomain = "cloudflare.com"
	}
	if proto == "quic" && quicHandshake && certDomain == "" {
		log.Fatal("responder: QUIC_CERT_DOMAIN must be non-empty when QUIC_HANDSHAKE=true")
	}
```
Update the `cfg` construction:
```go
	cfg := Config{
		Params:        params,
		Protocol:      proto,
		QUICHandshake: quicHandshake,
		CertDomain:    certDomain,
		WGPort:        wgPort,
	}
```
And extend the startup log line:
```go
	log.Printf("responder: protocol=%s queue=%d quic_handshake=%v — answering probes on WG_PORT",
		proto, queueNum, proto == "quic" && quicHandshake)
```
(`strconv` and `strings` are already imported in `main.go`.)

- [ ] **Step 2: Add the connmark NFQUEUE rule to the entrypoint**

In `docker-entrypoint.sh`, the connmark rule is only needed for the QUIC handshake. Add a guard and extend the insert/remove helpers.

After the `QUEUE_NUM=` line, add:
```sh
# The connmark claim rule is only needed for the multi-RTT QUIC handshake.
QUIC_HS="${QUIC_HANDSHAKE:-true}"
CLAIM_RULE=false
if [ "${IMITATE_PROTOCOL:-none}" = "quic" ] && [ "${QUIC_HS}" != "false" ]; then
  CLAIM_RULE=true
fi
```

Change `insert_nfqueue_rule` to also insert the connmark rule **ahead** of the NEW rule (so claimed flows always reach us). Insert the NEW rule first, then the connmark rule at position 1:
```sh
insert_nfqueue_rule() {
  # NEW-only first-contact rule (established flows bypass userspace).
  iptables -I INPUT 1 -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass
  if [ "${CLAIM_RULE}" = "true" ]; then
    # Claimed QUIC probe flows: keep the WHOLE flow queued to the responder
    # across RTTs. Inserted at position 1 so it precedes the NEW rule.
    iptables -I INPUT 1 -p udp --dport "${WG_PORT}" -m connmark --mark 0x1/0x1 \
      -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass
  fi
}
```

Change `remove_nfqueue_rule` to remove both (connmark first, symmetric):
```sh
remove_nfqueue_rule() {
  if [ "${CLAIM_RULE}" = "true" ]; then
    iptables -D INPUT -p udp --dport "${WG_PORT}" -m connmark --mark 0x1/0x1 \
      -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass 2>/dev/null || true
  fi
  iptables -D INPUT -p udp --dport "${WG_PORT}" -m conntrack --ctstate NEW \
    -m limit --limit 50/sec --limit-burst 100 \
    -j NFQUEUE --queue-num "${QUEUE_NUM}" --queue-bypass 2>/dev/null || true
}
```

- [ ] **Step 3: Drop the unused `conntrack-tools` from the runtime image**

The Plan-2 carry-forward flagged `conntrack-tools` as "keep if Plan 3 uses it, else drop." Plan 3 sets the mark via the nfqueue verdict (`SetVerdictWithConnMark`), **not** the `conntrack` CLI — so drop it.

In `Dockerfile`, change line 35 from:
```
RUN apk add --no-cache nodejs npm bash iproute2 iptables conntrack-tools dumb-init
```
to:
```
RUN apk add --no-cache nodejs npm bash iproute2 iptables dumb-init
```
(If Task 1's GATE fallback required the `conntrack` CLI, **skip this step** and leave `conntrack-tools` in.)

- [ ] **Step 4: Build the image through all stages**

Run: `docker build --tag amnezia-wg-easy:plan3 . && echo IMAGE_OK`
Expected: `IMAGE_OK` — the `build_responder` stage compiles the responder including quic-go (deps fetched via go.mod/go.sum; the stage already has `git`), and the runtime stage builds without `conntrack-tools`.

- [ ] **Step 5: Add env + docs**

In `docker-compose.yml`, after the `RESPONDER` env line, add:
```yaml
      # QUIC handshake mode (quic only): true = full TLS-1.3 handshake
      # continuation, false = Version-Negotiation only (weaker).
      - QUIC_HANDSHAKE=${QUIC_HANDSHAKE:-true}
      # SNI/cert domain for the QUIC handshake's default self-signed cert.
      - QUIC_CERT_DOMAIN=${QUIC_CERT_DOMAIN:-cloudflare.com}
```

In `.env.example`, after the `RESPONDER=false` block, add:
```bash
# When IMITATE_PROTOCOL=quic and RESPONDER=true, answer well-formed QUIC v1
# Initials with a full TLS-1.3 handshake (ServerHello + self-signed cert flight)
# instead of just Version-Negotiation. Set false for VN-only (weaker).
QUIC_HANDSHAKE=true
# SNI/cert domain for the handshake's default self-signed certificate. The
# responder still mints a per-SNI cert for each ClientHello; this is the
# fallback when a probe carries no SNI. (A chain-validating prober still sees a
# self-signed cert — a known, accepted limitation.)
QUIC_CERT_DOMAIN=cloudflare.com
```

In `README.md`, update the QUIC row of the responder table from the VN-only wording to:
```markdown
| `quic` | Full QUIC **TLS-1.3 handshake** (ServerHello + self-signed cert) for v1 Initials; Version-Negotiation (GREASE) for other versions |
```
and replace the closing blockquote ("The current QUIC answer is Version-Negotiation only; …") with:
```markdown
> The QUIC answer is a full TLS-1.3 handshake continuation (`QUIC_HANDSHAKE=true`,
> the default) via an embedded `quic-go` endpoint; set `QUIC_HANDSHAKE=false` for
> Version-Negotiation only. The handshake's self-signed cert (for `QUIC_CERT_DOMAIN`)
> defeats a cheap "does it speak QUIC/TLS" prober but is itself visible to a
> chain-validating one — a known, accepted limitation.
```

- [ ] **Step 6: Validate compose parses**

Run: `RESPONDER=true IMITATE_PROTOCOL=quic QUIC_HANDSHAKE=true docker compose config >/dev/null && echo COMPOSE_OK`
Expected: `COMPOSE_OK`.

- [ ] **Step 7: Commit**

```bash
git add responder/main.go docker-entrypoint.sh Dockerfile docker-compose.yml .env.example README.md
git commit -m "feat: wire QUIC_HANDSHAKE/QUIC_CERT_DOMAIN env + connmark rule; drop unused conntrack-tools

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Manual integration test matrix (acceptance gate)

**Files:** none (validation only — record results in `.git/sdd/progress.md`).

These require a real host with the AWG kernel module (or the go fallback), `nfnetlink_queue` + `nf_conntrack`, and a QUIC probe tool (`curl --http3`, a `quic-go` client, or `quiche`'s `quiche-client`). There is no automated equivalent; this is the acceptance gate. Record every result.

- [ ] **Step 1: Claim rule present**

`IMITATE_PROTOCOL=quic RESPONDER=true QUIC_HANDSHAKE=true` → `iptables -S INPUT | head -2` shows the `-m connmark --mark 0x1/0x1 ... NFQUEUE` rule at position 1 and the `--ctstate NEW ... NFQUEUE` rule at position 2; responder log prints `protocol=quic queue=0 quic_handshake=true`.

- [ ] **Step 2: Full handshake completes (multi-RTT, proves the connmark claim)**

Send a well-formed **v1** Initial from a real client (`curl --http3 https://WG_HOST:WG_PORT/` or a `quic-go` example client with `InsecureSkipVerify`). Expect the TLS-1.3 handshake to **complete** (ServerHello + Certificate received; client reaches the "handshake done" state). Concurrently, `conntrack -L -p udp --dport WG_PORT` shows the probe flow with `mark=1`. This is the decisive multi-RTT proof — without the connmark claim, the 2nd flight would stall on `awg0`.

- [ ] **Step 3: Unsupported version → Version-Negotiation**

Send a QUIC Initial with an **unsupported** version → tcpdump shows a Version-Negotiation reply (`sport=WG_PORT`) advertising GREASE `0x0a0a0a0a`, never `0x00000001`. (`QUIC_HANDSHAKE` does not affect this path.)

- [ ] **Step 4: `QUIC_HANDSHAKE=false` → VN only**

Restart with `QUIC_HANDSHAKE=false`. A well-formed v1 Initial now gets Version-Negotiation (no ServerHello); `iptables -S INPUT` shows **no** connmark rule (only the NEW rule).

- [ ] **Step 5: Real client still wins over detection (adversarial)**

With `IMITATE_PROTOCOL=quic RESPONDER=true`, a **real** AWG client still connects — its shaped handshake is ACCEPTed by `classifyAwgPacket` before the QUIC branch runs (re-confirm the Plan-2 invariant holds with the new branch in place).

- [ ] **Step 6: Fast path unaffected**

`iperf` over an established tunnel → responder CPU ~0 (ESTABLISHED, unmarked flows bypass userspace; only connmarked probe flows and `NEW` packets are queued). After the conntrack UDP idle-timeout a mid-stream transport packet re-enters as `NEW` and is still ACCEPTed (transport classify, Review F6).

- [ ] **Step 7: Crash isolation + claim teardown**

`kill` the `awg-responder` process → established tunnels keep flowing, new clients still connect; the entrypoint's `wait` tears down **both** the connmark and NEW rules (`iptables -S INPUT` shows neither). Only active-probe defense is lost. Abandoned probe `conntrack` entries with `mark=1` expire by the UDP idle-timeout (confirm they age out).

- [ ] **Step 8: Record the gate result**

Append the full matrix outcome (PASS/FAIL per step, with the Step-2 `curl`/client transcript and the `conntrack -L mark=1` line) to `.git/sdd/progress.md`. Mark Plan 3 complete only if Steps 1–7 pass.

---

## Self-Review

- **Spec coverage:** Design Decision 9 (full handshake) → Tasks 2–5; Review R2-1 (connmark via verdict CT, not save-mark) → Task 1 (gate) + Task 5 (`SetVerdictWithConnMark`); R2-2 (masked/disjoint mark) → Global Constraints + Task 6 rule; R2-3 (quic-go over custom PacketConn) → Tasks 3–4; R2-6 (self-signed cert limit) → Task 2 + README; QUIC VN retained for unsupported versions → Task 5 `decide` branch; `QUIC_HANDSHAKE`/`QUIC_CERT_DOMAIN` env → Task 6; SNI resolver → Task 2; idle eviction → `quic.Config.MaxIdleTimeout` + conntrack expiry (Task 4 doc, Task 7 Step 7). Plan-2 carry-forward (`conntrack-tools`) → Task 6 Step 3.
- **Divergence from the design, recorded:** the design's per-packet step 2 says "QUIC → … DROP the queued packet"; this plan **ACCEPTs** the claimed packet (with the connmark) because a DROP loses the unconfirmed conntrack entry/mark — the exact R2-1 failure the prototype guards. Stated in Global Constraints and the Task-5 code comment.
- **Type consistency:** `Config{QUICHandshake, CertDomain, WGPort}`, `respQUICClaim`, `connMarkClaim`, `newQUICManager(certDomain, wgPort)`, `(m *quicManager) handle(payload, *net.UDPAddr, net.IP)`, `newPacketConn(local, inject)`, `newCertResolver(domain)`/`getCertificate(*tls.ClientHelloInfo)` are used consistently across Tasks 2–6.
- **Open API risk (flagged, not a placeholder):** quic-go v0.60.0 surface names (`Transport.Listen`/`Dial`, `Listener.Accept`, `*quic.Conn`) are pinned to that version; the Task-4 note instructs the implementer to confirm via `go doc` after the dependency is added and adjust only if the pinned version differs.
