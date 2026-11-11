//go:build linux && !android

package desktopinstall

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/legacy-network.py
var legacyNetwork []byte

func TestKnownNetworkHelperMigratesAndRollsBack(t *testing.T) {
	for _, failStart := range []bool{false, true} {
		root, source := fixture(t)
		fail := failStart
		var calls []string
		helper := filepath.Join(root, "usr/local/libexec/quic-lab-network")
		if err := os.MkdirAll(filepath.Dir(helper), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(helper, legacyNetwork, 0755); err != nil {
			t.Fatal(err)
		}
		_, err := install(context.Background(), root, source, 1000, fakeRunner(true, &fail, &calls))
		if failStart {
			if err == nil {
				t.Fatal("expected failure")
			}
			b, e := os.ReadFile(helper)
			if e != nil || string(b) != string(legacyNetwork) {
				t.Fatal("legacy helper not restored", e)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			link, e := os.Readlink(helper)
			if e != nil || link != "../bin/quic-lab" {
				t.Fatal("helper not single binary", link, e)
			}
			// Installing again accepts our managed symlink.
			if _, err = install(context.Background(), root, source, 1000, fakeRunner(true, &fail, &calls)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestUnknownNetworkHelperIsNotOverwritten(t *testing.T) {
	root, source := fixture(t)
	fail := false
	var calls []string
	path := filepath.Join(root, "usr/local/libexec/quic-lab-network")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("custom helper"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := install(context.Background(), root, source, 1000, fakeRunner(true, &fail, &calls)); err == nil {
		t.Fatal("custom helper accepted")
	}
	if len(calls) != 0 {
		t.Fatal("commands ran before helper validation")
	}
}

func fixture(t *testing.T) (string, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned migration fixture")
	}
	root := t.TempDir()
	for _, p := range []string{"usr/local/bin", "usr/local/share/applications", "usr/local/share/quic-lab", "etc/quic-lab/profiles", "etc/systemd/system", "var/backups"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"usr/local/bin/quic-lab-client": "OLD_ENGINE", "usr/local/bin/quic-lab-gui": "OLD_GUI", "etc/systemd/system/quic-lab-control.service": "OLD_CONTROL", "etc/quic-lab/profiles/quic.json": `{"transport":"quic"}`, "etc/quic-lab/gui-control.json": `{"allowed_uid":1000}`}
	for p, b := range files {
		if err := os.WriteFile(filepath.Join(root, p), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("profiles/quic.json", filepath.Join(root, "etc/quic-lab/client.json")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "quic-lab")
	if err := os.WriteFile(source, []byte("NEW_ENGINE_AND_GUI"), 0755); err != nil {
		t.Fatal(err)
	}
	return root, source
}

func fakeRunner(active bool, fail *bool, calls *[]string) Runner {
	return func(ctx context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		*calls = append(*calls, call)
		if len(args) == 1 && args[0] == "status" {
			if active {
				return `{"selected":"quic","active":"active","started":true}`, nil
			}
			return `{"selected":"quic","active":"inactive","started":false}`, nil
		}
		if strings.Contains(call, " show quic-lab-client.service") {
			state := "inactive"
			if active {
				state = "active"
			}
			return "ActiveState=" + state + "\nUnitFileState=enabled\nExecStart={ path=/usr/local/bin/quic-lab-client ; argv[]=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json ; ignore_errors=no ; start_time=n/a ; }\n", nil
		}
		if strings.Contains(call, " show quic-lab-control.service") {
			return "ActiveState=active\nUnitFileState=enabled\nLoadState=loaded\n", nil
		}
		if strings.Contains(call, "restart quic-lab-control.service") && *fail {
			*fail = false
			return "", errors.New("fixture failure")
		}
		return "", nil
	}
}

func TestNoControllerReadinessRollsBack(t *testing.T) {
	root, source := fixture(t)
	fail := false
	var calls []string
	base := fakeRunner(true, &fail, &calls)
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		if len(args) == 1 && args[0] == "status" {
			return "", errors.New("socket unavailable")
		}
		return base(ctx, name, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := install(ctx, root, source, 1000, runner); err == nil {
		t.Fatal("accepted unavailable controller")
	}
	b, err := os.ReadFile(filepath.Join(root, "usr/local/bin/quic-lab-client"))
	if err != nil || string(b) != "OLD_ENGINE" {
		t.Fatal("rollback failed", err)
	}
}

func TestVPNWithoutStartupConfirmationRollsBack(t *testing.T) {
	root, source := fixture(t)
	fail := false
	var calls []string
	base := fakeRunner(true, &fail, &calls)
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		if len(args) == 1 && args[0] == "status" {
			return `{"selected":"quic","active":"active","started":false}`, nil
		}
		return base(ctx, name, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := install(ctx, root, source, 1000, runner); err == nil {
		t.Fatal("accepted VPN without startup")
	}
}

func TestMigrationIsOneExecutableAndPreservesProfiles(t *testing.T) {
	root, source := fixture(t)
	fail := false
	var calls []string
	backup, err := install(context.Background(), root, source, 1000, fakeRunner(true, &fail, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backup, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"quic-lab-client", "quic-lab-gui"} {
		target, err := os.Readlink(filepath.Join(root, "usr/local/bin", name))
		if err != nil || target != "quic-lab" {
			t.Fatal(name, target, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, "usr/local/bin/quic-lab"))
	if string(b) != "NEW_ENGINE_AND_GUI" {
		t.Fatal(string(b))
	}
	b, _ = os.ReadFile(filepath.Join(root, "etc/quic-lab/profiles/quic.json"))
	if string(b) != `{"transport":"quic"}` {
		t.Fatal("profile changed")
	}
	link, _ := os.Readlink(filepath.Join(root, "etc/quic-lab/client.json"))
	if link != "profiles/quic.json" {
		t.Fatal(link)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "restart quic-lab-client.service") {
		t.Fatal(calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "enable quic-lab-client") || strings.Contains(c, "disable quic-lab-client") {
			t.Fatal("VPN boot policy changed", c)
		}
	}
}

func TestFailureRestoresOldInstallation(t *testing.T) {
	root, source := fixture(t)
	fail := true
	var calls []string
	if _, err := install(context.Background(), root, source, 1000, fakeRunner(true, &fail, &calls)); err == nil {
		t.Fatal("expected failure")
	}
	for p, want := range map[string]string{"usr/local/bin/quic-lab-client": "OLD_ENGINE", "usr/local/bin/quic-lab-gui": "OLD_GUI", "etc/systemd/system/quic-lab-control.service": "OLD_CONTROL"} {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil || string(b) != want {
			t.Fatal(p, string(b), err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "usr/local/bin/quic-lab")); !os.IsNotExist(err) {
		t.Fatal("new executable not rolled back", err)
	}
}

func TestUnsafeMigrationRejectedBeforeCommands(t *testing.T) {
	root, source := fixture(t)
	fail := false
	var calls []string
	if err := os.Chmod(filepath.Join(root, "usr/local/bin"), 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := install(context.Background(), root, source, 1000, fakeRunner(false, &fail, &calls)); err == nil {
		t.Fatal("unsafe directory accepted")
	}
	if len(calls) != 0 {
		t.Fatal("commands before path validation", calls)
	}
}

func TestInactiveVPNStaysInactive(t *testing.T) {
	root, source := fixture(t)
	fail := false
	var calls []string
	if _, err := install(context.Background(), root, source, 1000, fakeRunner(false, &fail, &calls)); err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if strings.Contains(c, "restart quic-lab-client.service") || strings.Contains(c, "start quic-lab-client.service") {
			t.Fatal(c)
		}
	}
}
