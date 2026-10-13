package windowsengine

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func valid() Settings {
	return Settings{Transport: "quic", Endpoint: "192.0.2.1:443", Hostname: "test", Certificate: "cert", Key: "key", InterfaceIndex: 5, Targets: []string{"198.51.100.1/32"}}
}
func TestRejectUnsafeScope(t *testing.T) {
	for _, target := range []string{"0.0.0.0/0", "::/0", "192.0.2.1/32", "127.0.0.1/32", "224.0.0.1/32", "198.51.100.0/24"} {
		s := valid()
		s.Targets = []string{target}
		if s.Validate() == nil {
			t.Fatalf("accepted unsafe scope %s", target)
		}
	}
	s := valid()
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
}

type fakePlatform struct {
	calls *[]string
	fail  bool
}

func (f fakePlatform) Open(context.Context, Settings) (Link, error) {
	*f.calls = append(*f.calls, "open")
	return &fakeLink{f.calls, f.fail}, nil
}

type fakeLink struct {
	calls *[]string
	fail  bool
}

func (f *fakeLink) Configure(context.Context, Settings) error {
	*f.calls = append(*f.calls, "routes")
	if f.fail {
		return errors.New("partial failure")
	}
	return nil
}
func (f *fakeLink) Close() error { *f.calls = append(*f.calls, "close"); return nil }

type fakeGateway struct {
	calls *[]string
	fail  bool
}

func (f *fakeGateway) Start(context.Context, Settings) error {
	*f.calls = append(*f.calls, "connect")
	if f.fail {
		return errors.New("connect failed")
	}
	return nil
}
func (f *fakeGateway) Attach(Link) error { *f.calls = append(*f.calls, "attach"); return nil }
func (f *fakeGateway) Stop()             { *f.calls = append(*f.calls, "disconnect") }
func TestPartialStartRestoresOwnedResources(t *testing.T) {
	for _, failConnect := range []bool{false, true} {
		var calls []string
		e := New(valid(), fakePlatform{&calls, !failConnect}, &fakeGateway{&calls, failConnect})
		if e.Start(context.Background(), e.Config()) == nil {
			t.Fatal("failure accepted")
		}
		if err := e.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := []string{"open", "connect", "disconnect", "close"}
		if !failConnect {
			want = []string{"open", "connect", "attach", "routes", "disconnect", "close"}
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatal(calls, want)
		}
		e.Stop(context.Background())
		if !reflect.DeepEqual(calls, want) {
			t.Fatal("not idempotent")
		}
	}
}
func TestCancellationBeforeMutation(t *testing.T) {
	var calls []string
	e := New(valid(), fakePlatform{&calls, false}, &fakeGateway{&calls, false})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e.Start(ctx, e.Config()) == nil || len(calls) != 0 {
		t.Fatal("mutated after cancellation", calls)
	}
}
func TestNonDefaultEndpointPort(t *testing.T) {
	s := valid()
	s.Endpoint = "192.0.2.1:8443"
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestGatewayFailureNeverConfiguresRoutes(t *testing.T) {
	var calls []string
	e := New(valid(), fakePlatform{&calls, false}, &fakeGateway{&calls, true})
	if e.Start(context.Background(), e.Config()) == nil {
		t.Fatal("expected failure")
	}
	e.Stop(context.Background())
	for _, c := range calls {
		if c == "routes" {
			t.Fatal("routes installed before connection")
		}
	}
}
func TestSelectedRoutesMustUseOwnedAdapter(t *testing.T) {
	for _, routes := range [][]RouteSelection{nil, {{Target: "1.1.1.1/32", InterfaceIndex: 25}}, {{Target: "8.8.8.8/32", InterfaceIndex: 51}}} {
		if VerifyRoutes(51, []string{"1.1.1.1/32"}, routes) == nil {
			t.Fatal("accepted bypass", routes)
		}
	}
	if e := VerifyRoutes(51, []string{"1.1.1.1/32"}, []RouteSelection{{Target: "1.1.1.1/32", InterfaceIndex: 51}}); e != nil {
		t.Fatal(e)
	}
}
func TestProbeRequiresRoutedDestinationAndExpectedExit(t *testing.T) {
	s := valid()
	if s.ValidateProbe("168.119.242.240") == nil {
		t.Fatal("probe allowed outside routes")
	}
	s.Targets = []string{"1.1.1.1/32"}
	if s.ValidateProbe("") == nil {
		t.Fatal("missing expected exit")
	}
	if e := s.ValidateProbe("168.119.242.240"); e != nil {
		t.Fatal(e)
	}
}
func TestRejectAdapterOwnAddress(t *testing.T) {
	s := valid()
	s.Targets = []string{"10.254.253.1/32"}
	if s.Validate() == nil {
		t.Fatal("accepted local adapter address")
	}
}
func TestProbeExitMustBePresentAndMatch(t *testing.T) {
	for _, body := range []string{"colo=TEST\n", "ip=192.0.2.2\n"} {
		if VerifyProbeExit(body, "168.119.242.240") == nil {
			t.Fatal("accepted unproven exit")
		}
	}
	if e := VerifyProbeExit("ip=168.119.242.240\n", "168.119.242.240"); e != nil {
		t.Fatal(e)
	}
}
