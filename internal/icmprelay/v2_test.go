package icmprelay

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestRouterErrorsRestoreQuotedRequest(t *testing.T) {
	request := packet()
	request[8] = 1
	request[10] = 0
	request[11] = 0
	binary.BigEndian.PutUint16(request[10:12], Checksum(request[:20]))
	wire := append([]byte(nil), request[20:]...)
	wire[4] = 77
	wire[2] = 0
	wire[3] = 0
	binary.BigEndian.PutUint16(wire[2:4], Checksum(wire))
	for _, kind := range []byte{11, 3} {
		quote := append([]byte(nil), request[:28]...)
		copy(quote[20:], wire[:8])
		msg := append(make([]byte, 8), quote...)
		msg[0] = kind
		binary.BigEndian.PutUint16(msg[2:4], Checksum(msg))
		reply := RestoreResponse(request, wire, netip.MustParseAddr("10.8.0.1"), 57, msg)
		if !ValidateResponse(request, reply) || reply[8] != 57 || reply[20] != kind || reply[12] != 10 {
			t.Fatalf("invalid error reply %x", reply)
		}
		if string(reply[28:]) != string(request[:28]) {
			t.Fatal("quoted request not restored")
		}
		msg[32] ^= 1
		msg[2] = 0
		msg[3] = 0
		binary.BigEndian.PutUint16(msg[2:4], Checksum(msg))
		if RestoreResponse(request, wire, netip.MustParseAddr("10.8.0.1"), 57, msg) != nil {
			t.Fatal("unrelated error accepted")
		}
	}
}
func TestV2EchoPreservesReplyTTL(t *testing.T) {
	request := packet()
	msg := append([]byte(nil), request[20:]...)
	msg[0] = 0
	msg[2] = 0
	msg[3] = 0
	binary.BigEndian.PutUint16(msg[2:4], Checksum(msg))
	r := RestoreResponse(request, request[20:], netip.MustParseAddr("1.1.1.0"), 49, msg)
	if !ValidateResponse(request, r) || r[8] != 49 {
		t.Fatal("lost TTL")
	}
	if RestoreResponse(request, request[20:], netip.MustParseAddr("1.1.1.1"), 49, msg) != nil {
		t.Fatal("wrong peer")
	}
}
