package gateway

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"os"
	"quiclab/internal/research"
	"testing"
	"time"
)

func researchHelloFixture(ech bool) []byte {
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

func TestServerResearchTLSMetadataAndRevocation(t *testing.T) {
	var records []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(rec research.Record) bool { records = append(records, rec); return true })
	ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	grant := func(gen uint64, on bool) {
		r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: gen, Enabled: on})
	}
	grant(1, true)
	f := &observedConnection{receiver: r, user: "u", device: "d", network: "tcp4", destination: "1.2.3.4:443", ref: ref, id: 1}
	f.ready.Store(true)
	r.connections[f] = struct{}{}
	h := researchHelloFixture(true)
	for _, b := range h {
		f.payload([]byte{b}, true)
	}
	f.flush(false)
	var d research.FlowData
	json.Unmarshal(records[len(records)-1].Data, &d)
	if d.TLS == nil || !d.TLS.ECHOffered || d.TLS.ServerName != "api.example.org" || d.BytesUp != uint64(len(h)) {
		t.Fatalf("%+v", d)
	}
	grant(2, false)
	if f.tls != nil {
		t.Fatal("retained after revoke")
	}
	grant(3, true)
	f.payload(h, true)
	f.flush(false)
	json.Unmarshal(records[len(records)-1].Data, &d)
	if d.TLS.State != "not_observed" {
		t.Fatalf("reinterpreted midstream: %+v", d.TLS)
	}
	_ = time.Now()
}

func TestServerResearchQUICMetadata(t *testing.T) {
	for _, name := range []string{"quic-v1", "quic-v2", "quic-ech"} {
		t.Run(name, func(t *testing.T) {
			packet, err := os.ReadFile("../netobserve/testdata/" + name + ".bin")
			if err != nil {
				t.Fatal(err)
			}
			var records []research.Record
			r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(rec research.Record) bool { records = append(records, rec); return true })
			ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
			r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true})
			f := &observedConnection{receiver: r, user: "u", device: "d", network: "udp4", destination: "1.2.3.4:18456", ref: ref, id: 1}
			f.ready.Store(true)
			r.connections[f] = struct{}{}
			f.payload(packet, true)
			f.flush(false)
			var d research.FlowData
			json.Unmarshal(records[len(records)-1].Data, &d)
			want := "QUIC v1"
			if name == "quic-v2" {
				want = "QUIC v2"
			}
			if d.Detected == nil || d.Detected.Name != want || d.TLS == nil || d.TLS.ServerName != "api.example.org" || d.TLS.ECHOffered != (name == "quic-ech") || d.BytesUp != uint64(len(packet)) {
				t.Fatalf("metadata: %+v", d)
			}
			r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 2, Enabled: false})
			if f.tls != nil {
				t.Fatal("retained after revoke")
			}
		})
	}
}
