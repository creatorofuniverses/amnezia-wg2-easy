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
