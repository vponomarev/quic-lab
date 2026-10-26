package mobile

import (
	"encoding/json"
	"quiclab/internal/bond"
	"quiclab/internal/trafficbudget"
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
func TestRelayPoolRejectDifferentDirectExit(t *testing.T) {
	var c exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &c)
	g := c.Gateways["relay2"]
	g.UnderlayVLESSConfig = ""
	g.UnderlayEndpoint = ""
	g.Endpoint = "different.invalid:443"
	c.Model.Profiles[1].Endpoint = g.Endpoint
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

func TestRelayLateDNSAddsReserveWithoutReplacingPrimary(t *testing.T) {
	var c exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &c)
	c.InitialNetwork.UnderlayEndpoints = map[string]string{"relay1": "127.0.0.1:443"}
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("relay", 0)
	r.SetTrafficBudget(budget)
	raw, _ := json.Marshal(c)
	if err := r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 1 })
	primary := b.Stats().Paths[0].Name
	c.InitialNetwork.UnderlayEndpoints["relay2"] = "127.0.0.2:443"
	raw, _ = json.Marshal(c.InitialNetwork)
	if err := r.UpdateNetwork(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
	found := false
	for _, p := range b.Stats().Paths {
		if p.Name == primary {
			found = true
		}
	}
	if !found {
		t.Fatal("primary replaced by late DNS result")
	}
}

func TestRelayDisabledCellReserveStillAllowsRecovery(t *testing.T) {
	var c exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &c)
	c.Economy = false
	c.InitialNetwork.UnderlayEndpoints = map[string]string{"relay1": "127.0.0.1:443"}
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
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 1 && b.Stats().Paths[0].Ready })
	if e := r.UpdateNetwork(`{"network":"cell","available":true,"allowed":true,"reserve_check_allowed":false,"generation":1,"underlay_endpoints":{"relay2":"127.0.0.2:443"}}`, nil); e != nil {
		t.Fatal(e)
	}
	time.Sleep(300 * time.Millisecond)
	if len(b.Stats().Paths) != 1 {
		t.Fatal("disabled cellular reserve was warmed")
	}
	var snapshot map[string]any
	json.Unmarshal([]byte(r.Snapshot()), &snapshot)
	if snapshot["needs_cellular"] == true {
		t.Fatal("requested disallowed warm LTE")
	}
	if e := r.UpdateNetwork(`{"network":"wifi","available":false,"allowed":true,"generation":2,"underlay_endpoints":{}}`, nil); e != nil {
		t.Fatal(e)
	}
	awaitRuntime(t, func() bool { ps := b.Stats().Paths; return len(ps) == 1 && ps[0].Network == "cell" })
}

func TestRelayReservePermissionKeepsActiveNetwork(t *testing.T) {
	no := false
	r := NewExitRuntime(nil)
	r.cfg.relay = true
	r.networks = map[string]runtimeNetwork{"wifi": {ReserveCheckAllowed: &no}, "cell": {ReserveCheckAllowed: &no}}
	r.paths = map[string]*runtimePath{"active": {info: bond.PathInfo{ID: "active", Network: "cell"}, ready: true}}
	r.preferred = "active"
	if r.relayWarmAllowed("wifi") {
		t.Fatal("disabled Wi-Fi reserve admitted")
	}
	if !r.relayWarmAllowed("cell") {
		t.Fatal("active LTE interrupted by reserve permission")
	}
}

