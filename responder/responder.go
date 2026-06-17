package main

// Verdict is the NFQUEUE disposition for a queued packet.
type Verdict int

const (
	VerdictAccept Verdict = iota
	VerdictDrop
)

// respKind tells the loop how to turn decide()'s result into a wire reply.
type respKind int

const (
	respNone  respKind = iota // no reply (ACCEPT)
	respBytes                 // reply is the returned bytes (DNS/QUIC-VN)
	respSTUN                  // loop builds the STUN reply using the client addr
)

// Config is the responder's startup configuration.
type Config struct {
	Params   AwgParams
	Protocol string // none|quic|dns|stun|sip
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
			return VerdictDrop, respBytes, buildQUICVersionNegotiation(payload)
		}
	// "sip": shaping only, never answered (Decision 8). "none": unreachable.
	}
	// 3. Genuine junk -> ACCEPT, let awg0 silently drop it.
	return VerdictAccept, respNone, nil
}
