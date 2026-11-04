package approuting

import (
	"context"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestTUNPolicyModesAndUnknown(t *testing.T) {
	p := TUNPolicy{Version: 2, Enabled: true, Mode: OnlySelected, Only: []string{`C:\Apps\game.exe`}, Except: []string{`C:\Apps\other.exe`}}
	selected := Attribution{Quality: Resolved, Owners: []ProcessIdentity{{Path: `c:\apps\GAME.exe`}}}
	other := Attribution{Quality: Resolved, Owners: []ProcessIdentity{{Path: `C:\Apps\other.exe`}}}
	for _, mode := range []Mode{OnlySelected, AllExceptSelected} {
		p.Mode = mode
		c, e := CompileTUN(p)
		if e != nil {
			t.Fatal(e)
		}
		if c.Decide(selected) != Tunnel || c.Decide(other) != Direct {
			t.Fatal("wrong exit", mode)
		}
		if c.Decide(Attribution{Quality: Unknown}) != Tunnel {
			t.Fatal("default unknown must use VPN")
		}
		mixed := Attribution{Quality: Shared, Owners: append(selected.Owners, other.Owners...)}
		if c.Decide(mixed) != Tunnel {
			t.Fatal("mixed policy")
		}
		p.Unknown = Block
		c, _ = CompileTUN(p)
		if c.Decide(mixed) != Block {
			t.Fatal("mixed must follow explicit unknown")
		}
		same := Attribution{Quality: Shared, Owners: append(other.Owners, other.Owners...)}
		if c.Decide(same) != Direct {
			t.Fatal("shared agreeing owners")
		}
		p.Unknown = ""
	}
	c, _ := CompileTUN(p)
	p.Except[0] = `C:\Apps\game.exe`
	if c.Decide(selected) != Tunnel {
		t.Fatal("mutable compiled policy")
	}
	p.Enabled = false
	c, _ = CompileTUN(p)
	if c.Decide(Attribution{Quality: Unknown}) != Tunnel {
		t.Fatal("disabled must preserve tunnel")
	}
}

func TestTUNPolicyRejectsAmbiguousPaths(t *testing.T) {
	for _, path := range []string{`game.exe`, `\\server\game.exe`, `C:\a\..\game.exe`, `C:\a\game.exe:stream`, `C:\a\bad?.exe`, `C:\a \game.exe`, `C:/a/game.exe`} {
		_, e := CompileTUN(TUNPolicy{Version: 2, Mode: OnlySelected, Only: []string{path}})
		if e == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	for _, p := range []TUNPolicy{{Version: 1, Mode: OnlySelected}, {Version: 2, Mode: "oops"}, {Version: 2, Mode: OnlySelected, Unknown: "guess"}, {Version: 2, Mode: OnlySelected, Only: []string{`C:\a.exe`, `c:\A.exe`}}} {
		if _, e := CompileTUN(p); e == nil {
			t.Fatal("accepted invalid policy")
		}
	}
	c, _ := CompileTUN(TUNPolicy{Version: 2, Enabled: true, Mode: AllExceptSelected, Unknown: Block})
	if c.Decide(Attribution{Quality: Resolved}) != Block || c.Decide(Attribution{Quality: Resolved, Owners: []ProcessIdentity{{Path: ""}}}) != Block {
		t.Fatal("invalid attribution guessed")
	}
}

func TestOwnerCandidatesTCPAndUDP(t *testing.T) {
	src := netip.MustParseAddrPort("10.0.0.2:1234")
	dst := netip.MustParseAddrPort("1.1.1.1:443")
	f := FlowKey{Protocol: 6, Source: src, Destination: dst}
	rows := []OwnerRow{{Local: src, Remote: dst, PID: 10}, {Local: src, Remote: netip.MustParseAddrPort("2.2.2.2:443"), PID: 20}, {Local: netip.MustParseAddrPort("0.0.0.0:1234"), PID: 30}}
	ids, reason := OwnerCandidates(f, rows)
	if len(ids) != 1 || ids[0] != 10 || reason != "" {
		t.Fatal(ids, reason)
	}
	f.Protocol = 17
	ids, reason = OwnerCandidates(f, rows)
	if len(ids) != 3 || reason != "" {
		t.Fatal("must retain exact and wildcard candidates", ids, reason)
	}
	rows = append(rows, OwnerRow{Local: src, PID: 0})
	if _, reason = OwnerCandidates(f, rows); reason != "owner_unavailable" {
		t.Fatal(reason)
	}
	f.Protocol = 1
	if _, reason = OwnerCandidates(f, rows); reason != "unsupported_protocol" {
		t.Fatal(reason)
	}
}

func TestBoundedOwnerResolverTimeoutAndSaturation(t *testing.T) {
	gate := make(chan struct{})
	var calls atomic.Int32
	r := NewBoundedResolver(func(context.Context, FlowKey) Attribution {
		calls.Add(1)
		<-gate
		return Attribution{Quality: Resolved}
	}, 1, 20*time.Millisecond)
	a := r.Resolve(context.Background(), FlowKey{})
	if a.Quality != Unknown || a.Reason != "timeout" {
		t.Fatal(a)
	}
	a = r.Resolve(context.Background(), FlowKey{})
	if a.Reason != "overloaded" || calls.Load() != 1 {
		t.Fatal(a, calls.Load())
	}
	close(gate)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a = r.Resolve(ctx, FlowKey{}); a.Reason != "canceled" {
		t.Fatal(a)
	}
}
