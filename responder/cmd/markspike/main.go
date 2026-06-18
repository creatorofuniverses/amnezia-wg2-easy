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