func TestRelayLogicalReservePrefersAnotherConnection(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(relayRuntimeConfig(t)), &raw); err != nil {
		t.Fatal(err)
	}
	raw["logical_connections"] = map[string]string{"relay1": "subscription-a", "relay2": "subscription-a", "relay3": "subscription-b"}
	encoded, _ := json.Marshal(raw)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("logical", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(string(encoded), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
	seen := map[string]bool{}
	for _, p := range b.Stats().Paths {
		seen[p.ProfileID] = true
	}
	if !seen["relay1"] || !seen["relay3"] {
		t.Fatalf("reserve should use another logical connection: %v", seen)
	}
}

func TestRelayLogicalReserveFallsBackWithinConnection(t *testing.T) {
	var raw map[string]any
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &raw)
	raw["logical_connections"] = map[string]string{"relay1": "a", "relay2": "a", "relay3": "b"}
	encoded, _ := json.Marshal(raw)
	b := &runtimeTransport{fail: map[string]bool{"relay3": true}}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("logical-fallback", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(string(encoded), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
	seen := map[string]bool{}
	for _, p := range b.Stats().Paths {
		seen[p.ProfileID] = true
	}
	if !seen["relay1"] || !seen["relay2"] {
		t.Fatal(seen)
	}
}

func TestRelayLogicalHealthyPrimaryRetained(t *testing.T) {
	r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "first", Mode: "auto", PoolSize: 1}, {ID: "second", Mode: "auto", PoolSize: 1, Priority: 1}})
	r.cfg.relay = true
	now := time.Now()
	addPolicyPath(t, r, "first", 1, now)
	primary := addPolicyPath(t, r, "second", 1, now)
	r.preferred = primary.info.ID
	r.refresh(now)
	if r.preferred != primary.info.ID {
		t.Fatal("healthy primary replaced by list priority", r.preferred)
	}
}

func TestRelayLogicalSustainedDegradationSwitchesButSpikeDoesNot(t *testing.T) {
	r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "first", Mode: "auto", PoolSize: 1}, {ID: "second", Mode: "auto", PoolSize: 1, Priority: 1}})
	r.cfg.relay = true
	now := time.Now()
	primary := addPolicyPath(t, r, "first", 1, now)
	reserve := addPolicyPath(t, r, "second", 1, now)
	r.preferred = primary.info.ID
	b := r.backend.(*runtimeTransport)
	observe := func(at time.Time, rtt float64) {
		for id, s := range b.paths {
			s.DataProbes++
			if id == primary.info.ID {
				s.RTTMS = rtt
			} else {
				s.RTTMS = 50
			}
			b.paths[id] = s
		}
		r.refresh(at)
	}
	observe(now, 900)
	observe(now.Add(7*time.Second), 900)
	if r.preferred != primary.info.ID {
		t.Fatal("switched before sustained window")
	}
	observe(now.Add(8*time.Second), 100)
	observe(now.Add(9*time.Second), 900)
	observe(now.Add(16*time.Second), 900)
	if r.preferred != primary.info.ID {
		t.Fatal("spike carried over across healthy sample")
	}
	observe(now.Add(17*time.Second), 900)
	if r.preferred != reserve.info.ID {
		t.Fatal("sustained degradation did not promote usable reserve")
	}
}

func TestRelayLogicalGenerationRestartsDegradationWindow(t *testing.T) {
	r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "first", Mode: "auto", PoolSize: 1}, {ID: "second", Mode: "auto", PoolSize: 1, Priority: 1}})
	r.cfg.relay = true
	now := time.Now()
	primary := addPolicyPath(t, r, "first", 1, now)
	reserve := addPolicyPath(t, r, "second", 1, now)
	r.preferred = primary.info.ID
	b := r.backend.(*runtimeTransport)
	for second := 0; second <= 10; second++ {
		for id, s := range b.paths {
			s.DataProbes++
			if id == primary.info.ID {
				s.RTTMS = 900
			} else {
				s.RTTMS = 50
				if second == 7 {
					s.Generation++
				}
			}
			b.paths[id] = s
		}
		r.refresh(now.Add(time.Duration(second) * time.Second))
	}
	if r.preferred != primary.info.ID {
		t.Fatal("new reserve generation inherited old degradation evidence", reserve.info.ID)
	}
}

func TestRelayLogicalReservePrefersPermittedDifferentNetwork(t *testing.T) {
	r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "first", Mode: "auto", PoolSize: 1}, {ID: "second", Mode: "auto", PoolSize: 1, Priority: 1}})
	r.cfg.relay = true
	r.cfg.LogicalConnections = map[string]string{"first": "a", "second": "b"}
	now := time.Now()
	primary := addPolicyPath(t, r, "first", 1, now)
	r.preferred = primary.info.ID
	wifi := r.networks["wifi"]
	wifi.UnderlayEndpoints = map[string]string{"first": "127.0.0.1:443", "second": "127.0.0.2:443"}
	r.networks["wifi"] = wifi
	allowed := true
	r.networks["cell"] = runtimeNetwork{Network: "cell", Allowed: true, Available: true, Generation: 1, ReserveCheckAllowed: &allowed, UnderlayEndpoints: wifi.UnderlayEndpoints}
	r.candidates = append(r.candidates, &runtimeCandidate{profile: r.cfg.Model.Profiles[1], network: "cell"})
	got := r.selectDial(now)
	if got == nil || got.profile.ID != "second" || got.network != "cell" {
		t.Fatalf("reserve did not diversify permitted network: %+v", got)
	}
	allowed = false
	got = r.selectDial(now)
	if got == nil || got.network != "wifi" {
		t.Fatal("forbidden LTE reserve admitted", got)
	}
}

