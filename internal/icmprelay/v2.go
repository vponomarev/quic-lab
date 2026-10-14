package icmprelay

import (
	"bytes"
	"encoding/binary"
	"net/netip"
)

func ipv4Reply(request []byte, source netip.Addr, ttl byte, msg []byte) []byte {
	if !source.Is4() || source.IsUnspecified() || source.IsMulticast() || ttl == 0 {
		return nil
	}
	b := make([]byte, 20+len(msg))
	b[0] = 0x45
	b[8] = ttl
	b[9] = 1
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
	src := source.As4()
	copy(b[12:16], src[:])
	copy(b[16:20], request[12:16])
	copy(b[20:], msg)
	binary.BigEndian.PutUint16(b[10:12], Checksum(b[:20]))
	return b
}

// RestoreResponse correlates a real exit response, restoring identifiers and the
// original quoted IP header so the caller can associate router errors with ping.
func RestoreResponse(request, wire []byte, source netip.Addr, ttl byte, message []byte) []byte {
	target, q, ok := Parse(request)
	if !ok || !ValidMessage(wire, 8) || len(message) < 8 || len(message) > MaxMessage || Checksum(message) != 0 {
		return nil
	}
	var msg []byte
	switch message[0] {
	case 0:
		if source != target || !ValidMessage(message, 0) || !bytes.Equal(message[4:], wire[4:]) {
			return nil
		}
		msg = append([]byte(nil), message...)
		copy(msg[4:6], q[4:6])
	case 3, 11:
		if message[0] == 11 && message[1] > 1 || message[0] == 3 && message[1] > 15 {
			return nil
		}
		quoted := message[8:]
		if len(quoted) < 28 || quoted[0]>>4 != 4 || quoted[9] != 1 {
			return nil
		}
		h := int(quoted[0]&15) * 4
		if h < 20 || h+8 > len(quoted) || int(binary.BigEndian.Uint16(quoted[2:4])) < h+8 || binary.BigEndian.Uint16(quoted[6:8])&0x3fff != 0 || Checksum(quoted[:h]) != 0 || netip.AddrFrom4([4]byte(quoted[16:20])) != target || !bytes.Equal(quoted[h:h+8], wire[:8]) {
			return nil
		}
		// Keep code and next-hop MTU, but remove untrusted extensions and replace the quote.
		originalHeader := int(request[0]&15) * 4
		msg = append(append([]byte(nil), message[:8]...), request[:originalHeader+8]...)
	default:
		return nil
	}
	msg[2] = 0
	msg[3] = 0
	binary.BigEndian.PutUint16(msg[2:4], Checksum(msg))
	return ipv4Reply(request, source, ttl, msg)
}
func ValidateResponse(request, response []byte) bool {
	_, q, ok := Parse(request)
	if !ok || len(response) < 28 || response[0] != 0x45 || response[9] != 1 || int(binary.BigEndian.Uint16(response[2:4])) != len(response) || Checksum(response[:20]) != 0 || !bytes.Equal(response[16:20], request[12:16]) {
		return false
	}
	restored := RestoreResponse(request, q, netip.AddrFrom4([4]byte(response[12:16])), response[8], response[20:])
	return restored != nil && bytes.Equal(restored, response)
}
