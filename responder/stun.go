package main

import (
	"encoding/binary"
	"net"
)

const (
	stunBindingSuccess uint16 = 0x0101
	stunAttrXorMapped  uint16 = 0x0020
)

// buildSTUNBindingSuccess builds a Binding-Success-Response with a single
// XOR-MAPPED-ADDRESS attribute for the observed client address.
func buildSTUNBindingSuccess(incoming []byte, clientIP net.IP, clientPort uint16) []byte {
	v4 := clientIP.To4()
	addrLen := 16
	family := byte(0x02)
	if v4 != nil {
		addrLen = 4
		family = 0x01
	}
	valueLen := 4 + addrLen      // reserved+family+port (4) + addr
	msgLen := 4 + valueLen       // attr header (type+len) + value

	resp := make([]byte, 20+4+valueLen)
	binary.BigEndian.PutUint16(resp[0:2], stunBindingSuccess)
	binary.BigEndian.PutUint16(resp[2:4], uint16(msgLen))
	binary.BigEndian.PutUint32(resp[4:8], stunMagicCookie)
	if len(incoming) >= 20 {
		copy(resp[8:20], incoming[8:20]) // echo transaction ID
	}

	// Attribute header.
	binary.BigEndian.PutUint16(resp[20:22], stunAttrXorMapped)
	binary.BigEndian.PutUint16(resp[22:24], uint16(valueLen))
	resp[24] = 0x00 // reserved
	resp[25] = family

	// XOR port with high 16 bits of the magic cookie.
	binary.BigEndian.PutUint16(resp[26:28], clientPort^uint16(stunMagicCookie>>16))

	// XOR address.
	if v4 != nil {
		var cookie [4]byte
		binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
		for i := 0; i < 4; i++ {
			resp[28+i] = v4[i] ^ cookie[i]
		}
	} else {
		key := make([]byte, 16)
		binary.BigEndian.PutUint32(key[0:4], stunMagicCookie)
		if len(incoming) >= 20 {
			copy(key[4:16], incoming[8:20]) // transaction ID
		}
		ip16 := clientIP.To16()
		for i := 0; i < 16; i++ {
			resp[28+i] = ip16[i] ^ key[i]
		}
	}
	return resp
}
