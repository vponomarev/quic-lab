package netobserve

import (
	"encoding/binary"
	"testing"
)

func TestProtocolEvidenceAndNoPortGuessing(t *testing.T) {
	p := NewProtocolProbe(6, 443, false)
	for _, b := range []byte("GET / HTTP/1.1\r\n") {
		p.Feed([]byte{b})
	}
	if p.Detected() == nil || p.Detected().Name != "HTTP" {
		t.Fatal("HTTP request line not detected")
	}
	p = NewProtocolProbe(6, 443, false)
	p.Feed([]byte("random data"))
	if p.Detected() != nil {
		t.Fatal("port guessed as HTTPS")
	}
	p = NewProtocolProbe(6, 443, false)
	for _, b := range helloFixture(true) {
		p.Feed([]byte{b})
	}
	if p.Detected() == nil || p.Detected().Name != "TLS" || !p.Info().ECHOffered {
		t.Fatal("TLS evidence lost")
	}
	p = NewProtocolProbe(6, 443, true)
	p.Feed(helloFixture(false))
	if p.Detected() != nil || p.Info().State != "not_observed" {
		t.Fatal("late stream falsely classified")
	}
}
func TestProtocolDNSBoundsAndValidation(t *testing.T) {
	b := []byte{1, 2, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 'a', 0, 0, 1, 0, 1}
	for _, proto := range []int64{6, 17} {
		p := NewProtocolProbe(proto, 53, false)
		raw := b
		if proto == 6 {
			raw = append(binary.BigEndian.AppendUint16(nil, uint16(len(b))), b...)
		}
		if proto == 6 {
			for _, b := range raw {
				p.Feed([]byte{b})
			}
		} else {
			p.Feed(raw)
		}
		if p.Detected() == nil || p.Detected().Name != "DNS" {
			t.Fatalf("DNS proto %d", proto)
		}
	}
	p := NewProtocolProbe(17, 53, false)
	p.Feed([]byte("not dns"))
	if p.Detected() != nil {
		t.Fatal("port guessed DNS")
	}
	p = NewProtocolProbe(6, 80, false)
	p.Feed(make([]byte, 1<<20))
	if len(p.prefix) > 4096 {
		t.Fatal("unbounded prefix")
	}
	if (&ProtocolInfo{Name: "invented", Evidence: "port"}).Validate() == nil {
		t.Fatal("unknown classification accepted")
	}
}

func TestProtocolQUICPacketLimitKeepsMetadata(t *testing.T) {
	p := NewProtocolProbe(17, 443, false)
	first := qpacket(1, 0, qcrypto(0, qhello(false)[:8]))
	p.Feed(first)
	for i := 0; i < 65; i++ {
		p.Feed(first)
	}
	if p.Detected() == nil || p.Info() == nil || p.Info().State != "limit" {
		t.Fatal("lost QUIC resource-limit evidence")
	}
}
