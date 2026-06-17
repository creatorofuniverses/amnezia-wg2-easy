package main

import (
	"encoding/binary"
	"net"
)

// udpFlow holds the addressing extracted from an NFQUEUE L3 packet plus the
// UDP application payload.
type udpFlow struct {
	srcIP, dstIP     net.IP // srcIP = client, dstIP = us (WG_PORT side)
	srcPort, dstPort uint16
	payload          []byte
}

// cloneIP copies address bytes out of the packet buffer so the returned net.IP
// does not alias (and outlive) the NFQUEUE buffer.
func cloneIP(b []byte) net.IP {
	ip := make(net.IP, len(b))
	copy(ip, b)
	return ip
}

// parseL3UDP parses an IPv4 or IPv6 packet (as delivered by NFQUEUE) that
// carries UDP, returning the flow or ok=false if it is not parseable UDP.
func parseL3UDP(pkt []byte) (udpFlow, bool) {
	if len(pkt) < 1 {
		return udpFlow{}, false
	}
	switch pkt[0] >> 4 {
	case 4:
		if len(pkt) < 20 {
			return udpFlow{}, false
		}
		ihl := int(pkt[0]&0x0f) * 4
		if pkt[9] != 17 || len(pkt) < ihl+8 {
			return udpFlow{}, false
		}
		udp := pkt[ihl:]
		return udpFlow{
			srcIP:   cloneIP(pkt[12:16]),
			dstIP:   cloneIP(pkt[16:20]),
			srcPort: binary.BigEndian.Uint16(udp[0:2]),
			dstPort: binary.BigEndian.Uint16(udp[2:4]),
			payload: udp[8:],
		}, true
	case 6:
		if len(pkt) < 48 || pkt[6] != 17 { // 40-byte v6 header, Next Header = UDP
			return udpFlow{}, false
		}
		udp := pkt[40:]
		return udpFlow{
			srcIP:   cloneIP(pkt[8:24]),
			dstIP:   cloneIP(pkt[24:40]),
			srcPort: binary.BigEndian.Uint16(udp[0:2]),
			dstPort: binary.BigEndian.Uint16(udp[2:4]),
			payload: udp[8:],
		}, true
	default:
		return udpFlow{}, false
	}
}
