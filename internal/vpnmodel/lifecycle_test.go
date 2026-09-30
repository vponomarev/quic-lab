package vpnmodel

import "testing"

func TestLifecycleRejectsOldGeneration(t *testing.T) {
	c, _ := Parse([]byte(validConfig))
	r := NewRuntime(c)
	home := r.Start("home")
	first := r.Start("net")
	if !r.Apply(Event{"home", home, "active"}) {
		t.Fatal("home did not activate")
	}
	second := r.Start("net")
	if second == first || r.Apply(Event{"net", first, "active"}) {
		t.Fatal("stale generation accepted")
	}
	if r.State("home") != "active" {
		t.Fatal("restart affected home")
	}
	if !r.Apply(Event{"net", second, "incompatible"}) || r.State("net") != "incompatible" {
		t.Fatal("incompatibility lost")
	}
	if r.Apply(Event{"net", second, "active"}) {
		t.Fatal("incompatible session revived")
	}
	r.Stop("home")
	if r.Apply(Event{"home", home, "active"}) || r.State("home") != "stopped" {
		t.Fatal("stopped session revived")
	}
	if r.Start("unknown") != 0 || r.Apply(Event{"unknown", 0, "active"}) {
		t.Fatal("unknown exit accepted")
	}
}
func TestLifecycleRecoveryAndInvalidEvents(t *testing.T) {
	c, _ := Parse([]byte(validConfig))
	r := NewRuntime(c)
	g := r.Start("net")
	for _, kind := range []string{"active", "recovering", "blocked", "active"} {
		if !r.Apply(Event{"net", g, kind}) || string(r.State("net")) != kind {
			t.Fatal(kind)
		}
	}
	if r.Apply(Event{"net", g, "garbage"}) || r.State("net") != "active" {
		t.Fatal("invalid event changed state")
	}
}
