package icmprelay

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func packet() []byte {
	b := make([]byte, 32)
	b[0] = 0x45
	binary.BigEndian.PutUint16(b[2:4], 32)
	b[8] = 128
	b[9] = 1
	copy(b[12:16], []byte{10, 254, 253, 1})
	copy(b[16:20], []byte{1, 1, 1, 0})
	b[20] = 8
	b[25] = 7
	b[27] = 9
	copy(b[28:], "ping")
	binary.BigEndian.PutUint16(b[22:24], Checksum(b[20:]))
	binary.BigEndian.PutUint16(b[10:12], Checksum(b[:20]))
	return b
}
func TestRelayRealReplyAndNoFabrication(t *testing.T) {
	for _, fail := range []bool{false, true} {
		replies := make(chan []byte, 1)
		called := make(chan struct{}, 1)
		r := New(func(ctx context.Context, d netip.Addr, q []byte) ([]byte, error) {
			called <- struct{}{}
			if fail {
				return nil, errors.New("timeout")
			}
			if d.String() != "1.1.1.0" {
				t.Error(d)
			}
			p := append([]byte(nil), q...)
			p[0] = 0
			p[2] = 0
			p[3] = 0
			binary.BigEndian.PutUint16(p[2:4], Checksum(p))
			return p, nil
		}, func(p []byte) error { replies <- p; return nil })
		if !r.Handle(packet()) {
			t.Fatal("not consumed")
		}
		<-called
		r.Close()
		select {
		case b := <-replies:
			if fail || b[20] != 0 || Checksum(b[:20]) != 0 || Checksum(b[20:]) != 0 || b[25] != 7 || b[27] != 9 {
				t.Fatal("bad reply")
			}
		default:
			if !fail {
				t.Fatal("missing reply")
			}
		}
	}
}
func TestRejectMalformedAndNonEcho(t *testing.T) {
	b := packet()
	for _, change := range []func([]byte){func(b []byte) { b[20] = 3 }, func(b []byte) { b[6] = 0x20 }, func(b []byte) { b[3] = 0 }, func(b []byte) { b[0] = 0x4f }, func(b []byte) { b[31] ^= 1 }} {
		p := append([]byte(nil), b...)
		change(p)
		if _, _, ok := Parse(p); ok {
			t.Fatal("accepted malformed")
		}
	}
	if _, _, ok := Parse(b); !ok {
		t.Fatal("valid echo rejected")
	}
}
func TestCloseCancelsWorkers(t *testing.T) {
	started := make(chan struct{}, 1)
	r := New(func(ctx context.Context, _ netip.Addr, _ []byte) ([]byte, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}, func([]byte) error { t.Error("unexpected reply"); return nil })
	r.Handle(packet())
	<-started
	done := make(chan struct{})
	go func() { r.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close blocked")
	}
}
