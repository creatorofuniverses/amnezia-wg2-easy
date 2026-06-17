package main

import "encoding/binary"

const (
	stunMagicCookie    uint32 = 0x2112A442
	stunBindingRequest uint16 = 0x0001
)

// isQUICVersion matches QUIC v1 (RFC 9000), v2 (RFC 9369), IETF drafts
// (0xff0000xx, xx != 0), and GREASE values (0x?a?a?a?a). It deliberately does
// not treat 0 as a version (0 is the Version-Negotiation marker).
func isQUICVersion(v uint32) bool {
	switch {
	case v == 0x00000001:
		return true
	case v == 0x6b3343cf:
		return true
	case v&0xffffff00 == 0xff000000 && v&0xff != 0:
		return true
	case v&0x0f0f0f0f == 0x0a0a0a0a:
		return true
	default:
		return false
	}
}

// detectQUIC reports whether data looks like a QUIC long-header Initial.
func detectQUIC(data []byte) bool {
	if len(data) < 7 {
		return false
	}
	if data[0]&0xC0 != 0xC0 { // long header + fixed bit
		return false
	}
	if !isQUICVersion(binary.BigEndian.Uint32(data[1:5])) {
		return false
	}
	dcidLen := int(data[5])
	if dcidLen > 20 {
		return false
	}
	scidOff := 6 + dcidLen
	if scidOff >= len(data) {
		return false
	}
	scidLen := int(data[scidOff])
	if scidLen > 20 {
		return false
	}
	return scidOff+1+scidLen <= len(data)
}

// detectSTUN reports whether data is a STUN Binding Request.
func detectSTUN(data []byte) bool {
	if len(data) < 20 {
		return false
	}
	if binary.BigEndian.Uint32(data[4:8]) != stunMagicCookie {
		return false
	}
	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != stunBindingRequest || msgType&0xC000 != 0 {
		return false
	}
	msgLen := binary.BigEndian.Uint16(data[2:4])
	if msgLen%4 != 0 {
		return false
	}
	return len(data) == 20+int(msgLen)
}

// dnsQnameEnd walks an uncompressed QNAME starting at start and returns the
// index just past the terminating root label, plus ok=false on any malformed
// label (compression pointer, label > 63, name > 255, truncation).
func dnsQnameEnd(data []byte, start int) (int, bool) {
	pos := start
	total := 0
	for {
		if pos >= len(data) {
			return 0, false
		}
		l := int(data[pos])
		if l&0xC0 != 0 { // compression/reserved bits not allowed
			return 0, false
		}
		if l == 0 {
			return pos + 1, true
		}
		if l > 63 {
			return 0, false
		}
		total += l + 1
		if total > 255 {
			return 0, false
		}
		pos += 1 + l
	}
}

// detectDNS reports whether data is a plausible uncompressed DNS query.
func detectDNS(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	// Not STUN (cookie at 4..8).
	if binary.BigEndian.Uint32(data[4:8]) == stunMagicCookie {
		return false
	}
	flags := binary.BigEndian.Uint16(data[2:4])
	if flags&0xF800 != 0 { // QR + Opcode must be 0 (standard query)
		return false
	}
	if binary.BigEndian.Uint16(data[4:6]) != 1 { // exactly one question
		return false
	}
	end, ok := dnsQnameEnd(data, 12)
	if !ok || end+4 > len(data) {
		return false
	}
	qclass := binary.BigEndian.Uint16(data[end+2 : end+4])
	switch qclass {
	case 1, 3, 4, 255: // IN, CH, HS, ANY
		return true
	default:
		return false
	}
}
