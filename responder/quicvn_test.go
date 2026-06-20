package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildQUICVersionNegotiation(t *testing.T) {
	// dcid = {1,2,3,4} (len 4), scid = {9,9,9} (len 3).
	in := []byte{0xC3, 0, 0, 0, 1, 0x04, 1, 2, 3, 4, 0x03, 9, 9, 9}
	r := buildQUICVersionNegotiation(in)

	if r[0]&0xC0 != 0xC0 {
		t.Error("long-header/fixed bit not set")
	}
	if binary.BigEndian.Uint32(r[1:5]) != 0 {
		t.Error("version field must be 0 (VN marker)")
	}
	// Response DCID = incoming SCID.
	if r[5] != 3 || !bytes.Equal(r[6:9], []byte{9, 9, 9}) {
		t.Errorf("response DCID should be incoming SCID, got len=%d %v", r[5], r[6:9])
	}
	// Response SCID = incoming DCID.
	if r[9] != 4 || !bytes.Equal(r[10:14], []byte{1, 2, 3, 4}) {
		t.Errorf("response SCID should be incoming DCID, got len=%d %v", r[9], r[10:14])
	}
	// Supported version = GREASE, never v1.
	sv := binary.BigEndian.Uint32(r[14:18])
	if sv != 0x0a0a0a0a {
		t.Errorf("supported version should be GREASE 0x0a0a0a0a, got 0x%08x", sv)
	}
	if sv == 0x00000001 {
		t.Fatal("MUST NOT advertise v1")
	}
}

func TestBuildQUICVNMalformed(t *testing.T) {
	// Truncated: claims dcid_len=20 but no bytes follow -> both CIDs zero-length.
	in := []byte{0xC0, 0, 0, 0, 1, 0x14}
	r := buildQUICVersionNegotiation(in)
	if r[5] != 0 { // response DCID length
		t.Errorf("malformed input should yield zero-length DCID, got %d", r[5])
	}
	if r[6] != 0 { // response SCID length
		t.Errorf("malformed input should yield zero-length SCID, got %d", r[6])
	}
	if binary.BigEndian.Uint32(r[7:11]) != 0x0a0a0a0a {
		t.Error("supported version should still be GREASE")
	}
}
