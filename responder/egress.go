package main

import (
	"encoding/binary"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// onesComplementChecksum computes the 16-bit one's-complement checksum.
func onesComplementChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// udpChecksum computes the UDP checksum over the IPv4/IPv6 pseudo-header. The
// udp slice's checksum field (bytes 6:8) must be zero on entry. A zero result is
// returned as 0xffff (mandatory for IPv6).
func udpChecksum(src, dst net.IP, udp []byte) uint16 {
	var pseudo []byte
	if v4 := src.To4(); v4 != nil {
		pseudo = make([]byte, 12)
		copy(pseudo[0:4], v4)
		copy(pseudo[4:8], dst.To4())
		pseudo[9] = 17 // UDP
		binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(udp)))
	} else {
		pseudo = make([]byte, 40)
		copy(pseudo[0:16], src.To16())
		copy(pseudo[16:32], dst.To16())
		binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(udp)))
		pseudo[39] = 17
	}
	cs := onesComplementChecksum(append(pseudo, udp...))
	if cs == 0 {
		return 0xffff
	}
	return cs
}

// buildUDPDatagram builds a UDP header + payload with a filled-in checksum.
func buildUDPDatagram(src, dst net.IP, sport, dport uint16, payload []byte) []byte {
	udp := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(udp[0:2], sport)
	binary.BigEndian.PutUint16(udp[2:4], dport)
	binary.BigEndian.PutUint16(udp[4:6], uint16(8+len(payload)))
	copy(udp[8:], payload)
	binary.BigEndian.PutUint16(udp[6:8], udpChecksum(src, dst, udp))
	return udp
}

// buildIPv4UDP builds a complete IPv4 packet (20-byte header, no options) + UDP.
func buildIPv4UDP(src, dst net.IP, sport, dport uint16, payload []byte) []byte {
	udp := buildUDPDatagram(src, dst, sport, dport, payload)
	total := 20 + len(udp)
	ip := make([]byte, 20)
	ip[0] = 0x45 // version 4, IHL 5
	binary.BigEndian.PutUint16(ip[2:4], uint16(total))
	ip[8] = 64 // TTL
	ip[9] = 17 // UDP
	copy(ip[12:16], src.To4())
	copy(ip[16:20], dst.To4())
	binary.BigEndian.PutUint16(ip[10:12], onesComplementChecksum(ip))
	return append(ip, udp...)
}

// sendRawUDP injects a forged-source UDP reply to dst. Requires CAP_NET_RAW.
func sendRawUDP(src, dst net.IP, sport, dport uint16, payload []byte) error {
	if (src.To4() != nil) != (dst.To4() != nil) {
		return fmt.Errorf("sendRawUDP: src/dst address family mismatch (src=%v dst=%v)", src, dst)
	}
	if dst.To4() != nil {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_RAW)
		if err != nil {
			return fmt.Errorf("v4 socket: %w", err)
		}
		defer unix.Close(fd)
		pkt := buildIPv4UDP(src, dst, sport, dport, payload)
		var sa unix.SockaddrInet4
		copy(sa.Addr[:], dst.To4())
		sa.Port = int(dport)
		return unix.Sendto(fd, pkt, 0, &sa)
	}

	// IPv6: SOCK_RAW/IPPROTO_UDP. Bind to src so the kernel's chosen source
	// address matches the pseudo-header we used for the (mandatory) checksum.
	fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_RAW, unix.IPPROTO_UDP)
	if err != nil {
		return fmt.Errorf("v6 socket: %w", err)
	}
	defer unix.Close(fd)
	var bsa unix.SockaddrInet6
	copy(bsa.Addr[:], src.To16())
	if err := unix.Bind(fd, &bsa); err != nil {
		return fmt.Errorf("v6 bind src: %w", err)
	}
	udp := buildUDPDatagram(src, dst, sport, dport, payload)
	var sa unix.SockaddrInet6
	copy(sa.Addr[:], dst.To16())
	sa.Port = int(dport)
	return unix.Sendto(fd, udp, 0, &sa)
}
