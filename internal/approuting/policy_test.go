package approuting

import (
	"encoding/json"
	"strings"
	"testing"
)

var appA = strings.Repeat("a", 64)
var appB = strings.Repeat("b", 64)
var management = strings.Repeat("c", 64)

func TestSelectionAndFailureMatrix(t *testing.T) {
	for _, mode := range []Mode{OnlySelected, AllExceptSelected} {
		p, err := Compile(Policy{Version: 1, Mode: mode, Only: []string{appA}, Except: []string{appB}}, []string{management})
		if err != nil {
			t.Fatal(err)
		}
		for _, state := range []State{Stopped, Starting, Ready, Failed} {
			for _, proto := range []string{"tcp", "udp", "icmp"} {
				for _, v6 := range []bool{false, true} {
					want := Block
					if state == Stopped {
						want = Direct
					} else if state == Ready && !v6 && proto != "icmp" {
						want = Tunnel
					}
					if d := p.Decide(appA, proto, v6, state); d != want {
						t.Fatalf("%s %s %s %v: %s want %s", mode, state, proto, v6, d, want)
					}
					if p.Decide(appB, proto, v6, state) != Direct || p.Decide(management, proto, v6, state) != Direct {
						t.Fatal("unselected or protected captured")
					}
				}
			}
		}
		if p.Decide("", "tcp", false, Ready) != Block {
			t.Fatal("unknown attributed as direct")
		}
	}
}
func TestCompileRejectsInvalidAndOwnsLists(t *testing.T) {
	base := Policy{Version: 1, Mode: OnlySelected, Only: []string{appA}, Except: []string{appB}}
	for _, edit := range []func(*Policy){func(p *Policy) { p.Version = 2 }, func(p *Policy) { p.Mode = "all" }, func(p *Policy) { p.Only = []string{"game.exe"} }, func(p *Policy) { p.Only = []string{appA, appA} }, func(p *Policy) { p.Only = []string{management} }, func(p *Policy) { p.Except = []string{""} }} {
		p := base
		edit(&p)
		if _, e := Compile(p, []string{management}); e == nil {
			t.Fatal("invalid accepted", p)
		}
	}
	c, e := Compile(base, []string{management})
	if e != nil {
		t.Fatal(e)
	}
	base.Only[0] = appB
	if c.Decide(appA, "tcp", false, Ready) != Tunnel || c.Decide(appB, "tcp", false, Ready) != Direct {
		t.Fatal("caller mutated compiled policy")
	}
}
func TestPolicyParseStrictAndPreserveInactiveList(t *testing.T) {
	p := Policy{Version: 1, Mode: OnlySelected, Only: []string{appA}, Except: []string{appB}}
	raw, _ := json.Marshal(p)
	got, e := Parse(raw)
	if e != nil || got.Except[0] != appB {
		t.Fatal(got, e)
	}
	for _, raw := range [][]byte{[]byte(`{}`), []byte(`{"version":1,"mode":"only_selected","other":1}`), append(raw, []byte(` {}`)...), make([]byte, (1<<20)+1)} {
		if _, e := Parse(raw); e == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
}
func TestUninitializedPolicyBlocks(t *testing.T) {
	var c Compiled
	for _, state := range []State{Starting, Ready, Failed} {
		if c.Decide(appA, "tcp", false, state) != Block {
			t.Errorf("zero policy allowed %s", state)
		}
	}
	if c.Decide(appA, "tcp", false, Stopped) != Direct {
		t.Fatal("stop must release")
	}
}
func TestDuplicatePolicyFieldsRejected(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"mode":"only_selected","only":["` + appA + `"],"only":[]}`,
		`{"version":1,"mode":"only_selected","mode":"all_except_selected"}`,
		`{"version":1,"mode":"only_selected","ONLY":[]}`,
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("ambiguous policy accepted: %s", raw)
		}
	}
}
