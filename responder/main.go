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
