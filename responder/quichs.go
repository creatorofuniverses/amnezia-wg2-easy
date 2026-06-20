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

// maxClaimedFlows bounds srcByCli against a half-open Initial flood (an attacker
// can vary the source port to mint endless entries). Under normal load the map
// stays far below this; eviction only engages under flood, where dropping some
// in-flight prober state is the correct response. ~4096 * (key+net.IP) is well
// under a megabyte.
const maxClaimedFlows = 4096

// rawSender sends a forged-source UDP datagram. Production uses sendRawUDP; tests
// substitute an in-memory sender so the real injector path can be exercised
// without CAP_NET_RAW.
type rawSender func(src, dst net.IP, sport, dport uint16, payload []byte) error

// quicManager runs an embedded quic-go server over a packetConn. The NFQUEUE
// loop feeds probe packets via handle(); replies are injected by the raw socket
// with sport=WG_PORT. Probe flows are kept off the kernel fast path by the
// caller's connmark claim; abandoned flows are reclaimed by quic-go's idle
// timeout and the conntrack UDP idle-timeout (no explicit ct delete needed).
// Accepted connections are left to idle out (MaxIdleTimeout) rather than being
// closed immediately, so a probe sees a normal server that simply received no
// request instead of an instant CONNECTION_CLOSE.
type quicManager struct {
	conn   *packetConn
	tr     *quic.Transport
	ln     *quic.Listener
	cancel context.CancelFunc
	wgPort uint16
	send   rawSender

	mu       sync.Mutex
	srcByCli map[string]net.IP // client addr string -> server src IP for the reply
}

func newQUICManager(certDomain string, wgPort uint16) (*quicManager, error) {
	return newQUICManagerSend(certDomain, wgPort, sendRawUDP)
}

// newQUICManagerSend builds the manager with an explicit wire-sender (test seam).
func newQUICManagerSend(certDomain string, wgPort uint16, send rawSender) (*quicManager, error) {
	if certDomain == "" {
		return nil, fmt.Errorf("QUIC_CERT_DOMAIN must be non-empty when QUIC_HANDSHAKE=true")
	}
	m := &quicManager{
		wgPort:   wgPort,
		send:     send,
		srcByCli: make(map[string]net.IP),
	}
	resolver := newCertResolver(certDomain)
	tlsConf := &tls.Config{
		GetCertificate: resolver.getCertificate,
		NextProtos:     quicALPN,
		MinVersion:     tls.VersionTLS13,
	}
	m.conn = newPacketConn(&net.UDPAddr{Port: int(wgPort)}, m.inject)
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

// inject is the packetConn's outbound hook: resolve the reply source IP for the
// client from srcByCli and send via the configured wire-sender. Returned errors
// surface to quic-go's WriteTo.
func (m *quicManager) inject(p []byte, addr net.Addr) error {
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
	return m.send(src, ua.IP, m.wgPort, uint16(ua.Port), p)
}

// acceptLoop drains completed handshakes and lets each connection idle out
// (quic.Config.MaxIdleTimeout) rather than closing it immediately, so a probe
// sees a normal server that simply received no request instead of an instant
// CONNECTION_CLOSE. Accept must keep being called so the listener keeps
// completing new handshakes; we just never serve or close the accepted conn.
// Live-connection state is bounded by MaxIdleTimeout (30s) times the
// handshake-completion rate; completing a handshake costs the peer real crypto
// and round-trips, so this is not a cheap flood vector.
func (m *quicManager) acceptLoop(ctx context.Context) {
	for {
		conn, err := m.ln.Accept(ctx)
		if err != nil {
			return // listener/transport closed
		}
		_ = conn // idle-out: do not close or serve
	}
}

func (m *quicManager) handle(payload []byte, client *net.UDPAddr, serverIP net.IP) {
	key := client.String()
	m.mu.Lock()
	if _, ok := m.srcByCli[key]; !ok && len(m.srcByCli) >= maxClaimedFlows {
		// At capacity with a new client: evict an arbitrary entry to bound memory.
		for k := range m.srcByCli {
			delete(m.srcByCli, k)
			break
		}
	}
	m.srcByCli[key] = serverIP
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
