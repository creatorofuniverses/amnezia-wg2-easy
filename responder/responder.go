package main

import "encoding/binary"

// Verdict is the NFQUEUE disposition for a queued packet.
type Verdict int

const (
	VerdictAccept Verdict = iota
	VerdictDrop
)

// respKind tells the loop how to turn decide()'s result into a wire reply.
type respKind int

const (
	respNone      respKind = iota // no reply (ACCEPT)
	respBytes                     // reply is the returned bytes (DNS/QUIC-VN)
	respSTUN                      // loop builds the STUN reply using the client addr
	respQUICClaim                 // feed flow to the embedded quic-go endpoint; ACCEPT + connmark
)

// connMarkClaim is the conntrack mark the responder sets (via the verdict's
// NFQA_CT facility) to keep a multi-RTT QUIC probe flow queued to userspace.
// We own the conntrack mark entirely (awg-quick uses the packet fwmark, a
// different field), so the whole-value set is collision-free; the iptables
// match still uses the masked form 0x1/0x1 defensively.
const connMarkClaim = 0x1

// Config is the responder's startup configuration.
type Config struct {
	Params        AwgParams
	Protocol      string // none|quic|dns|stun|sip
	QUICHandshake bool   // quic only: full TLS-1.3 handshake (true) vs VN-only (false)
	CertDomain    string // quic only: default SNI/cert domain (QUIC_CERT_DOMAIN)
	WGPort        uint16 // reply source port for injected handshake packets
}

// decide runs the correctness-critical order: classify real AWG first, then —
// only for the configured protocol — detect a probe and choose a reply. It is
// pure: it never touches a socket and is addr-free (STUN's addr is applied by
// the caller via respSTUN).
func decide(payload []byte, cfg Config) (Verdict, respKind, []byte) {
	// 1. Genuine AWG handshake or transport -> ACCEPT (kernel fast path).
	if classifyAwgPacket(payload, cfg.Params) {
		return VerdictAccept, respNone, nil
	}
	// 2. Probe matching the configured protocol -> answer + DROP.
	switch cfg.Protocol {
	case "dns":
		if detectDNS(payload) {
			return VerdictDrop, respBytes, buildDNSServfail(payload)
		}
	case "stun":
		if detectSTUN(payload) {
			return VerdictDrop, respSTUN, nil
		}
	case "quic":
		if detectQUIC(payload) {
			// Full handshake only for a well-formed v1 Initial; any other
			// (still QUIC-shaped) version gets our GREASE Version-Negotiation,
			// never quic-go's own VN (which would fingerprint its version list).
			if cfg.QUICHandshake && binary.BigEndian.Uint32(payload[1:5]) == 0x00000001 {
				return VerdictAccept, respQUICClaim, nil
			}
			return VerdictDrop, respBytes, buildQUICVersionNegotiation(payload)
		}
	// "sip": shaping only, never answered (Decision 8). "none": unreachable.
	}
	// 3. Genuine junk -> ACCEPT, let awg0 silently drop it.
	return VerdictAccept, respNone, nil
}
