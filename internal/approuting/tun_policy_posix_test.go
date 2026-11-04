package approuting

import "testing"

func TestLinuxTUNPolicyPathsAndModes(t *testing.T) {
	p := TUNPolicy{Version: 2, Enabled: true, Mode: OnlySelected, Only: []string{"/opt/Game/client"}, Except: []string{"/usr/bin/browser"}, Unknown: Block}
	owner := func(path string) Attribution {
		return Attribution{Quality: Resolved, Owners: []ProcessIdentity{{Path: path}}}
	}
	for _, mode := range []Mode{OnlySelected, AllExceptSelected} {
		p.Mode = mode
		c, e := CompileLinuxTUN(p)
		if e != nil {
			t.Fatal(e)
		}
		if c.Decide(owner("/opt/Game/client")) != Tunnel || c.Decide(owner("/usr/bin/browser")) != Direct {
			t.Fatal("mode", mode)
		}
		if c.Decide(Attribution{Quality: Unknown}) != Block {
			t.Fatal("unknown")
		}
		if c.Decide(Attribution{Quality: Shared, Owners: []ProcessIdentity{{Path: "/opt/Game/client"}, {Path: "/usr/bin/browser"}}}) != Block {
			t.Fatal("mixed shared")
		}
	}
	p.Mode = OnlySelected
	c, _ := CompileLinuxTUN(p)
	if c.Decide(owner("/opt/game/client")) != Direct {
		t.Fatal("Linux case folded")
	}
	p.Only[0] = "/changed"
	if c.Decide(owner("/opt/Game/client")) != Tunnel {
		t.Fatal("policy alias")
	}
	for _, path := range []string{"relative", "/opt/../bin/app", "//bin/app", "/bin/./app", "/bin/app\n", "/bin/app (deleted)", "/"} {
		p.Only = []string{path}
		if _, e := CompileLinuxTUN(p); e == nil {
			t.Fatalf("bad path %q", path)
		}
	}
}
