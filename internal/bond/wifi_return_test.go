package bond

import (
	"context"
	"testing"
	"time"
)

// A returned Wi-Fi path can answer tiny health packets but lose game datagrams.
// Keep the already working cellular path available throughout the experiment.
func TestReturningWifiDoesNotLoseUDPBeforeHedge(t *testing.T) {
	delivered := make(chan Record, 8)
	client := New(context.Background(), func(Record) bool { return true })
	server := New(context.Background(), func(r Record) bool { delivered <- r; return true })
	defer client.Close()
	defer server.Close()
	ca, cb, _ := pair(time.Millisecond)
	wa, wb, _ := pair(time.Millisecond)
	if err := client.AddPath("cell", ca); err != nil {
		t.Fatal(err)
	}
	if err := server.AddPath("cell", cb); err != nil {
		t.Fatal(err)
	}
	if err := client.Send(context.Background(), Record{Kind: Datagram, Flow: 1, Payload: make([]byte, 900)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("cellular baseline failed")
	}
	if err := client.AddPath("wifi", sizedDropPath{wa}); err != nil {
		t.Fatal(err)
	}
	if err := server.AddPath("wifi", wb); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	p := client.paths["wifi"]
	p.healthySince = time.Now().Add(-9 * time.Second)
	p.lastReply = time.Now()
	p.lastSend = time.Now()
	p.rtt = 100 * time.Millisecond
	p.variance = 50 * time.Millisecond
	chosen := client.preferred(time.Now())
	delay := hedge(p)
	client.mu.Unlock()
	t.Logf("return selects %s; hedge=%s; datagram lifetime=200ms", chosen, delay)
	if chosen != "wifi" {
		t.Fatal("fixture did not reproduce Wi-Fi return")
	}
	if err := client.Send(context.Background(), Record{Kind: Datagram, Flow: 1, Seq: 1, Payload: make([]byte, 900)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	case <-time.After(600 * time.Millisecond):
		t.Fatalf("UDP lost although LTE remains usable: %+v", client.Stats())
	}
}
