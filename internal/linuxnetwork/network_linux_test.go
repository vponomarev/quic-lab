package linuxnetwork

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type testRunner struct {
	calls  [][]string
	failAt int
}

func (r *testRunner) run(_ context.Context, args []string) error {
	r.calls = append(r.calls, append([]string{}, args...))
	if len(r.calls) == r.failAt {
		return errors.New("SECRET system command failure")
	}
	return nil
}

func networkFixture(t *testing.T, config string) (*helper, *testRunner, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("fixture requires root-owned files")
	}
	base := t.TempDir()
	root := filepath.Join(base, "quic-lab")
	resolved := filepath.Join(base, "systemd")
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(resolved, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles", "quic.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("profiles/quic.json", filepath.Join(root, "client.json")); err != nil {
		t.Fatal(err)
	}
	runner := &testRunner{}
	return &helper{configRoot: root, systemdRoot: resolved, run: runner.run}, runner, filepath.Join(resolved, "resolved.conf.d", "90-quic-lab-client.conf")
}

func TestCoveredDNSStartAndStopRestoresNetwork(t *testing.T) {
	h, r, path := networkFixture(t, `{"transport":"quic","dns":"1.1.1.1","targets":["1.1.1.0/24"]}`)
	if err := h.execute(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "[Resolve]\nDNS=1.1.1.1\nFallbackDNS=\nDomains=~.\n" {
		t.Fatalf("%q", content)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0644 {
		t.Fatal(info.Mode())
	}
	if err = h.execute(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dropin not removed", err)
	}
	want := [][]string{
		{"/usr/bin/systemctl", "reset-failed", "systemd-resolved"}, {"/usr/bin/systemctl", "restart", "systemd-resolved"},
		{"/usr/sbin/sysctl", "-q", "-w", "net.ipv6.conf.all.disable_ipv6=1", "net.ipv6.conf.default.disable_ipv6=1"},
		{"/usr/sbin/sysctl", "-q", "-w", "net.ipv6.conf.all.disable_ipv6=0", "net.ipv6.conf.default.disable_ipv6=0"},
		{"/usr/bin/systemctl", "reset-failed", "systemd-resolved"}, {"/usr/bin/systemctl", "restart", "systemd-resolved"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("commands: %#v", r.calls)
	}
}

func TestUncoveredDNSOnlyDisablesIPv6AndDefaultDNS(t *testing.T) {
	for _, config := range []string{`{"transport":"quic","dns":"8.8.8.8","targets":["1.1.1.0/24"]}`, `{"transport":"quic","targets":[]}`} {
		h, r, path := networkFixture(t, config)
		if err := h.execute(context.Background(), "start"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("uncovered DNS override created")
		}
		if len(r.calls) != 1 || r.calls[0][0] != "/usr/sbin/sysctl" {
			t.Fatal(r.calls)
		}
	}
	for _, dns := range []string{"", `,"dns":""`} {
		h, _, path := networkFixture(t, `{"transport":"quic","targets":["0.0.0.0/0"]`+dns+`}`)
		if err := h.execute(context.Background(), "start"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "DNS=1.1.1.1\n") {
			t.Fatal(string(data))
		}
	}
}

func TestInvalidConfigHasNoEffects(t *testing.T) {
	for _, config := range []string{`{"transport":"quic","dns":"1.1.1.1\nDomains=evil","targets":[]}`, `{"transport":"quic","dns":"::1","targets":[]}`, `{"transport":"quic","dns":true}`, `{"transport":"quic","targets":["0.0.0.0/0","bad"]}`, `{"transport":"quic","targets":["::/0"]}`, `{"transport":"quic","targets":"0.0.0.0/0"}`, `{"transport":"quic","targets":[42]}`, `{"transport":"https","targets":[]}`, `{"transport":"quic","dns":"1.1.1.1","dns":"8.8.8.8"}`, `[]`} {
		h, r, path := networkFixture(t, config)
		if err := h.execute(context.Background(), "start"); err == nil {
			t.Fatal("invalid config accepted", config)
		}
		if len(r.calls) != 0 {
			t.Fatal("invalid config ran commands")
		}
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Fatal("invalid config created directory")
		}
	}
}

func TestExistingDropinIsNeverOverwritten(t *testing.T) {
	h, r, path := networkFixture(t, `{"transport":"quic","targets":["0.0.0.0/0"]}`)
	os.Mkdir(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("existing"), 0644)
	if err := h.execute(context.Background(), "start"); err == nil {
		t.Fatal("existing dropin accepted")
	}
	content, _ := os.ReadFile(path)
	if string(content) != "existing" || len(r.calls) != 0 {
		t.Fatal("existing dropin modified")
	}
}

func TestUnsafeProfileAndDirectoriesRejected(t *testing.T) {
	for _, kind := range []string{"profile_permissions", "profile_owner", "profile_symlink", "profile_fifo", "config_regular", "config_other_target", "config_owner", "profiles_writable", "root_symlink", "systemd_writable", "dropin_dir_symlink"} {
		t.Run(kind, func(t *testing.T) {
			h, r, path := networkFixture(t, `{"transport":"quic","targets":["0.0.0.0/0"]}`)
			profile := filepath.Join(h.configRoot, "profiles", "quic.json")
			selected := filepath.Join(h.configRoot, "client.json")
			switch kind {
			case "profile_permissions":
				os.Chmod(profile, 0644)
			case "profile_owner":
				os.Chown(profile, 65534, 65534)
			case "profile_symlink":
				os.Rename(profile, profile+".real")
				os.Symlink(profile+".real", profile)
			case "profile_fifo":
				os.Remove(profile)
				unix.Mkfifo(profile, 0600)
			case "config_regular":
				os.Remove(selected)
				os.WriteFile(selected, []byte(`{}`), 0600)
			case "config_other_target":
				os.Remove(selected)
				os.Symlink("/etc/shadow", selected)
			case "config_owner":
				os.Lchown(selected, 65534, 65534)
			case "profiles_writable":
				os.Chmod(filepath.Dir(profile), 0777)
			case "root_symlink":
				link := h.configRoot + "-link"
				os.Symlink(h.configRoot, link)
				h.configRoot = link
			case "systemd_writable":
				os.Chmod(h.systemdRoot, 0777)
			case "dropin_dir_symlink":
				os.Symlink(t.TempDir(), filepath.Dir(path))
			}
			if err := h.execute(context.Background(), "start"); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
			if len(r.calls) != 0 {
				t.Fatal("unsafe configuration had command effects")
			}
		})
	}
}

func TestStopPreservesUnownedDropinsAndDoesNotReadConfig(t *testing.T) {
	for _, kind := range []string{"content", "owner", "permissions", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			h, r, path := networkFixture(t, `not JSON`)
			os.Mkdir(filepath.Dir(path), 0755)
			os.WriteFile(path, []byte("[Resolve]\nDNS=1.1.1.1\nFallbackDNS=\nDomains=~.\n"), 0644)
			switch kind {
			case "content":
				os.WriteFile(path, []byte("other"), 0644)
			case "owner":
				os.Chown(path, 65534, 65534)
			case "permissions":
				os.Chmod(path, 0666)
			case "symlink":
				os.Rename(path, path+".target")
				os.Symlink(path+".target", path)
			}
			if err := h.execute(context.Background(), "stop"); err == nil {
				t.Fatal("unsafe dropin accepted")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("unowned dropin removed")
			}
			if len(r.calls) != 1 || r.calls[0][3] != "net.ipv6.conf.all.disable_ipv6=0" {
				t.Fatal(r.calls)
			}
		})
	}
	h, r, _ := networkFixture(t, `invalid JSON`)
	if err := h.execute(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0][0] != "/usr/sbin/sysctl" {
		t.Fatal("stop without dropin changed resolver", r.calls)
	}
}

func TestFailedStartCanBeCleanedByExecStopPost(t *testing.T) {
	for failure := 1; failure <= 3; failure++ {
		h, r, path := networkFixture(t, `{"transport":"quic","targets":["0.0.0.0/0"]}`)
		r.failAt = failure
		err := h.execute(context.Background(), "start")
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("unsanitized or missing failure", err)
		}
		r.failAt = 0
		if err = h.execute(context.Background(), "stop"); err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("ExecStopPost did not clean up")
		}
	}
}

func TestUnknownActionHasNoEffects(t *testing.T) {
	h, r, _ := networkFixture(t, `{}`)
	if err := h.execute(context.Background(), "arbitrary"); err == nil || len(r.calls) != 0 {
		t.Fatal(err, r.calls)
	}
}

func TestCommandRunnerBoundsTimeAndDiscardsOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := runCommand(ctx, []string{"/bin/sh", "-c", "sleep 20"}); err == nil || time.Since(start) > time.Second {
		t.Fatal("command timeout failed", err)
	}
	if err := runCommand(context.Background(), []string{"/bin/sh", "-c", "head -c 2097152 /dev/zero; echo SECRET >&2; exit 3"}); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("output was exposed", err)
	}
}
