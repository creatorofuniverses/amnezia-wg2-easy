package main

import (
	"encoding/binary"
	"net"
	"testing"
)

func stunReq(txid []byte) []byte {
	d := make([]byte, 20)
	binary.BigEndian.PutUint16(d[0:], 0x0001)
	binary.BigEndian.PutUint32(d[4:], 0x2112A442)
	copy(d[8:20], txid)
	return d
}

func TestBuildSTUNv4(t *testing.T) {
	txid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	r := buildSTUNBindingSuccess(stunReq(txid), net.IPv4(203, 0, 113, 5), 40000)

	if binary.BigEndian.Uint16(r[0:2]) != 0x0101 {
		t.Error("not a binding-success response")
	}
	if binary.BigEndian.Uint16(r[2:4]) != 12 { // attr header 4 + value 8
		t.Errorf("message length wrong: %d", binary.BigEndian.Uint16(r[2:4]))
	}
	if string(r[8:20]) != string(txid) {
		t.Error("txid not echoed")
	}
	if binary.BigEndian.Uint16(r[20:22]) != 0x0020 { // XOR-MAPPED-ADDRESS
		t.Error("attr type not XOR-MAPPED-ADDRESS")
	}
	if r[25] != 0x01 { // family IPv4
		t.Errorf("family wrong: 0x%02x", r[25])
	}
	// XOR-decode and confirm round-trip.
	gotPort := binary.BigEndian.Uint16(r[26:28]) ^ uint16(0x2112)
	if gotPort != 40000 {
		t.Errorf("port decode wrong: %d", gotPort)
	}
	var key [4]byte
	binary.BigEndian.PutUint32(key[:], 0x2112A442)
	gotIP := net.IPv4(r[28]^key[0], r[29]^key[1], r[30]^key[2], r[31]^key[3])
	if !gotIP.Equal(net.IPv4(203, 0, 113, 5)) {
		t.Errorf("ip decode wrong: %v", gotIP)
	}
}

func TestBuildSTUNv6(t *testing.T) {
	txid := []byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2}
	ip := net.ParseIP("2001:db8::1")
	r := buildSTUNBindingSuccess(stunReq(txid), ip, 1234)
	if binary.BigEndian.Uint16(r[2:4]) != 24 { // attr header 4 + value 20
		t.Errorf("v6 message length wrong: %d", binary.BigEndian.Uint16(r[2:4]))
	}
	if r[25] != 0x02 {
		t.Errorf("v6 family wrong: 0x%02x", r[25])
	}
	// Reconstruct key = cookie(4) || txid(12) and decode.
	key := make([]byte, 16)
	binary.BigEndian.PutUint32(key[0:4], 0x2112A442)
	copy(key[4:16], txid)
	dec := make([]byte, 16)
	for i := 0; i < 16; i++ {
		dec[i] = r[28+i] ^ key[i]
	}
	if !net.IP(dec).Equal(ip) {
		t.Errorf("v6 ip decode wrong: %v", net.IP(dec))
	}
}
