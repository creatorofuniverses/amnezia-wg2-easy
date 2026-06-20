package main

import (
	"encoding/binary"
	"testing"
)

func TestIsQUICVersion(t *testing.T) {
	ok := []uint32{0x00000001, 0x6b3343cf, 0xff000001, 0x0a0a0a0a, 0x1a2a3a4a}
	bad := []uint32{0x00000000, 0xff000000, 0x12345678}
	for _, v := range ok {
		if !isQUICVersion(v) {
			t.Errorf("0x%08x should be a QUIC version", v)
		}
	}
	for _, v := range bad {
		if isQUICVersion(v) {
			t.Errorf("0x%08x should NOT be a QUIC version", v)
		}
	}
}

func TestDetectQUIC(t *testing.T) {
	// long header (0xC0 bits) + v1 + dcid_len + scid_len.
	d := []byte{0xC3, 0, 0, 0, 1, 0x04, 1, 2, 3, 4, 0x03, 9, 9, 9}
	if !detectQUIC(d) {
		t.Fatal("well-formed QUIC Initial not detected")
	}
	// fixed bit clear -> not QUIC.
	bad := append([]byte{}, d...)
	bad[0] = 0x00
	if detectQUIC(bad) {
		t.Fatal("short-header/no-fixed-bit must not detect as QUIC")
	}
}

func TestDetectSTUN(t *testing.T) {
	d := make([]byte, 20)
	binary.BigEndian.PutUint16(d[0:], 0x0001) // Binding Request
	binary.BigEndian.PutUint16(d[2:], 0)      // length 0
	binary.BigEndian.PutUint32(d[4:], 0x2112A442)
	if !detectSTUN(d) {
		t.Fatal("STUN binding request not detected")
	}
	d[4] = 0 // break magic cookie
	if detectSTUN(d) {
		t.Fatal("bad magic cookie must not detect as STUN")
	}
}

func TestDetectDNS(t *testing.T) {
	// txid, flags=0x0100 (RD), qd=1, then qname "a" + root, QTYPE A, QCLASS IN.
	d := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
	if !detectDNS(d) {
		t.Fatal("valid DNS query not detected")
	}
	// QR=1 (response) must be rejected.
	bad := append([]byte{}, d...)
	bad[2] = 0x80
	if detectDNS(bad) {
		t.Fatal("DNS response (QR=1) must not detect as query")
	}
}

func TestDNSQnameEnd(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want int
		ok   bool
	}{
		{"valid single label", []byte{0x01, 'a', 0x00}, 3, true},
		{"root only", []byte{0x00}, 1, true},
		{"compression pointer rejected", []byte{0xC0, 0x0C}, 0, false},
		{"truncated no root", []byte{0x03, 'a', 'b', 'c'}, 0, false},
	}
	for _, c := range cases {
		got, ok := dnsQnameEnd(c.data, 0)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: got (%d,%v) want (%d,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestDNSQnameEndNameTooLong(t *testing.T) {
	// Five 63-byte labels (top bits clear, each individually valid) overflow the
	// 255-octet name limit -> must reject.
	var b []byte
	for i := 0; i < 5; i++ {
		b = append(b, 63)
		b = append(b, make([]byte, 63)...)
	}
	b = append(b, 0x00)
	if _, ok := dnsQnameEnd(b, 0); ok {
		t.Fatal("name > 255 octets should be rejected")
	}
}

func TestDetectQUICRejects(t *testing.T) {
	base := []byte{0xC3, 0, 0, 0, 1, 0x04, 1, 2, 3, 4, 0x03, 9, 9, 9}
	if detectQUIC(base[:6]) {
		t.Error("len<7 should reject")
	}
	badver := append([]byte{}, base...)
	badver[1], badver[2], badver[3], badver[4] = 0x12, 0x34, 0x56, 0x78
	if detectQUIC(badver) {
		t.Error("unrecognized version should reject")
	}
	bigDCID := append([]byte{}, base...)
	bigDCID[5] = 21
	if detectQUIC(bigDCID) {
		t.Error("dcid_len>20 should reject")
	}
}

func TestDetectDNSRejects(t *testing.T) {
	valid := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0, 0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01}
	q2 := append([]byte{}, valid...)
	q2[5] = 2
	if detectDNS(q2) {
		t.Error("qdcount!=1 should reject")
	}
	badClass := append([]byte{}, valid...)
	badClass[18] = 0x05 // qclass = 5 (not IN/CH/HS/ANY)
	if detectDNS(badClass) {
		t.Error("invalid qclass should reject")
	}
	stunish := make([]byte, 20)
	binary.BigEndian.PutUint16(stunish[0:], 0x0001)
	binary.BigEndian.PutUint32(stunish[4:], 0x2112A442)
	if detectDNS(stunish) {
		t.Error("STUN magic cookie must exclude DNS")
	}
}
