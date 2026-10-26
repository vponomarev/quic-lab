package mobile

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/bond"
	"quiclab/internal/protocol"
	"quiclab/internal/vpnmodel"
	"sync/atomic"
	"testing"
	"time"
)

func bondAWGProfile(seed byte) string {
	private := make([]byte, 32)
	public := make([]byte, 32)
	for i := range private {
		private[i] = seed
		public[i] = seed + 1
	}
	return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 10.55.0.2/32\nDNS = 10.55.0.1\n[Peer]\nPublicKey = %s\nEndpoint = peer.invalid:51820\nAllowedIPs = 0.0.0.0/0\n", base64.StdEncoding.EncodeToString(private), base64.StdEncoding.EncodeToString(public))
}
func withAWGUnderlays(t *testing.T, config string, peers map[string]string) string {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(config), &raw); err != nil {
		t.Fatal(err)
	}
	gateways := raw["gateways"].(map[string]any)
	for id, profile := range peers {
		gateway := gateways[id].(map[string]any)
		gateway["underlay_vless_config"] = ""
		gateway["underlay_awg_config"] = profile
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// Ignoring the AWG envelope used to reject an otherwise usable shared-exit path.
func TestAWGRelayRuntimeAcceptsIsolatedUnderlay(t *testing.T) {
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("awg-relay", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(withAWGUnderlays(t, relayRuntimeConfig(t), map[string]string{"relay1": bondAWGProfile(3)}), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
}

// Two imported profiles with the same key pair must never compete through roaming.
func TestAWGRelayDuplicatePeersShareOnePhysicalSlot(t *testing.T) {
	var cfg exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &cfg)
	cfg.Model.Profiles = cfg.Model.Profiles[:2]
	delete(cfg.Gateways, "relay3")
	raw, _ := json.Marshal(cfg)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("awg-duplicate", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(withAWGUnderlays(t, string(raw), map[string]string{"relay1": bondAWGProfile(3), "relay2": bondAWGProfile(3)}), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.UpdateNetwork(`{"network":"cell","generation":1,"available":true,"allowed":true,"underlay_endpoints":{"relay1":"127.0.0.1:51820","relay2":"127.0.0.1:51820"}}`, nil); err != nil {
		t.Fatal(err)
	}
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) > 0 })
	time.Sleep(250 * time.Millisecond)
	if paths := b.Stats().Paths; len(paths) != 1 {
		t.Fatalf("same peer opened competing paths: %+v", paths)
	}
	b.mu.Lock()
	b.fail = map[string]bool{"relay1": true}
	for id, p := range b.paths {
		if p.ProfileID == "relay1" {
			delete(b.paths, id)
		}
	}
	b.mu.Unlock()
	awaitRuntime(t, func() bool { p := b.Stats().Paths; return len(p) == 1 && p[0].ProfileID == "relay2" })
}

func TestMixedRelayAcceptsOwnDirectCarrier(t *testing.T) {
	var cfg exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &cfg)
	for id, g := range cfg.Gateways {
		g.Endpoint = "127.0.0.1:443"
		cfg.Gateways[id] = g
	}
	for i := range cfg.Model.Profiles {
		cfg.Model.Profiles[i].Endpoint = "127.0.0.1:443"
	}
	direct := cfg.Gateways["relay2"]
	direct.UnderlayVLESSConfig = ""
	direct.UnderlayEndpoint = ""
	direct.Transport = "quic"
	cfg.Gateways["relay2"] = direct
	cfg.Model.Profiles[1].Transport = "quic"
	raw, _ := json.Marshal(cfg)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("mixed-direct", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
	seen := false
	for _, p := range b.Stats().Paths {
		seen = seen || p.ProfileID == "relay2"
	}
	if !seen {
		t.Fatal("compatible direct path was not admitted")
	}
}

func TestAWGRelayRequiresOneSlot(t *testing.T) {
	var cfg exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &cfg)
	cfg.Model.Profiles = []vpnmodel.Profile{cfg.Model.Profiles[0]}
	cfg.Model.Profiles[0].PoolSize = 2
	raw, _ := json.Marshal(cfg)
	r := NewExitRuntime(nil)
	r.backend = &runtimeTransport{}
	budget, _ := NewTrafficBudget("awg-slots", 0)
	r.SetTrafficBudget(budget)
	defer r.Stop()
	if err := r.Start(withAWGUnderlays(t, string(raw), map[string]string{"relay1": bondAWGProfile(3)}), nil); err == nil {
		t.Fatal("same AWG peer accepted multiple pool slots")
	}
}

