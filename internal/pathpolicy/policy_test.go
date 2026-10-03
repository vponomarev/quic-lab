package pathpolicy

import (
	"quiclab/internal/vpnmodel"
	"testing"
	"time"
)

func fixture() *Policy {
	return New(vpnmodel.Config{Profiles: []vpnmodel.Profile{
		{ID: "fast", ExitID: "internet", Mode: "auto", Priority: 0},
		{ID: "backup", ExitID: "internet", Mode: "reserve", Priority: 1},
		{ID: "off", ExitID: "internet", Mode: "disabled"},
	}})
}
func find(ds []Decision, action, profile, network string) (Decision, bool) {
	for _, d := range ds {
		if d.Action == action && d.ProfileID == profile && (network == "" || d.Network == network) {
			return d, true
		}
	}
	return Decision{}, false
}
func fail(p *Policy, d Decision, at time.Time) {
	p.Observe(Observation{PathID: d.PathID, Generation: d.Generation, At: at, Failed: true})
}
func TestReserveAndDisabled(t *testing.T) {
	p := fixture()
	now := time.Unix(100, 0)
	p.SetNetwork("wifi", true, true)
	p.SetNetwork("cell", true, true)
	ds := p.Next(now)
	w, ok := find(ds, "dial", "fast", "wifi")
	if !ok {
		t.Fatal(ds)
	}
	for _, d := range ds {
		if d.ProfileID != "fast" {
			t.Fatal("reserve or disabled used early", ds)
		}
	}
	fail(p, w, now)
	ds = p.Next(now)
	c, ok := find(ds, "dial", "fast", "cell")
	if !ok {
		t.Fatal("auto LTE must precede reserve", ds)
	}
	if _, ok := find(ds, "dial", "backup", ""); ok {
		t.Fatal("reserve before all autos failed")
	}
	fail(p, c, now)
	ds = p.Next(now)
	if _, ok := find(ds, "dial", "backup", ""); !ok {
		t.Fatal("reserve missing", ds)
	}
	for _, d := range ds {
		if d.ProfileID == "off" {
			t.Fatal("disabled used")
		}
	}
}
func TestReturnHysteresis(t *testing.T) {
	p := fixture()
	now := time.Unix(100, 0)
	p.SetNetwork("wifi", true, true)
	d, _ := find(p.Next(now), "dial", "fast", "")
	fail(p, d, now)
	r, _ := find(p.Next(now), "dial", "backup", "")
	p.Observe(Observation{PathID: r.PathID, Generation: r.Generation, At: now, Connected: true, DataProbeOK: true})
	if _, ok := find(p.Next(now), "promote", "backup", ""); !ok {
		t.Fatal("no fallback promotion")
	}
	d, ok := find(p.Next(now.Add(2*time.Second)), "dial", "fast", "")
	if !ok {
		t.Fatal("no retry")
	}
	for _, sec := range []int{2, 5, 9} {
		p.Observe(Observation{PathID: d.PathID, Generation: d.Generation, At: now.Add(time.Duration(sec) * time.Second), Connected: true, DataProbeOK: true})
		if _, ok := find(p.Next(now.Add(time.Duration(sec)*time.Second)), "promote", "fast", ""); ok {
			t.Fatal("premature return")
		}
	}
	if _, ok := find(p.Next(now.Add(10*time.Second)), "promote", "fast", ""); !ok {
		t.Fatal("stable preferred path not restored")
	}
}
func TestReserveProbeOptIn(t *testing.T) {
	p := fixture()
	for i := range p.profiles {
		if p.profiles[i].ID == "backup" {
			p.profiles[i].CheckReserve = true
		}
	}
	now := time.Unix(100, 0)
	p.SetNetwork("wifi", true, true)
	ds := p.Next(now)
	if _, ok := find(ds, "probe", "backup", ""); !ok {
		t.Fatal("opt-in probe missing")
	}
	if _, ok := find(p.Next(now.Add(time.Second)), "probe", "backup", ""); ok {
		t.Fatal("probe too frequent")
	}
}
func TestStaleGenerationAndDialTimeout(t *testing.T) {
	p := fixture()
	now := time.Unix(100, 0)
	p.SetNetwork("wifi", true, true)
	d, _ := find(p.Next(now), "dial", "fast", "")
	ds := p.Next(now.Add(10 * time.Second))
	if _, ok := find(ds, "close", "fast", ""); !ok {
		t.Fatal("dial never expires")
	}
	p.Observe(Observation{PathID: d.PathID, Generation: d.Generation - 1, At: now.Add(11 * time.Second), Connected: true, DataProbeOK: true})
	if _, ok := find(p.Next(now.Add(11*time.Second)), "promote", "fast", ""); ok {
		t.Fatal("stale callback accepted")
	}
}

