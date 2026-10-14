// Package icmprelay forwards bounded IPv4 Echo requests, never fabricating reachability.
package icmprelay

import (
	"encoding/binary"
	"net/netip"
)

const MaxMessage = 1260 // IPv4 packet fits the Windows 1280-byte TUN MTU.
func Checksum(b []byte) uint16 {
	var s uint32
	for len(b) > 1 {
		s += uint32(binary.BigEndian.Uint16(b))
		b = b[2:]
	}
	if len(b) > 0 {
		s += uint32(b[0]) << 8
	}
	for s>>16 != 0 {
		s = (s & 65535) + (s >> 16)
	}
	return ^uint16(s)
}
func ValidMessage(b []byte, kind byte) bool {
	return len(b) >= 8 && len(b) <= MaxMessage && b[0] == kind && b[1] == 0 && Checksum(b) == 0
}
func Parse(b []byte) (netip.Addr, []byte, bool) {
	if len(b) < 28 || b[0]>>4 != 4 || b[9] != 1 {
		return netip.Addr{}, nil, false
	}
	h := int(b[0]&15) * 4
	n := int(binary.BigEndian.Uint16(b[2:4]))
	if h < 20 || h+8 > len(b) || n != len(b) || binary.BigEndian.Uint16(b[6:8])&0x3fff != 0 || Checksum(b[:h]) != 0 || !ValidMessage(b[h:], 8) {
		return netip.Addr{}, nil, false
	}
	return netip.AddrFrom4([4]byte(b[16:20])), b[h:], true
}
func Reply(request, message []byte) []byte {
	_, q, ok := Parse(request)
	if !ok || !ValidMessage(message, 0) || len(q) != len(message) {
		return nil
	}
	for i := 4; i < len(q); i++ {
		if q[i] != message[i] {
			return nil
		}
	}
	b := make([]byte, 20+len(message))
	b[0] = 0x45
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
	b[8] = 64
	b[9] = 1
	copy(b[12:16], request[16:20])
	copy(b[16:20], request[12:16])
	copy(b[20:], message)
	binary.BigEndian.PutUint16(b[10:12], Checksum(b[:20]))
	return b
}
