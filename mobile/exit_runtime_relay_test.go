package mobile

import (
	"encoding/json"
	"quiclab/internal/vpnmodel"
	"testing"
	"time"
)

func relayRuntimeConfig(t *testing.T) string {
	t.Helper()
	var c exitRuntimeConfig
	json.Unmarshal([]byte(runtimeConfig(true, "")), &c)
	var sample gatewayConfig
	json.Unmarshal([]byte(vlessGatewayJSON(t)), &sample)
	c.Model.Profiles = nil
	c.Gateways = map[string]gatewayConfig{}
	for i, id := range []string{"relay1", "relay2", "relay3"} {
		c.Model.Profiles = append(c.Model.Profiles, vpnmodel.Profile{ID: id, ExitID: "exit", Transport: "https", Mode: "auto", Endpoint: "gateway.invalid:443", PoolSize: 1, Priority: i})
		c.Gateways[id] = gatewayConfig{Transport: "https", Endpoint: "gateway.invalid:443", UnderlayVLESSConfig: sample.VLESSConfig, UnderlayEndpoint: "127.0.0.1:443"}
	}
	raw, _ := json.Marshal(c)
	return string(raw)
}
func TestRelayPoolTwoDistinctAndReplacement(t *testing.T) {
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("relay", 0)
	r.SetTrafficBudget(budget)
	if e := r.Start(relayRuntimeConfig(t), nil); e != nil {
		t.Fatal(e)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
	time.Sleep(250 * time.Millisecond)
	ps := b.Stats().Paths
	if len(ps) != 2 || ps[0].ProfileID == ps[1].ProfileID {
		t.Fatal("distinct bounded pool", ps)
	}
	b.mu.Lock()
	b.fail = map[string]bool{"relay1": true}
	for id, p := range b.paths {
		if p.ProfileID == "relay1" {
			delete(b.paths, id)
		}
	}
	b.mu.Unlock()
	awaitRuntime(t, func() bool {
		ps := b.Stats().Paths
		seen := map[string]bool{}
		for _, p := range ps {
			seen[p.ProfileID] = true
		}
		return len(ps) == 2 && seen["relay2"] && seen["relay3"]
	})
	time.Sleep(250 * time.Millisecond)
	if len(b.Stats().Paths) != 2 {
		t.Fatal("exceeded two paths")
	}
}
func TestRelayPoolRejectMixedDirect(t *testing.T) {
	var c exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &c)
	g := c.Gateways["relay2"]
	g.UnderlayVLESSConfig = ""
	g.UnderlayEndpoint = ""
	c.Gateways["relay2"] = g
	raw, _ := json.Marshal(c)
	r := NewExitRuntime(nil)
	r.backend = &runtimeTransport{}
	budget, _ := NewTrafficBudget("relay", 0)
	r.SetTrafficBudget(budget)
	defer r.Stop()
	if e := r.Start(string(raw), nil); e == nil {
		t.Fatal("accepted direct fallback inside relay exit")
	}
}

func TestRelayPoolAllFailedBoundedAndStop(t *testing.T) {
	b := &runtimeTransport{fail: map[string]bool{"relay1": true, "relay2": true, "relay3": true}}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("relay", 0)
	r.SetTrafficBudget(budget)
	if e := r.Start(relayRuntimeConfig(t), nil); e != nil {
		t.Fatal(e)
	}
	awaitRuntime(t, func() bool { n, _ := b.counts(); return n >= 3 })
	time.Sleep(250 * time.Millisecond)
	n, _ := b.counts()
	if n > 6 {
		t.Fatal("unbounded retry", n)
	}
	r.Stop()
	n, _ = b.counts()
	time.Sleep(150 * time.Millisecond)
	after, _ := b.counts()
	if after != n {
		t.Fatal("dial after Stop")
	}
}
func TestRelayPoolStopCancelsPending(t *testing.T) {
	b := &runtimeTransport{hold: true, entered: make(chan struct{}, 1)}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("relay", 0)
	r.SetTrafficBudget(budget)
	if e := r.Start(relayRuntimeConfig(t), nil); e != nil {
		t.Fatal(e)
	}
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("no dial")
	}
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel")
	}
	if len(b.Stats().Paths) != 0 {
		t.Fatal("late admission after Stop")
	}
}
func TestRelayPoolOneAvailableCandidate(t *testing.T) {
	b := &runtimeTransport{fail: map[string]bool{"relay2": true, "relay3": true}}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("relay", 0)
	r.SetTrafficBudget(budget)
	if e := r.Start(relayRuntimeConfig(t), nil); e != nil {
		t.Fatal(e)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { n, _ := b.counts(); return n >= 3 })
	ps := b.Stats().Paths
	if len(ps) != 1 || ps[0].ProfileID != "relay1" {
		t.Fatal("useful single path lost", ps)
	}
}

func TestRelayNetworkEndpointsAreGenerationBound(t *testing.T) {
	var c exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &c)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("relay", 0)
	r.SetTrafficBudget(budget)
	raw, _ := json.Marshal(c)
	if e := r.Start(string(raw), nil); e != nil {
		t.Fatal(e)
	}
	defer r.Stop()
	if e := r.UpdateNetwork(`{"network":"wifi","available":true,"allowed":true,"generation":2,"underlay_endpoints":{"relay1":"127.0.0.2:443"}}`, nil); e != nil {
		t.Fatal(e)
	}
	r.mu.Lock()
	got := r.gatewayForNetwork("relay1", "wifi").UnderlayEndpoint
	missing := r.gatewayForNetwork("relay2", "wifi").UnderlayEndpoint
	r.mu.Unlock()
	if got != "127.0.0.2:443" || missing != "" {
		t.Fatal("used stale network resolution", got, missing)
	}
	if e := r.UpdateNetwork(`{"network":"wifi","available":true,"allowed":true,"generation":2,"underlay_endpoints":{"relay1":"127.0.0.3:443"}}`, nil); e == nil {
		t.Fatal("resolution changed within generation")
	}
	if e := r.UpdateNetwork(`{"network":"wifi","available":true,"allowed":true,"generation":3,"underlay_endpoints":{"relay1":"unresolved.invalid:443"}}`, nil); e == nil {
		t.Fatal("accepted system DNS fallback")
	}
}

func TestRelayPoolRejectDifferentExit(t *testing.T) {
	for _, field := range []string{"endpoint", "server_name", "control_url"} {
		t.Run(field, func(t *testing.T) {
			var c exitRuntimeConfig
			json.Unmarshal([]byte(relayRuntimeConfig(t)), &c)
			g := c.Gateways["relay2"]
			switch field {
			case "endpoint":
				g.Endpoint = "other.invalid:443"
				c.Model.Profiles[1].Endpoint = g.Endpoint
			case "server_name":
				g.ServerName = "other.invalid"
			case "control_url":
				g.ControlURL = "https://other.invalid/api/v1/capabilities"
			}
			c.Gateways["relay2"] = g
			raw, _ := json.Marshal(c)
			r := NewExitRuntime(nil)
			r.backend = &runtimeTransport{}
			budget, _ := NewTrafficBudget("relay", 0)
			r.SetTrafficBudget(budget)
			defer r.Stop()
			if e := r.Start(string(raw), nil); e == nil {
				t.Fatal("accepted multiple logical gateways")
			}
		})
	}
}
