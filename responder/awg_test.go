package main

import (
	"encoding/binary"
	"testing"
)

// makeAwg builds a synthetic AWG datagram: sOff random bytes, then a 4-byte
// LE header equal to hdr, then enough trailer to reach totalLen.
func makeAwg(sOff int, hdr uint32, totalLen int) []byte {
	d := make([]byte, totalLen)
	for i := 0; i < sOff && i < totalLen; i++ {
		d[i] = byte(0x40 + i) // arbitrary non-zero padding
	}
	if sOff+4 <= totalLen {
		binary.LittleEndian.PutUint32(d[sOff:], hdr)
	}
	return d
}

var testParams = AwgParams{
	S1: 8, S2: 12, S3: 16, S4: 20,
	H1: HRange{100, 200}, H2: HRange{300, 400},
	H3: HRange{500, 600}, H4: HRange{700, 800},
}

func TestClassifyHandshakeInit(t *testing.T) {
	// init = 148 payload, so totalLen = S1 + 148.
	d := makeAwg(8, 150, 8+148)
	if !classifyAwgPacket(d, testParams) {
		t.Fatal("valid handshake-init not classified")
	}
}

func TestClassifyTransportMinAndLarger(t *testing.T) {
	// transport: header in H4, len >= S4 + 32. Test exact-min and larger.
	for _, n := range []int{20 + 32, 20 + 1400} {
		d := makeAwg(20, 750, n)
		if !classifyAwgPacket(d, testParams) {
			t.Fatalf("valid transport len=%d not classified", n)
		}
	}
}

func TestClassifyRejectsWrongHeader(t *testing.T) {
	// init size but header outside H1.
	d := makeAwg(8, 999, 8+148)
	if classifyAwgPacket(d, testParams) {
		t.Fatal("header outside H1 must not classify as init")
	}
}

func TestClassifyRejectsWrongSize(t *testing.T) {
	// header in H1 but length is not S1+148 (and not any other exact size).
	d := makeAwg(8, 150, 8+100)
	if classifyAwgPacket(d, testParams) {
		t.Fatal("wrong handshake size must not classify")
	}
}

func TestClassifyRejectsJunk(t *testing.T) {
	if classifyAwgPacket([]byte{1, 2, 3}, testParams) {
		t.Fatal("short junk must not classify")
	}
}

func TestClassifyHandshakeResponse(t *testing.T) {
	d := makeAwg(12, 350, 12+92)
	if !classifyAwgPacket(d, testParams) {
		t.Fatal("valid handshake-response not classified")
	}
}

func TestClassifyCookieReply(t *testing.T) {
	d := makeAwg(16, 550, 16+64)
	if !classifyAwgPacket(d, testParams) {
		t.Fatal("valid cookie-reply not classified")
	}
}
