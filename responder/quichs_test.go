package main

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	quic "github.com/quic-go/quic-go"
)

func TestQUICManagerSrcByCliBounded(t *testing.T) {
	m, err := newQUICManager("cloudflare.com", 51820)
	if err != nil {
		t.Fatalf("newQUICManager: %v", err)
	}
	defer m.Close()
	server := net.IPv4(10, 0, 0, 1)
	for i := 0; i < maxClaimedFlows+500; i++ {
		client := &net.UDPAddr{IP: net.IPv4(203, 0, 113, byte(i%256)), Port: 1024 + i}
		m.handle([]byte{0xc0, 0, 0, 0, 1, 0, 0}, client, server)
	}
	m.mu.Lock()
	n := len(m.srcByCli)
	m.mu.Unlock()
	if n > maxClaimedFlows {
		t.Fatalf("srcByCli grew to %d, want <= %d (unbounded map = DoS vector)", n, maxClaimedFlows)
	}
}

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

func TestQUICManagerHandshakeThroughInjector(t *testing.T) {
	serverIP := net.IPv4(10, 0, 0, 1)
	clientAddr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 40000}

	var clientConn *packetConn
	// In-memory wire-sender: deliver the server's egress to the client conn,
	// exercising the REAL m.inject (type assertion + srcByCli lookup) + m.send.
	send := func(src, dst net.IP, sport, dport uint16, p []byte) error {
		clientConn.push(p, &net.UDPAddr{IP: src, Port: int(sport)})
		return nil
	}
	m, err := newQUICManagerSend("cloudflare.com", 443, send)
	if err != nil {
		t.Fatalf("newQUICManagerSend: %v", err)
	}
	defer m.Close()

	// Client->server packets traverse the REAL handle() (stores srcByCli, pushes).
	clientConn = newPacketConn(clientAddr, func(p []byte, _ net.Addr) error {
		m.handle(p, clientAddr, serverIP)
		return nil
	})
	cliTr := &quic.Transport{Conn: clientConn}
	defer cliTr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := cliTr.Dial(ctx, &net.UDPAddr{IP: serverIP, Port: 443}, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "probe.example",
		NextProtos:         quicALPN,
	}, &quic.Config{})
	if err != nil {
		t.Fatalf("handshake through real manager injector failed: %v", err)
	}
	defer conn.CloseWithError(0, "")
	if !conn.ConnectionState().TLS.HandshakeComplete {
		t.Fatal("handshake did not complete")
	}
	if got := conn.ConnectionState().TLS.ServerName; got != "probe.example" {
		t.Fatalf("negotiated ServerName = %q, want probe.example", got)
	}
}