// A late cellular network must replace only the Wi-Fi spare, even when both
// logical connection slots were filled before Android supplied LTE endpoints.
func TestRelayLateCellRebalancesFullPool(t *testing.T) {
	for _, blocked := range []string{"", "permission", "economy", "unresolved", "backoff", "exclusive-primary", "exclusive-spare", "budget"} {
		t.Run(blocked, func(t *testing.T) {
			r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "first", Mode: "auto", PoolSize: 1}, {ID: "second", Mode: "auto", PoolSize: 1, Priority: 1}})
			r.cfg.relay = true
			now := time.Now()
			primary := addPolicyPath(t, r, "first", 1, now)
			spare := addPolicyPath(t, r, "second", 1, now)
			r.preferred = primary.info.ID
			yes := true
			r.networks["cell"] = runtimeNetwork{Network: "cell", Allowed: true, Available: true, Generation: 1, ReserveCheckAllowed: &yes, UnderlayEndpoints: map[string]string{"second": "127.0.0.2:443"}}
			candidate := &runtimeCandidate{profile: r.cfg.Model.Profiles[1], network: "cell"}
			r.candidates = append(r.candidates, candidate)
			switch blocked {
			case "permission":
				yes = false
			case "economy":
				r.cfg.Economy = true
			case "unresolved":
				n := r.networks["cell"]
				n.UnderlayEndpoints = nil
				r.networks["cell"] = n
			case "backoff":
				candidate.retry = now.Add(time.Minute)
			case "exclusive-primary":
				r.cfg.exclusivePeers = map[string][32]byte{"first": {}, "second": {}}
			case "exclusive-spare":
				r.cfg.exclusivePeers = map[string][32]byte{"second": {}}
			case "budget":
				r.budget, _ = NewTrafficBudget("exhausted", 1)
				r.budget.ledger.ObserveReceived(1, trafficbudget.User)
			}
			got := r.selectDial(now)
			if blocked != "" && blocked != "exclusive-spare" {
				if got != nil || len(r.paths) != 2 {
					t.Fatal("unusable LTE displaced healthy spare", got, len(r.paths))
				}
				return
			}
			if got != candidate {
				t.Fatalf("full Wi-Fi pool did not admit late LTE: %+v", got)
			}
			if r.paths[primary.info.ID] != primary || r.preferred != primary.info.ID || r.paths[spare.info.ID] != nil {
				t.Fatal("replacement disturbed primary or retained old spare")
			}
		})
	}
}

func TestRelayLateCellReserveRemainsWarm(t *testing.T) {
	var c exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &c)
	c.Economy = false
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("late-lte", 0)
	r.SetTrafficBudget(budget)
	raw, _ := json.Marshal(c)
	if err := r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
	r.mu.Lock()
	primary := r.preferred
	r.mu.Unlock()
	if err := r.UpdateNetwork(`{"network":"cell","available":true,"allowed":true,"reserve_check_allowed":true,"generation":1,"underlay_endpoints":{"relay2":"127.0.0.2:443","relay3":"127.0.0.3:443"}}`, nil); err != nil {
		t.Fatal(err)
	}
	awaitRuntime(t, func() bool {
		for _, p := range b.Stats().Paths {
			if p.Network == "cell" && p.Ready {
				return true
			}
		}
		return false
	})
	n, _ := b.counts()
	time.Sleep(1500 * time.Millisecond)
	after, _ := b.counts()
	r.mu.Lock()
	defer r.mu.Unlock()
	if after != n || len(r.paths) != 2 || r.preferred != primary || r.paths[primary] == nil {
		t.Fatal("warm LTE churned or primary changed", n, after, r.preferred)
	}
}
