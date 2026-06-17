package main

import (
	"encoding/binary"
	"testing"
)

func TestBuildDNSServfailEchoesQuestion(t *testing.T) {
	q := []byte{
		0xAB, 0xCD, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x03, 'w', 'w', 'w', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
	r := buildDNSServfail(q)
	if binary.BigEndian.Uint16(r[0:2]) != 0xABCD {
		t.Error("txid not echoed")
	}
	flags := binary.BigEndian.Uint16(r[2:4])
	if flags&0x8000 == 0 || flags&0x000F != 2 || flags&0x0080 == 0 {
		t.Errorf("flags wrong: 0x%04x (want QR=1, RA=1, RCODE=2)", flags)
	}
	if flags&0x0100 == 0 {
		t.Error("RD bit should be copied from query")
	}
	if binary.BigEndian.Uint16(r[4:6]) != 1 {
		t.Error("QDCOUNT should be 1 when question echoed")
	}
	// Question bytes echoed verbatim.
	if string(r[12:]) != string(q[12:]) {
		t.Error("question section not echoed verbatim")
	}
}

func TestBuildDNSServfailHeaderOnly(t *testing.T) {
	// Header-only input (12 bytes) -> QDCOUNT 0, no question.
	q := make([]byte, 12)
	q[0], q[1] = 0x00, 0x05
	r := buildDNSServfail(q)
	if len(r) != 12 {
		t.Errorf("want 12-byte header-only response, got %d", len(r))
	}
	if binary.BigEndian.Uint16(r[4:6]) != 0 {
		t.Error("QDCOUNT should be 0 with no question")
	}
}

// Additional coverage tests per requirements

// TestBuildDNSServfailRDClear: query with RD clear -> response flags must NOT set the RD bit
func TestBuildDNSServfailRDClear(t *testing.T) {
	q := []byte{
		0x12, 0x34, 0x00, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x03, 'w', 'w', 'w', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
	// Flags at bytes 2-3 are 0x0000 (no RD bit set)
	r := buildDNSServfail(q)

	// Verify txid is echoed
	if binary.BigEndian.Uint16(r[0:2]) != 0x1234 {
		t.Error("txid not echoed")
	}

	flags := binary.BigEndian.Uint16(r[2:4])

	// Should be SERVFAIL (QR=1, RA=1, RCODE=2) but without RD
	if flags&0x8000 == 0 {
		t.Error("QR bit should be set")
	}
	if flags&0x0080 == 0 {
		t.Error("RA bit should be set")
	}
	if flags&0x000F != 2 {
		t.Error("RCODE should be 2 (SERVFAIL)")
	}
	if flags&0x0100 != 0 {
		t.Error("RD bit should NOT be set when query had RD clear")
	}

	// Question should still be echoed
	if binary.BigEndian.Uint16(r[4:6]) != 1 {
		t.Error("QDCOUNT should be 1 when question echoed")
	}
}

// TestBuildDNSServfailMalformedQName: query with compression pointer in QNAME
// -> question is NOT echoed and QDCOUNT stays 0
func TestBuildDNSServfailMalformedQName(t *testing.T) {
	// Header + malformed QNAME (compression pointer at byte 12: 0xC0)
	q := []byte{
		0x11, 0x22, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0xC0, 0x10, 0x00, 0x01, 0x00, 0x01,
	}
	r := buildDNSServfail(q)

	// Should be header-only (12 bytes) since QNAME is malformed
	if len(r) != 12 {
		t.Errorf("want 12-byte header-only response for malformed QNAME, got %d", len(r))
	}

	// QDCOUNT should be 0
	if binary.BigEndian.Uint16(r[4:6]) != 0 {
		t.Error("QDCOUNT should be 0 when QNAME is malformed")
	}

	// Verify txid is still echoed and flags are correct
	if binary.BigEndian.Uint16(r[0:2]) != 0x1122 {
		t.Error("txid not echoed")
	}

	flags := binary.BigEndian.Uint16(r[2:4])
	if flags&0x8000 == 0 || flags&0x000F != 2 || flags&0x0080 == 0 {
		t.Errorf("flags wrong: 0x%04x (want QR=1, RA=1, RCODE=2)", flags)
	}
}
