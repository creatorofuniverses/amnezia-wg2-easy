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

func TestPacketConnCloseUnblocksAndRejects(t *testing.T) {
	c := newPacketConn(&net.UDPAddr{}, func([]byte, net.Addr) error { return nil })
	done := make(chan error, 1)
	go func() {
		_, _, err := c.ReadFrom(make([]byte, 16))
		done <- err
	}()
	time.Sleep(20 * time.Millisecond) // let ReadFrom block
	_ = c.Close()
	select {
	case err := <-done:
		if err != net.ErrClosed {
			t.Fatalf("ReadFrom after Close = %v, want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ReadFrom did not unblock on Close")
	}
	if _, err := c.WriteTo([]byte("x"), &net.UDPAddr{}); err != net.ErrClosed {
		t.Fatalf("WriteTo after Close = %v, want net.ErrClosed", err)
	}
	c.push([]byte("dropped"), &net.UDPAddr{}) // must not panic or block
	_ = c.Close()                             // double Close must not panic
}

func TestPacketConnPushDropsWhenFull(t *testing.T) {
	c := newPacketConn(&net.UDPAddr{}, func([]byte, net.Addr) error { return nil })
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	// Overfill well past the buffer; push must never block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			c.push([]byte("p"), addr)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("push blocked on a full queue (must be non-blocking)")
	}
}

func TestPacketConnDeadlineUpdateObserved(t *testing.T) {
	c := newPacketConn(&net.UDPAddr{}, func([]byte, net.Addr) error { return nil })
	done := make(chan error, 1)
	go func() {
		_, _, err := c.ReadFrom(make([]byte, 16))
		done <- err
	}()
	time.Sleep(20 * time.Millisecond) // ReadFrom blocking with no deadline
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout error after deadline set mid-read")
		}
	case <-time.After(time.Second):
		t.Fatal("ReadFrom did not observe the deadline set after it began blocking")
	}
}
