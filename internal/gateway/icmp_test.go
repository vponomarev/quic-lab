package gateway

import (
	"context"
	"encoding/binary"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"io"
	"log/slog"
	"net"
	"quiclab/internal/icmprelay"
	"testing"
	"time"
)

func TestICMPEchoUsesExitAndRealReply(t *testing.T) {
	s, _ := New("1.1.1.0/24", slog.New(slog.NewTextHandler(io.Discard, nil)))
	called := make(chan string, 1)
	s.DialContext = func(ctx context.Context, n, a string) (net.Conn, error) {
		called <- n + " " + a
		c, p := net.Pipe()
		go func() {
			defer p.Close()
			b := make([]byte, 1500)
			n, e := p.Read(b)
			if e != nil {
				return
			}
			m, e := icmp.ParseMessage(1, b[:n])
			if e != nil {
				return
			}
			m.Type = ipv4.ICMPTypeEchoReply
			r, _ := m.Marshal(nil)
			// net.IPConn.Read includes the IPv4 header on Linux raw sockets.
			ipPacket := make([]byte, 20+len(r))
			ipPacket[0] = 0x45
			ipPacket[8] = 64
			ipPacket[9] = 1
			copy(ipPacket[12:16], []byte{1, 1, 1, 0})
			copy(ipPacket[16:20], []byte{10, 8, 0, 9})
			binary.BigEndian.PutUint16(ipPacket[2:4], uint16(len(ipPacket)))
			binary.BigEndian.PutUint16(ipPacket[10:12], icmprelay.Checksum(ipPacket[:20]))
			copy(ipPacket[20:], r)
			p.Write(ipPacket)
		}()
		return c, nil
	}
	a, b := net.Pipe()
	defer b.Close()
	b.SetDeadline(time.Now().Add(time.Second))
	go s.handle(context.Background(), pipeStream{a}, "test", s.Log)
	WriteJSON(b, Request{Network: "icmp-echo-v1", Address: "1.1.1.0"})
	var r Reply
	if e := ReadJSON(b, &r); e != nil || r.Error != "" {
		t.Fatalf("handshake %v %+v", e, r)
	}
	q, _ := (&icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: 19, Seq: 25, Data: []byte("real reply")}}).Marshal(nil)
	WritePacket(b, q)
	p, e := ReadPacket(b, 1280)
	if e != nil {
		t.Fatal(e)
	}
	m, _ := icmp.ParseMessage(1, p)
	echo := m.Body.(*icmp.Echo)
	if m.Type != ipv4.ICMPTypeEchoReply || echo.ID != 19 || echo.Seq != 25 || string(echo.Data) != "real reply" {
		t.Fatal("incorrect response")
	}
	if got := <-called; got != "ip4:icmp 1.1.1.0" {
		t.Fatal(got)
	}
}
func TestICMPEchoDeniedBeforeDial(t *testing.T) {
	s, _ := New("1.1.1.0/24", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.DialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("denied address dialed")
		return nil, io.EOF
	}
	a, b := net.Pipe()
	defer b.Close()
	b.SetDeadline(time.Now().Add(time.Second))
	go s.handle(context.Background(), pipeStream{a}, "test", s.Log)
	WriteJSON(b, Request{Network: "icmp-echo-v1", Address: "127.0.0.1"})
	var r Reply
	ReadJSON(b, &r)
	if r.Error != "destination denied" {
		t.Fatalf("%+v", r)
	}
}

func TestICMPEchoNeverConfirmsWrongReply(t *testing.T) {
	s, _ := New("1.1.1.0/24", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.DialContext = func(ctx context.Context, n, a string) (net.Conn, error) {
		c, p := net.Pipe()
		go func() {
			defer p.Close()
			b := make([]byte, 1500)
			n, _ := p.Read(b)
			m, _ := icmp.ParseMessage(1, b[:n])
			m.Type = ipv4.ICMPTypeEchoReply
			m.Body.(*icmp.Echo).Seq++
			r, _ := m.Marshal(nil)
			p.Write(r)
		}()
		return c, nil
	}
	a, b := net.Pipe()
	defer b.Close()
	b.SetDeadline(time.Now().Add(time.Second))
	go s.handle(context.Background(), pipeStream{a}, "test", s.Log)
	WriteJSON(b, Request{Network: "icmp-echo-v1", Address: "1.1.1.0"})
	var r Reply
	ReadJSON(b, &r)
	q, _ := (&icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: 19, Seq: 25, Data: []byte("real reply")}}).Marshal(nil)
	WritePacket(b, q)
	if p, e := ReadPacket(b, 1280); e == nil {
		t.Fatalf("fabricated reply %x", p)
	}
}
