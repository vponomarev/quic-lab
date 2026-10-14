package approuting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeExecutableIdentity(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	a, e := ResolveExecutable(exe)
	if e != nil {
		t.Fatal(e)
	}
	b, e := ResolveExecutable(strings.ToUpper(exe))
	if e != nil {
		t.Fatal(e)
	}
	if a.ID != b.ID || !IsCanonicalID(a.ID) || len(a.AppID) == 0 || a.Path == "" {
		t.Fatal("noncanonical executable identity")
	}
	for _, bad := range []string{"relative.exe", exe + ":stream", filepath.Dir(exe), filepath.Join(t.TempDir(), "missing.exe")} {
		if _, e := ResolveExecutable(bad); e == nil {
			t.Fatal("invalid executable accepted")
		}
	}
}

func TestRunningApplicationsIncludesCurrentExecutable(t *testing.T) {
	exe, _ := os.Executable()
	id, e := ResolveExecutable(exe)
	if e != nil {
		t.Fatal(e)
	}
	apps, _, e := RunningApplications()
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, app := range apps {
		if app.ID == id.ID {
			for _, pid := range app.PIDs {
				if pid == uint32(os.Getpid()) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("current executable missing from read-only inventory")
	}
}
