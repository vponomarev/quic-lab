package netobserve

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func helloFixture(ech bool) []byte {
	ext := func(id uint16, b []byte) []byte {
		out := binary.BigEndian.AppendUint16(nil, id)
		out = binary.BigEndian.AppendUint16(out, uint16(len(b)))
		return append(out, b...)
	}
	name := []byte("api.example.org")
	item := append([]byte{0, 0, byte(len(name))}, name...)
	sni := binary.BigEndian.AppendUint16(nil, uint16(len(item)))
	sni = append(sni, item...)
	exts := ext(0, sni)
	exts = append(exts, ext(16, []byte{0, 3, 2, 'h', '2'})...)
	exts = append(exts, ext(43, []byte{4, 3, 4, 3, 3})...)
	if ech {
		exts = append(exts, ext(0xfe0d, []byte{0, 1, 2, 3})...)
	}
	b := append([]byte{3, 3}, make([]byte, 32)...)
	b = append(b, 0, 0, 2, 0x13, 1, 1, 0)
	b = binary.BigEndian.AppendUint16(b, uint16(len(exts)))
	b = append(b, exts...)
	h := append([]byte{1, byte(len(b) >> 16), byte(len(b) >> 8), byte(len(b))}, b...)
	rec := func(x []byte) []byte { return append([]byte{22, 3, 1, byte(len(x) >> 8), byte(len(x))}, x...) }
	// Handshake split across TLS records, then the transport delivers one byte at a time.
	return append(rec(h[:31]), rec(h[31:])...)
}
func TestTLSClientHelloFragmentsAndECH(t *testing.T) {
	for _, ech := range []bool{false, true} {
		p := NewTLSProbe(false)
		for _, b := range helloFixture(ech) {
			p.Feed([]byte{b})
		}
		v := p.Info()
		if v.State != "observed" || v.ServerName != "api.example.org" || v.ECHOffered != ech || len(v.ALPN) != 1 || v.ALPN[0] != "h2" || len(v.OfferedVersions) != 2 || v.OfferedVersions[0] != "TLS 1.3" {
			t.Fatalf("%+v", v)
		}
		if len(p.record) != 0 || len(p.handshake) != 0 {
			t.Fatal("raw hello retained")
		}
		p.Feed(bytes.Repeat([]byte{7}, 1<<20))
		if p.Info().ServerName != v.ServerName {
			t.Fatal("parsed application payload")
		}
	}
}
func TestTLSProbeBoundaries(t *testing.T) {
	p := NewTLSProbe(false)
	p.Feed([]byte("GET / HTTP/1.1\r\n"))
	if p.Info().State != "not_tls" {
		t.Fatal(p.Info())
	}
	p = NewTLSProbe(true)
	p.Feed(helloFixture(false))
	if p.Info().State != "not_observed" {
		t.Fatal(p.Info())
	}
	p = NewTLSProbe(false)
	p.Feed(helloFixture(false)[:12])
	if p.Info().State != "incomplete" {
		t.Fatal(p.Info())
	}
	p = NewTLSProbe(false)
	p.Feed([]byte{22, 3, 3, 0xff, 0xff})
	if p.Info().State != "limit" {
		t.Fatal(p.Info())
	}
	p = NewTLSProbe(false)
	h := helloFixture(false)
	h[5] = 2
	p.Feed(h)
	if p.Info().State != "not_observed" {
		t.Fatal(p.Info())
	}
}
func FuzzTLSProbe(f *testing.F) {
	f.Add(helloFixture(true))
	f.Add([]byte("GET /"))
	f.Fuzz(func(t *testing.T, b []byte) {
		p := NewTLSProbe(false)
		for len(b) > 0 {
			n := min(17, len(b))
			p.Feed(b[:n])
			b = b[n:]
		}
		v := p.Info()
		if len(v.ServerName) > 253 || len(p.record)+len(p.handshake) > 2*MaxTLSHelloBytes {
			t.Fatal("bound")
		}
	})
}

func TestTLSRealClientHello(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	a.SetDeadline(time.Now().Add(time.Second))
	b.SetDeadline(time.Now().Add(time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		c := tls.Client(a, &tls.Config{ServerName: "real.example.org", NextProtos: []string{"h2", "http/1.1"}})
		_ = c.Handshake()
	}()
	h := make([]byte, 5)
	if _, e := io.ReadFull(b, h); e != nil {
		t.Fatal(e)
	}
	payload := make([]byte, int(binary.BigEndian.Uint16(h[3:])))
	if _, e := io.ReadFull(b, payload); e != nil {
		t.Fatal(e)
	}
	b.Close()
	<-done
	p := NewTLSProbe(false)
	p.Feed(append(h, payload...))
	v := p.Info()
	if v.State != "observed" || v.ServerName != "real.example.org" || len(v.ALPN) != 2 {
		t.Fatalf("real hello: %+v", v)
	}
}
