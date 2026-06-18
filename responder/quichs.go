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
