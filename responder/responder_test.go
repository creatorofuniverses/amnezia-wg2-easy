package main

import (
	"encoding/binary"
	"testing"
)

func dnsQuery() []byte {
	return []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
}

func TestDecideRealAwgAccepted(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "dns"}
	// A real transport packet (classify wins even though protocol=dns).
	real := makeAwg(20, 750, 20+200)
	v, kind, _ := decide(real, cfg)
	if v != VerdictAccept || kind != respNone {
		t.Fatalf("real AWG must be ACCEPTed, got v=%v kind=%v", v, kind)
	}
}

func TestDecideDNSProbeAnswered(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "dns"}
	v, kind, resp := decide(dnsQuery(), cfg)
	if v != VerdictDrop || kind != respBytes {
		t.Fatalf("DNS probe must be answered+DROP, got v=%v kind=%v", v, kind)
	}
	if binary.BigEndian.Uint16(resp[2:4])&0x000F != 2 {
		t.Error("expected SERVFAIL response bytes")
	}
}

func TestDecideOtherProtocolIgnored(t *testing.T) {
	// Configured for STUN; a DNS probe must be ignored (ACCEPT, no reply).
	cfg := Config{Params: testParams, Protocol: "stun"}
	v, kind, _ := decide(dnsQuery(), cfg)
	if v != VerdictAccept || kind != respNone {
		t.Fatalf("off-protocol probe must be ACCEPTed, got v=%v kind=%v", v, kind)
	}
}

func TestDecideSipNeverAnswers(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "sip"}
	v, kind, _ := decide(dnsQuery(), cfg)
	if v != VerdictAccept || kind != respNone {
		t.Fatalf("sip must never answer, got v=%v kind=%v", v, kind)
	}
}

func TestDecideSTUNProbeAnswered(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "stun"}
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:], 0x0001)
	binary.BigEndian.PutUint32(req[4:], 0x2112A442)
	v, kind, resp := decide(req, cfg)
	if v != VerdictDrop || kind != respSTUN || resp != nil {
		t.Fatalf("STUN probe should be DROP+respSTUN (loop builds reply), got v=%v kind=%v resp=%v", v, kind, resp)
	}
}

func TestDecideQUICProbeAnswered(t *testing.T) {
	cfg := Config{Params: testParams, Protocol: "quic"}
	in := []byte{0xC3, 0, 0, 0, 1, 0x04, 1, 2, 3, 4, 0x03, 9, 9, 9}
	v, kind, resp := decide(in, cfg)
	if v != VerdictDrop || kind != respBytes || len(resp) == 0 {
		t.Fatalf("QUIC probe should be DROP+respBytes, got v=%v kind=%v len=%d", v, kind, len(resp))
	}
	if resp[0]&0xC0 != 0xC0 {
		t.Error("QUIC reply should be a long-header VN packet")
	}
}
