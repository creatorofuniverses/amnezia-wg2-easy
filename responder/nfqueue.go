package main

import (
	"context"
	"log"
	"net"

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

	var qmgr *quicManager
	if cfg.Protocol == "quic" && cfg.QUICHandshake {
		qmgr, err = newQUICManager(cfg.CertDomain, cfg.WGPort)
		if err != nil {
			return err
		}
		defer qmgr.Close()
	}

	fn := func(a nfqueue.Attribute) int {
		// go-nfqueue/v2 populates PacketID for every real queued packet. A
		// message with no PacketID cannot be verdicted (no id for SetVerdict),
		// so skip it gracefully rather than nil-deref panic in this
		// single-threaded receive loop (which would kill probe defense).
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
			// Non-deprecated option form; SetVerdictWithConnMark is deprecated in v2.0.4.
			_ = nf.SetVerdictWithOption(id, nfqueue.NfAccept, nfqueue.WithConnMark(connMarkClaim))
			return 0
		}
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
	// P1 instrumentation: distinguish a clean signal-driven exit (SIGINT/SIGTERM
	// cancels ctx) from a fatal error path (which returns up to main's log.Fatalf).
	// If the responder dies, this line tells us whether it was asked to.
	log.Printf("responder: context cancelled (%v) — read loop exiting cleanly", ctx.Err())
	return nil
}
