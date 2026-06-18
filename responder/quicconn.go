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
