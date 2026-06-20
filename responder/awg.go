package main

import "encoding/binary"

// HRange is an inclusive AmneziaWG header magic range [Min, Max].
type HRange struct {
	Min, Max uint32
}

// Contains reports whether v falls within the inclusive range.
func (r HRange) Contains(v uint32) bool {
	return v >= r.Min && v <= r.Max
}

// AwgParams holds the S-padding offsets and H header ranges read from wg0.conf.
// S1/H1 = handshake-init, S2/H2 = handshake-response, S3/H3 = cookie-reply,
// S4/H4 = transport-data.
type AwgParams struct {
	S1, S2, S3, S4 uint32
	H1, H2, H3, H4 HRange
}

// WireGuard message sizes (S-padding excluded).
const (
	wgHandshakeInitSize     = 148
	wgHandshakeResponseSize = 92
	wgCookieReplySize       = 64
	wgTransportMinSize      = 32
)

// classifyAwgPacket reports whether data is a genuine AmneziaWG packet for the
// given params. It tries all four (S-offset, H-range, size) candidates in
// order. The obfuscated 4-byte header at the S-offset is read little-endian and
// must fall in the matching H-range. Handshake/cookie types require an exact
// length (S + size); transport requires at least S + 32.
func classifyAwgPacket(data []byte, p AwgParams) bool {
	type cand struct {
		off   uint32
		rng   HRange
		size  int
		exact bool
	}
	cands := []cand{
		{p.S1, p.H1, wgHandshakeInitSize, true},
		{p.S2, p.H2, wgHandshakeResponseSize, true},
		{p.S3, p.H3, wgCookieReplySize, true},
		{p.S4, p.H4, wgTransportMinSize, false},
	}
	for _, c := range cands {
		off := int(c.off)
		if len(data) < off+4 {
			continue
		}
		if c.exact {
			if len(data) != off+c.size {
				continue
			}
		} else if len(data) < off+c.size {
			continue
		}
		hdr := binary.LittleEndian.Uint32(data[off : off+4])
		if c.rng.Contains(hdr) {
			return true
		}
	}
	return false
}
