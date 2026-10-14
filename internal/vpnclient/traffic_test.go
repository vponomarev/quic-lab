package vpnclient

import (
	"testing"
	"time"
)

func TestTrafficMeterRatesResetAndMalformed(t *testing.T) {
	var m TrafficMeter
	at := time.Unix(100, 0)
	m.Reset("a")
	m.Observe(`{"event":"traffic","tx_bytes":100,"rx_bytes":200}`, at)
	m.Observe(`{"event":"traffic","tx_bytes":500,"rx_bytes":800}`, at.Add(2*time.Second))
	s := m.Snapshot()
	if !s.Available || s.TxBytes != 500 || s.TxRate != 200 || s.RxRate != 300 {
		t.Fatal(s)
	}
	m.Observe(`{"event":"traffic","tx_bytes":-1,"rx_bytes":200}`, at.Add(3*time.Second))
	if m.Snapshot() != s {
		t.Fatal("invalid event changed counters")
	}
	m.Observe(`{"event":"traffic","tx_bytes":2,"rx_bytes":3}`, at.Add(4*time.Second))
	if m.Snapshot().TxRate != 0 {
		t.Fatal("counter reset underflow")
	}
	m.Reset("b")
	if m.Snapshot().Available || m.Snapshot().Session != "b" {
		t.Fatal("session reset")
	}
}
func TestBackendRuntimeIncludedInSnapshot(t *testing.T) {
	c := NewController(&infoBackend{})
	s := c.Snapshot()
	if s.Runtime == nil || s.Runtime.Mode != "fixture" {
		t.Fatal(s)
	}
}

type infoBackend struct{ fakeBackend }

func (*infoBackend) RuntimeInfo() RuntimeInfo { return RuntimeInfo{Mode: "fixture"} }

func TestAdditionalICMPTraffic(t *testing.T) {
	var m TrafficMeter
	m.Reset("a")
	at := time.Unix(100, 0)
	m.Observe(`{"event":"traffic","tx_bytes":0,"rx_bytes":0}`, at)
	m.Add(40, 40)
	m.Observe(`{"event":"traffic","tx_bytes":100,"rx_bytes":200}`, at.Add(time.Second))
	s := m.Snapshot()
	if s.TxBytes != 140 || s.RxBytes != 240 || s.TxRate != 140 || s.RxRate != 240 {
		t.Fatal(s)
	}
	m.Reset("b")
	m.Observe(`{"event":"traffic","tx_bytes":0,"rx_bytes":0}`, at)
	if m.Snapshot().TxBytes != 0 {
		t.Fatal("ICMP counters leaked across session")
	}
}
