package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"quiclab/internal/bond"
	"strings"
	"testing"
	"time"
)

type exitBondPath struct {
	ctx     context.Context
	in, out chan []byte
}

func (p *exitBondPath) SendDatagram(b []byte) error {
	select {
	case p.out <- append([]byte(nil), b...):
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}
func (p *exitBondPath) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case b := <-p.in:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	}
}
func (p *exitBondPath) Close() error { return nil }
func TestBondStreamsPreserveSelectedExitIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, _ := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Egress = func(cs tls.ConnectionState) (*Egress, error) {
		return &Egress{Resolver: &net.Resolver{}, DialContext: func(context.Context, string, string) (net.Conn, error) {
			a, b := net.Pipe()
			go func() { defer b.Close(); b.Write([]byte(cs.ServerName)) }()
			return a, nil
		}}, nil
	}
	cert := &x509.Certificate{Raw: []byte("bond-exit-device")}
	cs := tls.ConnectionState{ServerName: "west", PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	entry, _, err := s.joinBond(ctx, ctx, cs, BondHello{Token: strings.Repeat("ab", 32), Path: "wifi", Create: true}, "quic", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	defer entry.mux.Close()
	client := bond.NewMux(ctx, true)
	defer client.Close()
	a, b := make(chan []byte, 256), make(chan []byte, 256)
	if err = entry.mux.Session.AddPath("wifi", &exitBondPath{ctx, a, b}); err != nil {
		t.Fatal(err)
	}
	if err = client.Session.AddPath("wifi", &exitBondPath{ctx, b, a}); err != nil {
		t.Fatal(err)
	}
	stream, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(2 * time.Second))
	if err = WriteJSON(stream, Request{Network: "tcp", Address: "192.0.2.1:443"}); err != nil {
		t.Fatal(err)
	}
	var reply Reply
	if err = ReadJSON(stream, &reply); err != nil || reply.Error != "" {
		t.Fatal(reply, err)
	}
	raw := make([]byte, 4)
	if _, err = io.ReadFull(stream, raw); err != nil || string(raw) != "west" {
		t.Fatal("bond lost selected exit", string(raw), err)
	}
}
