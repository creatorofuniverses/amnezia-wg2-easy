package main

import (
	"encoding/binary"
	"net"
	"testing"
)

// foldedSum verifies the standard internet-checksum invariant: summing all
// 16-bit words of a buffer whose checksum field is already filled yields 0xffff.
func foldedSum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return uint16(sum)
}

func TestIPv4HeaderChecksumValid(t *testing.T) {
	pkt := buildIPv4UDP(net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2), 51820, 5000, []byte("hi"))
	// IHL words 0..20 must checksum to 0xffff.
	if got := foldedSum(pkt[0:20]); got != 0xffff {
		t.Errorf("IPv4 header checksum invalid: 0x%04x", got)
	}
	if pkt[9] != 17 { // protocol UDP
		t.Errorf("IP proto not UDP: %d", pkt[9])
	}
	if int(binary.BigEndian.Uint16(pkt[2:4])) != len(pkt) {
		t.Error("IP total length mismatch")
	}
}

func TestUDPChecksumNonZeroAndValidV6(t *testing.T) {
	src := net.ParseIP("2001:db8::1")
	dst := net.ParseIP("2001:db8::2")
	udp := buildUDPDatagram(src, dst, 51820, 5000, []byte("probe-reply"))
	// Recompute over pseudo-header + udp (with checksum in place) -> 0xffff.
	pseudo := make([]byte, 40)
	copy(pseudo[0:16], src.To16())
	copy(pseudo[16:32], dst.To16())
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(udp)))
	pseudo[39] = 17
	if got := foldedSum(append(pseudo, udp...)); got != 0xffff {
		t.Errorf("v6 UDP checksum invalid: 0x%04x", got)
	}
	if binary.BigEndian.Uint16(udp[6:8]) == 0 {
		t.Error("v6 UDP checksum must not be zero")
	}
	if binary.BigEndian.Uint16(udp[0:2]) != 51820 {
		t.Error("source port not forged to 51820")
	}
}
