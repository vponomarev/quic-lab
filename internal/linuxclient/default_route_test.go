package linuxclient

import (
	"reflect"
	"slices"
	"testing"
)

func TestDefaultRouteOnlinkGateway(t *testing.T) {
	p, err := planDefaultRoute("198.18.0.2", []byte(`[{"dst":"default","gateway":"192.0.2.254","dev":"eth0","flags":["onlink"]}]`), []byte(`[{"dev":"eth0","gateway":"192.0.2.254","flags":[]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.pin, "onlink") {
		t.Fatalf("gateway reachability lost: %v", p.pin)
	}
}

func TestDefaultRoutePlan(t *testing.T) {
	lookup := []byte(`[{"dev":"eth0","gateway":"192.168.5.1"}]`)
	for _, defaults := range []string{`{"dst":"default","metric":600}`, `{"dst":"default"}`, `{"dst":"default","metric":1}`} {
		p, err := planDefaultRoute("198.18.0.2", []byte(`[`+defaults+`,{"dst":"192.168.5.0/24"}]`), lookup)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"198.18.0.2/32", "via", "192.168.5.1", "dev", "eth0", "proto", "242", "metric", "42760", "onlink"}
		if !reflect.DeepEqual(p.pin, want) {
			t.Fatalf("pin: %v", p.pin)
		}
		args := p.captureArgs("qltest")
		if len(args) != 2 || args[0][3] != "0.0.0.0/1" || args[1][3] != "128.0.0.0/1" {
			t.Fatalf("capture: %v", args)
		}
	}
}

func TestDefaultRoutePreservesEndpointExceptions(t *testing.T) {
	for _, dst := range []string{"198.18.0.0/24", "198.18.0.2/32", "198.18.0.2"} {
		p, err := planDefaultRoute("198.18.0.2", []byte(`[{"dst":"default"},{"dst":"`+dst+`"}]`), []byte(`[{"dev":"eth0"}]`))
		if err != nil || len(p.pin) != 0 {
			t.Fatalf("%s: %v %v", dst, p.pin, err)
		}
	}
	// Point-to-point physical defaults do not have a gateway address.
	p, err := planDefaultRoute("198.18.0.2", []byte(`[{"dst":"default"}]`), []byte(`[{"dev":"ppp0"}]`))
	if err != nil || !reflect.DeepEqual(p.pin, []string{"198.18.0.2/32", "dev", "ppp0", "proto", "242", "metric", "42760"}) {
		t.Fatal(p, err)
	}
}

func TestDefaultRouteRejectsAmbiguousCapture(t *testing.T) {
	for _, dst := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		raw := []byte(`[{"dst":"default"},{"dst":"` + dst + `"}]`)
		if CheckRoutes([]string{"0.0.0.0/0"}, raw) == nil {
			t.Fatal("existing /1 accepted", dst)
		}
		if _, err := planDefaultRoute("198.18.0.2", raw, []byte(`[{"dev":"eth0"}]`)); err == nil {
			t.Fatal("ambiguous plan", dst)
		}
	}
	for _, lookup := range []string{`[]`, `[{}]`, `[{"dev":"lo"}]`, `[{"dev":"eth0","gateway":"invalid"}]`, `[{"dev":"eth0","type":"blackhole"}]`} {
		if _, err := planDefaultRoute("198.18.0.2", []byte(`[{"dst":"default"}]`), []byte(lookup)); err == nil {
			t.Fatal("invalid endpoint path", lookup)
		}
	}
}
