package mobile

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"quiclab/internal/gateway"
	"quiclab/internal/research"
	"testing"
	"time"
)

func TestResearchTLSPrefixRevocation(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	c := researchTestCollector(t, o, func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true}, nil
	})
	c.sync(time.Now())
	f := c.openFlow(6, "10.0.0.2", 1000, "1.2.3.4", 443)
	f.tlsPayload([]byte{22, 3})
	c.mu.Lock()
	if f.tls.Info().State != "incomplete" {
		t.Fatal("prefix")
	}
	c.mu.Unlock()
	o.allowed.Store(false)
	c.changed()
	c.mu.Lock()
	if f.tls != nil {
		t.Fatal("prefix retained after revoke")
	}
	c.mu.Unlock()
	o.allowed.Store(true)
	c.sync(time.Now())
	f.tlsPayload([]byte{22, 3, 3, 0, 4, 2, 0, 0, 0})
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flowRecordLocked(f, "flow_delta", time.Now())
	var d research.FlowData
	json.Unmarshal(c.queue[len(c.queue)-1].record.Data, &d)
	if d.TLS == nil || d.TLS.State != "not_observed" {
		t.Fatalf("late consent: %+v", d.TLS)
	}
}
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

func TestResearchTLSWireMetadata(t *testing.T) {
	for _, ech := range []bool{false, true} {
		o := &researchTestObserver{}
		o.allowed.Store(true)
		c := researchTestCollector(t, o, func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error) {
			return gateway.ResearchReply{Allowed: true}, nil
		})
		c.sync(time.Now())
		f := c.openFlow(6, "10.0.0.2", 1000, "1.2.3.4", 443)
		h := researchHelloFixture(ech)
		for _, b := range h {
			f.tlsPayload([]byte{b})
		}
		f.add(uint64(len(h)), 0, 0, 0)
		c.mu.Lock()
		c.flowRecordLocked(f, "flow_delta", time.Now())
		var d research.FlowData
		json.Unmarshal(c.queue[len(c.queue)-1].record.Data, &d)
		c.mu.Unlock()
		if d.TLS == nil || d.TLS.ServerName != "api.example.org" || d.TLS.ECHOffered != ech || d.BytesUp != uint64(len(h)) {
			t.Fatalf("%+v", d)
		}
	}
}

func TestResearchClosedPendingTLSRevoke(t *testing.T) {
	o := &researchTestObserver{gate: make(chan struct{})}
	o.allowed.Store(true)
	defer close(o.gate)
	c := researchTestCollector(t, o, func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true}, nil
	})
	c.sync(time.Now())
	f := c.openFlow(6, "10.0.0.2", 1000, "1.2.3.4", 443)
	f.tlsPayload([]byte{22, 3})
	f.close()
	o.allowed.Store(false)
	c.changed()
	c.mu.Lock()
	defer c.mu.Unlock()
	if f.tls != nil {
		t.Fatal("closed flow retained TLS prefix awaiting owner after revocation")
	}
}
