package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"quiclab/internal/icmprelay"
	"testing"
	"time"
)

func echoPacket(ttl byte) []byte {
	p := make([]byte, 32)
	p[0] = 0x45
	p[8] = ttl
	p[9] = 1
	p[12] = 10
	p[15] = 1
	copy(p[16:20], []byte{1, 1, 1, 0})
	binary.BigEndian.PutUint16(p[2:4], 32)
	p[20] = 8
	p[25] = 11
	p[27] = 12
	copy(p[28:], "ping")
	binary.BigEndian.PutUint16(p[22:24], icmprelay.Checksum(p[20:]))
	binary.BigEndian.PutUint16(p[10:12], icmprelay.Checksum(p[:20]))
	return p
}
func TestICMPv2TTLAndRouterResponse(t *testing.T) {
	s, _ := New("1.1.1.0/24", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.ProbeICMP = func(ctx context.Context, p []byte) ([]byte, error) {
		if p[8] != 1 {
			t.Error("TTL lost")
		}
		msg := append(make([]byte, 8), p[:28]...)
		msg[0] = 11
		binary.BigEndian.PutUint16(msg[2:4], icmprelay.Checksum(msg))
		return icmprelay.RestoreResponse(p, p[20:], netip.MustParseAddr("10.8.0.1"), 55, msg), nil
	}
	open := func(ctx context.Context, kind, address string) (Stream, error) {
		a, b := net.Pipe()
		go s.handle(ctx, pipeStream{a}, "test", s.Log)
		return Open(ctx, func(context.Context) (Stream, error) { return pipeStream{b}, nil }, kind, address)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, e := ExchangeICMP(ctx, open, echoPacket(1))
	if e != nil || len(r) < 28 || r[20] != 11 || r[8] != 55 {
		t.Fatalf("%x %v", r, e)
	}
}
func TestICMPLegacyFallbackOnlyForOrdinaryTTL(t *testing.T) {
	for _, ttl := range []byte{1, 30, 128} {
		calls := 0
		open := func(ctx context.Context, kind, address string) (Stream, error) {
			calls++
			if kind == "icmp-echo-v2" {
				return nil, errors.New("unsupported network")
			}
			a, b := net.Pipe()
			go func() {
				defer a.Close()
				q, e := ReadPacket(a, 1280)
				if e != nil {
					return
				}
				q[0] = 0
				q[2] = 0
				q[3] = 0
				binary.BigEndian.PutUint16(q[2:4], icmprelay.Checksum(q))
				WritePacket(a, q)
			}()
			return pipeStream{b}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		r, e := ExchangeICMP(ctx, open, echoPacket(ttl))
		cancel()
		if ttl < 64 {
			if calls != 1 || e == nil {
				t.Fatal("traceroute fell back")
			}
		} else if e != nil || calls != 2 || r[20] != 0 {
			t.Fatal("legacy ping failed", e)
		}
	}
}
func TestICMPv2NeverBypassesCustomExit(t *testing.T) {
	s, _ := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.DialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("unexpected dial")
		return nil, io.EOF
	}
	a, b := net.Pipe()
	defer b.Close()
	b.SetDeadline(time.Now().Add(time.Second))
	go s.handle(context.Background(), pipeStream{a}, "test", s.Log)
	WriteJSON(b, Request{Network: "icmp-echo-v2", Address: "1.1.1.0"})
	var r Reply
	ReadJSON(b, &r)
	if r.Error != "ICMP TTL unavailable" {
		t.Fatal(r)
	}
}
