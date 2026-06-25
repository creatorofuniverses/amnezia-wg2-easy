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
	// Round 3 / P1 instrumentation: UTC microsecond timestamps on every responder
	// log line so its events (notably `nfqueue error: netlink i/o timeout`) can be
	// ordered against Node's ISO-8601 `tunnel`/`lifecycle` lines in the same
	// `docker logs` stream — both UTC, both sub-second. This is the responder half
	// of the "timestamped logging on both sides" P1 requires to pin flap causality.
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC)

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

	cfg := Config{
		Params:        params,
		Protocol:      proto,
		QUICHandshake: quicHandshake,
		CertDomain:    certDomain,
		WGPort:        wgPort,
	}
	if proto == "sip" {
		log.Println("responder: IMITATE_PROTOCOL=sip — shaping only; SIP probes are NOT answered")
	}
	log.Printf("responder: protocol=%s queue=%d quic_handshake=%v — answering probes on WG_PORT",
		proto, queueNum, proto == "quic" && quicHandshake)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runQueue(ctx, queueNum, cfg); err != nil {
		log.Fatalf("responder: queue: %v", err)
	}
}