// A target-protocol allowlist admits the chosen server's QUIC port while rejecting
// a candidate that silently changes the shared exit.
func TestMixedRelayTargetEndpointsAndNetworkResolution(t *testing.T) {
	for _, withUnderlay := range []bool{false, true} {
		t.Run(fmt.Sprintf("underlay=%t", withUnderlay), func(t *testing.T) {
			var cfg exitRuntimeConfig
			json.Unmarshal([]byte(relayRuntimeConfig(t)), &cfg)
			cfg.Model.Profiles = cfg.Model.Profiles[:2]
			delete(cfg.Gateways, "relay3")
			cfg.TargetEndpoints = map[string]string{"https": "gateway.invalid:443", "quic": "gateway.invalid:4443"}
			cfg.InitialNetwork.UnderlayEndpoints = map[string]string{"relay1": "127.0.0.1:443", "relay2": "127.0.0.1:4443"}
			first := cfg.Gateways["relay1"]
			if !withUnderlay {
				first.UnderlayVLESSConfig = ""
				first.UnderlayEndpoint = ""
			}
			cfg.Gateways["relay1"] = first
			second := cfg.Gateways["relay2"]
			second.UnderlayVLESSConfig = ""
			second.UnderlayEndpoint = ""
			second.Transport = "quic"
			second.Endpoint = "gateway.invalid:4443"
			cfg.Gateways["relay2"] = second
			cfg.Model.Profiles[1].Transport = "quic"
			cfg.Model.Profiles[1].Endpoint = second.Endpoint
			raw, _ := json.Marshal(cfg)
			b := &runtimeTransport{}
			r := NewExitRuntime(nil)
			r.backend = b
			budget, _ := NewTrafficBudget("mixed-target", 0)
			r.SetTrafficBudget(budget)
			if err := r.Start(string(raw), nil); err != nil {
				t.Fatal(err)
			}
			defer r.Stop()
			awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 2 })
			r.mu.Lock()
			resolved := r.gatewayForNetwork("relay2", "wifi")
			original := r.cfg.Gateways["relay2"]
			shared := r.cfg.relay
			r.mu.Unlock()
			if !shared || resolved.Endpoint != "127.0.0.1:4443" || original.Endpoint != "gateway.invalid:4443" || resolved.UnderlayEndpoint != "" {
				t.Fatalf("incorrect target/resolution lifecycle: %+v", resolved)
			}
		})
	}
}

func TestAWGRelayInvalidEnvelopeNeverFallsBack(t *testing.T) {
	var sample gatewayConfig
	json.Unmarshal([]byte(vlessGatewayJSON(t)), &sample)
	base := gatewayConfig{Transport: "https", Endpoint: "127.0.0.1:1", UnderlayAWGConfig: bondAWGProfile(7), UnderlayEndpoint: "127.0.0.1:51820"}
	for _, change := range []struct {
		name  string
		alter func(*gatewayConfig)
	}{
		{"conflicting profiles", func(c *gatewayConfig) { c.UnderlayVLESSConfig = sample.VLESSConfig }},
		{"malformed profile", func(c *gatewayConfig) { c.UnderlayAWGConfig = "[Interface]\nPrivateKey = synthetic-invalid-secret" }},
		{"unresolved outer", func(c *gatewayConfig) { c.UnderlayEndpoint = "peer.invalid:51820" }},
		{"unspecified outer", func(c *gatewayConfig) { c.UnderlayEndpoint = "0.0.0.0:51820" }},
		{"IPv6 outer", func(c *gatewayConfig) { c.UnderlayEndpoint = "[::1]:51820" }},
		{"QUIC carrier", func(c *gatewayConfig) { c.Transport = "quic" }},
		{"orphan endpoint", func(c *gatewayConfig) { c.UnderlayAWGConfig = "" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			cfg := base
			change.alter(&cfg)
			g := NewGateway(nil)
			g.cfg = cfg
			if err := g.addBondNamedPath(context.Background(), bond.PathInfo{ID: "invalid", Network: "wifi"}, gatewayVLESSBinder{}, true); err == nil {
				t.Fatal("invalid envelope accepted")
			}
		})
	}
	if _, _, err := newBondAWGDialer(base.UnderlayAWGConfig, base.UnderlayEndpoint, nil); err == nil {
		t.Fatal("unprotected AWG accepted")
	}
}

func TestAWGUnderlayStartupNeverUsesDirectControl(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	var calls atomic.Int32
	h := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 1}).ServeHTTP(w, r)
	}))
	h.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	h.StartTLS()
	defer h.Close()
	for _, ha := range []bool{false, true} {
		calls.Store(0)
		cfg := gatewayConfig{Transport: "https", Endpoint: "127.0.0.1:1", Hostname: "localhost", CA: cp, Certificate: cp, Key: kp, ControlURL: h.URL, UnderlayAWGConfig: "invalid synthetic config", UnderlayEndpoint: "127.0.0.1:51820", MaxAvailability: ha}
		raw, _ := json.Marshal(cfg)
		g := NewGateway(nil)
		err := g.Start(string(raw), nil)
		g.Stop()
		if err == nil || calls.Load() != 0 {
			t.Fatalf("HA=%t: underlay made direct control request: calls=%d err=%v", ha, calls.Load(), err)
		}
	}
}

func TestAWGRelayUnresolvedProfilesUseOnlyNetworkResolutions(t *testing.T) {
	var cfg exitRuntimeConfig
	json.Unmarshal([]byte(relayRuntimeConfig(t)), &cfg)
	cfg.Model.Profiles = cfg.Model.Profiles[:2]
	delete(cfg.Gateways, "relay3")
	for id, g := range cfg.Gateways {
		g.UnderlayEndpoint = "peer.invalid:51820"
		cfg.Gateways[id] = g
	}
	cfg.InitialNetwork.UnderlayEndpoints = map[string]string{"relay2": "127.0.0.1:51820"}
	raw, _ := json.Marshal(cfg)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("awg-resolutions", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(withAWGUnderlays(t, string(raw), map[string]string{"relay1": bondAWGProfile(9), "relay2": bondAWGProfile(11)}), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 1 })
	if p := b.Stats().Paths[0]; p.ProfileID != "relay2" {
		t.Fatal("unresolved candidate bypassed network map", p)
	}
	r.mu.Lock()
	resolved := r.gatewayForNetwork("relay2", "wifi")
	original := r.cfg.Gateways["relay2"]
	r.mu.Unlock()
	if resolved.UnderlayEndpoint != "127.0.0.1:51820" || original.UnderlayEndpoint != "peer.invalid:51820" {
		t.Fatal("raw endpoint overwritten or not resolved")
	}
}
