package main

import "encoding/binary"

const dnsMaxResponse = 512 // RFC 1035 §2.3.4

// buildDNSServfail builds a SERVFAIL response (RCODE=2) echoing the query's
// transaction ID, RD bit, and question section when present.
func buildDNSServfail(incoming []byte) []byte {
	resp := make([]byte, 12)

	// Transaction ID (echo, or 0 if too short).
	if len(incoming) >= 2 {
		copy(resp[0:2], incoming[0:2])
	}

	// Flags: QR=1, Opcode=0, AA=0, TC=0, RD=copy, RA=1, RCODE=2.
	flags := uint16(0x8082)
	if len(incoming) >= 4 {
		if binary.BigEndian.Uint16(incoming[2:4])&0x0100 != 0 {
			flags |= 0x0100
		}
	}
	binary.BigEndian.PutUint16(resp[2:4], flags)
	// QDCOUNT/ANCOUNT/NSCOUNT/ARCOUNT already zero.

	// Echo the question section if we can parse a valid uncompressed QNAME.
	if len(incoming) > 12 {
		if end, ok := dnsQnameEnd(incoming, 12); ok {
			questionEnd := end + 4 // + QTYPE + QCLASS
			if questionEnd <= len(incoming) && len(resp)+(questionEnd-12) <= dnsMaxResponse {
				resp = append(resp, incoming[12:questionEnd]...)
				binary.BigEndian.PutUint16(resp[4:6], 1) // QDCOUNT = 1
			}
		}
	}
	return resp
}
