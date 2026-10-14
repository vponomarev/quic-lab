package netstack

import (
	"context"
	"encoding/binary"
	"net/netip"
	"quiclab/internal/icmprelay"
	"testing"
	"time"
)

func requestPacket() []byte {
	p := make([]byte, 36)
	p[0] = 0x45
	p[8] = 1
	p[9] = 1
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
	copy(p[12:16], []byte{10, 254, 254, 1})
	copy(p[16:20], []byte{1, 1, 1, 1})
	p[20] = 8
	p[25] = 7
	p[27] = 9
	copy(p[28:], []byte("testdata"))
	binary.BigEndian.PutUint16(p[22:24], icmprelay.Checksum(p[20:]))
	binary.BigEndian.PutUint16(p[10:12], icmprelay.Checksum(p[:20]))
	return p
}
func TestICMPPacketThroughDevice(t *testing.T) {
	for _, router := range []bool{false, true} {
		dev, n, e := CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.55.0.2")}, nil, 1280)
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		defer dev.Close()
		request := requestPacket()
		result := make(chan []byte, 1)
		go func() { r, _ := n.ExchangeICMP(ctx, request); result <- r }()
		b := make([]byte, 1500)
		sizes := make([]int, 1)
		if _, e = dev.Read([][]byte{b}, sizes, 0); e != nil {
			t.Fatal(e)
		}
		wire := b[:sizes[0]]
		if wire[8] != 1 || string(wire[12:16]) != string([]byte{10, 55, 0, 2}) {
			t.Fatal("TTL/source not forwarded")
		}
		if _, _, ok := icmprelay.Parse(wire); !ok {
			t.Fatal("invalid forwarded request")
		}
		var response []byte
		if router {
			msg := append([]byte{11, 0, 0, 0, 0, 0, 0, 0}, wire[:28]...)
			binary.BigEndian.PutUint16(msg[2:4], icmprelay.Checksum(msg))
			response = icmprelay.RestoreResponse(wire, wire[20:], netip.MustParseAddr("10.55.0.1"), 61, msg)
		} else {
			msg := append([]byte(nil), wire[20:]...)
			msg[0] = 0
			msg[2] = 0
			msg[3] = 0
			binary.BigEndian.PutUint16(msg[2:4], icmprelay.Checksum(msg))
			response = icmprelay.RestoreResponse(wire, wire[20:], netip.MustParseAddr("1.1.1.1"), 47, msg)
		}
		if _, e = dev.Write([][]byte{response}, 0); e != nil {
			t.Fatal(e)
		}
		got := <-result
		if !icmprelay.ValidateResponse(request, got) {
			t.Fatal("response not restored")
		}
		if (!router && got[8] != 47) || (router && got[20] != 11) {
			t.Fatal("response TTL/type lost")
		}
	}
}
func TestICMPCancelAndClose(t *testing.T) {
	dev, n, _ := CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.55.0.2")}, nil, 1280)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := n.ExchangeICMP(ctx, requestPacket()); e == nil {
		t.Fatal("canceled accepted")
	}
	dev.Close()
	if _, e := n.ExchangeICMP(context.Background(), requestPacket()); e == nil {
		t.Fatal("closed accepted")
	}
}