func TestCarouselRecommendation(t *testing.T) {
	p := fixture()
	now := time.Unix(100, 0)
	p.SetNetwork("wifi", true, true)
	p.jitter = func(d time.Duration) time.Duration { return d }
	var d Decision
	for cycle := 0; cycle < 3; cycle++ {
		at := now.Add(time.Duration(cycle*100) * time.Second)
		var ok bool
		d, ok = find(p.Next(at), "dial", "fast", "")
		if !ok {
			t.Fatal("no dial", cycle)
		}
		p.Observe(Observation{PathID: d.PathID, Generation: d.Generation, At: at, Connected: true, PendingBytes: 1050, AckedBytes: 100})
		if _, ok := find(p.Next(at.Add(time.Second)), "close", "fast", ""); !ok {
			t.Fatal("stall not closed")
		}
		d, ok = find(p.Next(at.Add(40*time.Second)), "dial", "fast", "")
		if !ok {
			t.Fatal("no replacement")
		}
		p.Observe(Observation{PathID: d.PathID, Generation: d.Generation, At: at.Add(41 * time.Second), Connected: true, AckedBytes: 1050})
		ds := p.Next(at.Add(41 * time.Second))
		_, recommended := find(ds, "recommend_carousel", "fast", "")
		if recommended != (cycle == 2) {
			t.Fatal("wrong recommendation", cycle, ds)
		}
		fail(p, d, at.Add(42*time.Second))
	}
}
func TestIndependentExitsAndNetworkRevocation(t *testing.T) {
	p := New(vpnmodel.Config{Profiles: []vpnmodel.Profile{{ID: "a", ExitID: "home", Mode: "auto"}, {ID: "b", ExitID: "internet", Mode: "auto"}}})
	now := time.Unix(100, 0)
	p.SetNetwork("wifi", true, true)
	ds := p.Next(now)
	for _, id := range []string{"a", "b"} {
		if _, ok := find(ds, "dial", id, ""); !ok {
			t.Fatal("exit starved", ds)
		}
	}
	p.SetNetwork("wifi", false, true)
	ds = p.Next(now.Add(time.Second))
	for _, id := range []string{"a", "b"} {
		if _, ok := find(ds, "close", id, ""); !ok {
			t.Fatal("disallowed network remains", ds)
		}
	}
}

func TestBackoffAndProgressReset(t *testing.T) {
	p := fixture()
	p.jitter = func(d time.Duration) time.Duration { return d }
	c := &candidate{profile: p.profiles[0], id: "manual", state: "ready", generation: 1}
	p.paths[c.id] = c
	now := time.Unix(100, 0)
	for i, seconds := range []int{1, 2, 4, 8, 16, 30, 30} {
		p.failed(c, now)
		if c.retry.Sub(now) != time.Duration(seconds)*time.Second {
			t.Fatal("backoff", i, c.retry.Sub(now))
		}
	}
	c.state = "ready"
	for sec := 0; sec <= 30; sec++ {
		p.Observe(Observation{PathID: c.id, Generation: 1, At: now.Add(time.Duration(sec) * time.Second), AckedBytes: uint64(sec + 1)})
	}
	if c.failures != 0 {
		t.Fatal("useful progress did not reset backoff")
	}
	c.failures = 5
	c.progressSince = time.Time{}
	p.Observe(Observation{PathID: c.id, Generation: 1, At: now.Add(31 * time.Second), AckedBytes: 32})
	for sec := 32; sec <= 70; sec++ {
		p.Observe(Observation{PathID: c.id, Generation: 1, At: now.Add(time.Duration(sec) * time.Second), AckedBytes: 32})
	}
	p.Observe(Observation{PathID: c.id, Generation: 1, At: now.Add(71 * time.Second), AckedBytes: 33})
	if c.failures != 5 {
		t.Fatal("idle time reset backoff")
	}
}

func TestIntermittentUsefulProgressResetsBackoff(t *testing.T) {
	p := fixture()
	c := &candidate{profile: p.profiles[0], id: "manual", state: "ready", generation: 1, failures: 6}
	p.paths[c.id] = c
	now := time.Unix(100, 0)
	for tick := 0; tick <= 310; tick++ {
		p.Observe(Observation{PathID: c.id, Generation: 1, At: now.Add(time.Duration(tick) * 100 * time.Millisecond), AckedBytes: uint64(tick/10 + 1)})
	}
	if c.failures != 0 {
		t.Fatal("healthy intermittent ACKs never reset policy backoff")
	}
}
