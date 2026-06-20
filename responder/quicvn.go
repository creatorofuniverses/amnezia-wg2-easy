package main

import "encoding/binary"

// quicGREASEVersion is advertised as the sole "supported" version. It signals
// "no version in common" without ever claiming v1 (RFC 9000 §6.2), which would
// both violate the spec and be a fingerprint.
const quicGREASEVersion uint32 = 0x0a0a0a0a

// buildQUICVersionNegotiation builds a Version-Negotiation packet that swaps the
// incoming DCID/SCID and advertises only a GREASE version.
func buildQUICVersionNegotiation(incoming []byte) []byte {
	var inDCID, inSCID []byte
	if len(incoming) >= 6 {
		dcidLen := int(incoming[5])
		dcidEnd := 6 + dcidLen
		if dcidLen <= 20 && len(incoming) > dcidEnd {
			scidLen := int(incoming[dcidEnd])
			scidEnd := dcidEnd + 1 + scidLen
			if scidLen <= 20 && len(incoming) >= scidEnd {
				inDCID = incoming[6:dcidEnd]
				inSCID = incoming[dcidEnd+1 : scidEnd]
			}
		}
	}

	first := byte(0xC0)
	if len(incoming) > 0 {
		first = incoming[0] | 0xC0
	}

	resp := make([]byte, 0, 7+len(inDCID)+len(inSCID)+4)
	resp = append(resp, first)
	resp = append(resp, 0, 0, 0, 0)          // version = 0 (VN marker)
	resp = append(resp, byte(len(inSCID)))   // response DCID len = incoming SCID len
	resp = append(resp, inSCID...)           // response DCID = incoming SCID
	resp = append(resp, byte(len(inDCID)))   // response SCID len = incoming DCID len
	resp = append(resp, inDCID...)           // response SCID = incoming DCID
	var gv [4]byte
	binary.BigEndian.PutUint32(gv[:], quicGREASEVersion)
	resp = append(resp, gv[:]...)
	return resp
}
