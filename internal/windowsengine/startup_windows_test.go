//go:build windows

package windowsengine

import (
	"context"
	"encoding/json"
	"quiclab/internal/vpnclient"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in, read-only measurement. Run with -run '^$' -bench
// '^BenchmarkDiscoverWindowsUplink$' -benchtime=3x in a WMI-capable user context.
func BenchmarkDiscoverWindowsUplink(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := DiscoverWindowsUplink(ctx, Settings{UplinkMode: "auto"})
		cancel()
		if err != nil {
			b.Fatal(err)
		}
	}
}

type resolvingAutoFake struct {
	autoFake
	settings Settings
	result   chan Settings
}

func (f *resolvingAutoFake) Start(ctx context.Context, c vpnclient.Config) error {
	s, err := (WindowsPlatform{}).Resolve(ctx, f.settings)
	if err == nil {
		f.result <- s
	}
	return err
}

func TestAutomaticStartDiscoversUplinkOnce(t *testing.T) {
	old := runUplinkCommand
	defer func() { runUplinkCommand = old }()
	var calls atomic.Int32
	runUplinkCommand = func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		return json.Marshal(Uplink{Index: 5, Address: "192.168.5.2", Gateway: "192.168.5.1", Local: []string{"192.168.5.0/24"}})
	}
	s := valid()
	s.RoutingMode = "all_ipv4"
	s.UplinkMode = "auto"
	resolved := make(chan Settings, 1)
	b := NewAutomatic(s, DiscoverWindowsUplink, func(s Settings) ManagedEngine { return &resolvingAutoFake{settings: s, result: resolved} }, func(context.Context) (<-chan struct{}, func(), error) { return nil, func() {}, nil }, time.Hour)
	if err := b.Start(context.Background(), b.Config()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(context.Background())
	select {
	case got := <-resolved:
		if got.InterfaceIndex != 5 || containsIPv4(got.Targets, "192.168.5.1") || !containsIPv4(got.Targets, "1.1.1.1") {
			t.Fatal("lost discovery route exclusions")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("start failed")
	}
	if calls.Load() != 1 {
		t.Fatalf("startup queried uplink %d times; want one snapshot", calls.Load())
	}
}

func TestUplinkDiscoveryUsesBoundedQueries(t *testing.T) {
	old := runUplinkCommand
	defer func() { runUplinkCommand = old }()
	// Execute the real discovery script against deterministic CIM substitutes.
	// Each family may be read once, independent of the number of adapters.
	prelude := `$ErrorActionPreference='Stop';$script:calls=@{};
 function Count($name){$script:calls[$name]++;if($script:calls[$name] -gt 1){throw "Repeated CIM query: $name"}}
 function Get-NetAdapter {param([switch]$IncludeHidden);Count 'adapter';@([pscustomobject]@{ifIndex=5;Status='Up';HardwareInterface=$true;InterfaceDescription='Ethernet'},[pscustomobject]@{ifIndex=7;Status='Up';HardwareInterface=$true;InterfaceDescription='Wi-Fi'})}
 function Get-NetIPInterface {param($InterfaceIndex,$AddressFamily);Count 'interface';@([pscustomobject]@{InterfaceIndex=5;InterfaceMetric=30},[pscustomobject]@{InterfaceIndex=7;InterfaceMetric=5})}
 function Get-NetIPAddress {param($InterfaceIndex,$AddressFamily,$ErrorAction);Count 'address';@([pscustomobject]@{InterfaceIndex=5;AddressState='Preferred';IPAddress='192.168.5.2'},[pscustomobject]@{InterfaceIndex=7;AddressState='Preferred';IPAddress='192.168.7.2'})}
 function Get-NetRoute {param($InterfaceIndex,$AddressFamily,$DestinationPrefix,$PolicyStore,$ErrorAction);Count 'route';@([pscustomobject]@{InterfaceIndex=5;DestinationPrefix='0.0.0.0/0';NextHop='192.168.5.1';RouteMetric=5},[pscustomobject]@{InterfaceIndex=7;DestinationPrefix='0.0.0.0/0';NextHop='192.168.7.1';RouteMetric=10},[pscustomobject]@{InterfaceIndex=5;DestinationPrefix='192.168.5.0/24';NextHop='0.0.0.0';RouteMetric=1},[pscustomobject]@{InterfaceIndex=7;DestinationPrefix='192.168.7.0/24';NextHop='0.0.0.0';RouteMetric=1})};`
	runUplinkCommand = func(ctx context.Context, script string) ([]byte, error) { return networkScript(ctx, prelude+script) }
	s := valid()
	s.UplinkMode = "auto"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u, err := DiscoverWindowsUplink(ctx, s)
	if err != nil {
		t.Fatal("discovery did not use bounded snapshots:", err)
	}
	if u.Index != 7 || u.Address != "192.168.7.2" || u.Gateway != "192.168.7.1" || len(u.Local) != 2 {
		t.Fatalf("incorrect snapshot selection: %+v", u)
	}
}
