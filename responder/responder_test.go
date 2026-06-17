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
