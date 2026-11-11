package desktopcontrol

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const managedExec = "{ path=/usr/local/bin/quic-lab-client ; argv[]=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json ; ignore_errors=no ; start_time=n/a ; }"

type fixtureSystem struct {
	active, enabled, invocation, exec string
	failStarts, failStops             int
	calls                             [][]string
	journal                           func([]string) string
	shows                             int
	changeInvocation                  bool
}

func (s *fixtureSystem) run(ctx context.Context, args []string, timeout time.Duration) (string, error) {
	s.calls = append(s.calls, append([]string{}, args...))
	if args[0] == journalctl {
		if s.journal != nil {
			return s.journal(args), nil
		}
		return journal(`{"state":"running","readiness":"engine_ready"}`, s.invocation, ""), nil
	}
	if args[0] == vpn {
		if len(args) != 4 || args[1] != "-validate" || args[2] != "-config" {
			panic("unexpected engine command")
		}
		return "", nil
	}
	if args[0] != systemctl {
		panic("unexpected executable")
	}
	switch args[1] {
	case "show":
		s.shows++
		if s.changeInvocation && s.shows == 2 {
			s.invocation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}
		return fmt.Sprintf("ActiveState=%s\nUnitFileState=%s\nInvocationID=%s\nResult=success\nExecStart=%s\n", s.active, s.enabled, s.invocation, s.exec), nil
	case "stop":
		s.active = "inactive"
		if s.failStops > 0 {
			s.failStops--
			return "", controlError("operation_failed")
		}
	case "start":
		if s.failStarts > 0 {
			s.failStarts--
			return "", controlError("operation_failed")
		}
		s.active = "active"
	case "enable":
		s.enabled = "enabled"
	case "disable":
		s.enabled = "disabled"
	case "reset-failed":
	default:
		panic("unexpected systemctl operation")
	}
	return "", nil
}

func fixture(t *testing.T) (*Controller, *fixtureSystem, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root ownership fixture requires Linux root")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gui-control.json"), []byte(`{"allowed_uid":65534}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"quic", "https", "awg", "vless"} {
		if err := os.WriteFile(filepath.Join(root, "profiles", p+".json"), []byte(fmt.Sprintf(`{"transport":%q,"key":"DO-NOT-LEAK"}`, p)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("profiles/quic.json", filepath.Join(root, "client.json")); err != nil {
		t.Fatal(err)
	}
	c, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	s := &fixtureSystem{active: "active", enabled: "enabled", invocation: invocationA, exec: managedExec}
	c.run = s.run
	return c, s, root
}

func TestStrictSchemaAndUnauthorizedCannotExecute(t *testing.T) {
	c, s, _ := fixture(t)
	for _, raw := range []string{`[]`, `null`, `{"op":"shell"}`, `{"op":"connect","profile":"../../etc/passwd"}`, `{"op":"status","path":"/etc/shadow"}`, `{"op":"autostart","enabled":"yes"}`, `{"op":"autostart","enabled":null}`, `{"op":"status","op":"disconnect"}`, `{"op":"connect"}`, `{"Op":"status"}`, `{"op":"status"} {}`} {
		if _, err := c.handle(context.Background(), 65534, []byte(raw)); err != controlError("invalid_request") {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if _, err := c.handle(context.Background(), 65533, []byte(`{"op":"status"}`)); err != controlError("access_denied") {
		t.Fatal(err)
	}
	if len(s.calls) != 0 {
		t.Fatal("invalid request executed commands")
	}
}

func TestSwitchAutostartAndDisconnect(t *testing.T) {
	c, _, root := fixture(t)
	got, err := c.handle(context.Background(), 65534, []byte(`{"op":"connect","profile":"https"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Selected != "https" || !got.Started || len(got.Profiles) != 4 {
		t.Fatalf("%+v", got)
	}
	target, _ := os.Readlink(filepath.Join(root, "client.json"))
	if target != filepath.Join(root, "profiles", "https.json") {
		t.Fatal(target)
	}
	got, err = c.handle(context.Background(), 65534, []byte(`{"op":"autostart","enabled":false}`))
	if err != nil || got.Autostart || got.Active != "active" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = c.handle(context.Background(), 65534, []byte(`{"op":"disconnect"}`))
	if err != nil || got.Active != "inactive" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSwitchRestoresPriorSymlinkAndRunningState(t *testing.T) {
	for _, tt := range []struct {
		name          string
		running       bool
		starts, stops int
		want          string
	}{{"start", true, 1, 0, "switch_failed_rolled_back"}, {"stop_timeout", true, 0, 1, "switch_failed_rolled_back"}, {"inactive", false, 1, 0, "switch_failed_rolled_back"}, {"rollback_failure", true, 2, 0, "rollback_failed"}} {
		t.Run(tt.name, func(t *testing.T) {
			c, s, root := fixture(t)
			s.failStarts = tt.starts
			s.failStops = tt.stops
			if !tt.running {
				s.active = "inactive"
			}
			_, err := c.handle(context.Background(), 65534, []byte(`{"op":"connect","profile":"https"}`))
			if err != controlError(tt.want) {
				t.Fatal(err)
			}
			target, _ := os.Readlink(filepath.Join(root, "client.json"))
			if target != "profiles/quic.json" {
				t.Fatal("old symlink not restored", target)
			}
			if tt.want != "rollback_failed" && (s.active == "active") != tt.running {
				t.Fatal("prior running state not restored", s.active)
			}
		})
	}
}

func TestUnsafeConfigurationAndUnmanagedUnitCannotStop(t *testing.T) {
	for _, kind := range []string{"permissions", "symlink", "fifo", "transport", "unmanaged_profile", "unmanaged_unit", "extra_command"} {
		t.Run(kind, func(t *testing.T) {
			c, s, root := fixture(t)
			p := filepath.Join(root, "profiles", "https.json")
			switch kind {
			case "permissions":
				os.Chmod(p, 0666)
			case "symlink":
				os.Remove(p)
				os.Symlink("quic.json", p)
			case "fifo":
				os.Remove(p)
				if err := unix.Mkfifo(p, 0600); err != nil {
					t.Fatal(err)
				}
			case "transport":
				os.WriteFile(p, []byte(`{"transport":"quic"}`), 0600)
			case "unmanaged_profile":
				os.Remove(filepath.Join(root, "client.json"))
				os.WriteFile(filepath.Join(root, "client.json"), []byte(`{}`), 0600)
			case "unmanaged_unit":
				s.exec = strings.ReplaceAll(managedExec, "/etc/quic-lab/client.json", "/etc/other.json")
			case "extra_command":
				s.exec = managedExec + " ; " + managedExec
			}
			if _, err := c.handle(context.Background(), 65534, []byte(`{"op":"connect","profile":"https"}`)); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
			for _, args := range s.calls {
				if args[1] == "stop" {
					t.Fatal("unsafe configuration stopped VPN")
				}
			}
		})
	}
}

func TestStatusInvocationChangeDiscardsOldEvents(t *testing.T) {
	c, s, _ := fixture(t)
	s.changeInvocation = true
	got, err := c.handle(context.Background(), 0, []byte(`{"op":"status"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Started || got.SessionState != "unknown" || got.RTTMS != nil || got.Invocation != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("mixed sessions: %+v", got)
	}
}

func TestStatusFindsTransitionsAfterTrafficRotation(t *testing.T) {
	c, s, _ := fixture(t)
	s.journal = func(args []string) string {
		if strings.Contains(strings.Join(args, " "), `--grep="(state|readiness)"`) {
			return journal(`{"event":"readiness","state":"degraded"}`, invocationA, "") + "\n" + journal(`{"state":"running","readiness":"vpn_dns_ready"}`, invocationA, "")
		}
		return journal(`{"event":"traffic","tx_bytes":123,"rx_bytes":456}`, invocationA, "")
	}
	got, err := c.handle(context.Background(), 0, []byte(`{"op":"status"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Started || got.Readiness != "degraded" || got.TXBytes != 123 {
		t.Fatalf("%+v", got)
	}
}

func TestRunnerBoundsOutputAndSanitizesErrors(t *testing.T) {
	_, err := runCommand(context.Background(), []string{"/bin/sh", "-c", "head -c 2097152 /dev/zero"}, time.Second)
	if err != controlError("response_too_large") {
		t.Fatal(err)
	}
	_, err = runCommand(context.Background(), []string{"/bin/sh", "-c", "echo SECRET >&2; exit 3"}, time.Second)
	if err != controlError("operation_failed") || strings.Contains(ErrorText(err), "SECRET") {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = runCommand(context.Background(), []string{"/bin/sh", "-c", "sleep 20"}, 50*time.Millisecond)
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatal("subprocess timeout did not terminate promptly", err)
	}
}

func TestNewRejectsUnsafeRootAndAuthorizationFile(t *testing.T) {
	for _, kind := range []string{"root_writable", "profiles_writable", "config_readable", "config_other_owner", "config_symlink", "root_symlink", "uid_zero", "uid_bool", "uid_overflow", "uid_fraction", "not_object", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			_, _, root := fixture(t)
			config := filepath.Join(root, "gui-control.json")
			switch kind {
			case "root_writable":
				os.Chmod(root, 0777)
			case "profiles_writable":
				os.Chmod(filepath.Join(root, "profiles"), 0777)
			case "config_readable":
				os.Chmod(config, 0644)
			case "config_other_owner":
				os.Chown(config, 65534, 65534)
			case "config_symlink":
				os.Rename(config, config+".real")
				os.Symlink(config+".real", config)
			case "root_symlink":
				link := filepath.Join(t.TempDir(), "link")
				os.Symlink(root, link)
				root = link
			case "uid_zero":
				os.WriteFile(config, []byte(`{"allowed_uid":0}`), 0600)
			case "uid_bool":
				os.WriteFile(config, []byte(`{"allowed_uid":true}`), 0600)
			case "uid_overflow":
				os.WriteFile(config, []byte(`{"allowed_uid":4294967296}`), 0600)
			case "uid_fraction":
				os.WriteFile(config, []byte(`{"allowed_uid":1000.5}`), 0600)
			case "not_object":
				os.WriteFile(config, []byte(`[]`), 0600)
			case "oversized":
				os.WriteFile(config, []byte(strings.Repeat(" ", 1048577)), 0600)
			}
			if _, err := New(root); err != controlError("unsafe_configuration") {
				t.Fatal(err)
			}
		})
	}
}

func TestControllerSerializesRequestsAndReportsBusy(t *testing.T) {
	c, s, _ := fixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	first := true
	c.run = func(ctx context.Context, args []string, timeout time.Duration) (string, error) {
		if first {
			first = false
			close(entered)
			<-release
		}
		return s.run(ctx, args, timeout)
	}
	done := make(chan error, 1)
	go func() { _, err := c.handle(context.Background(), 65534, []byte(`{"op":"status"}`)); done <- err }()
	<-entered
	if _, err := c.handle(context.Background(), 65534, []byte(`{"op":"disconnect"}`)); err != controlError("service_busy") {
		t.Error(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.active != "active" {
		t.Fatal("busy disconnect changed service")
	}
}

func TestCancelledSwitchStillRestoresProfile(t *testing.T) {
	c, s, root := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := true
	c.run = func(ctx context.Context, args []string, timeout time.Duration) (string, error) {
		if ctx.Err() != nil {
			return "", controlError("operation_failed")
		}
		if args[0] == systemctl && args[1] == "start" && first {
			first = false
			cancel()
			return "", controlError("operation_failed")
		}
		return s.run(ctx, args, timeout)
	}
	if _, err := c.handle(ctx, 65534, []byte(`{"op":"connect","profile":"https"}`)); err != controlError("switch_failed_rolled_back") {
		t.Fatal(err)
	}
	target, _ := os.Readlink(filepath.Join(root, "client.json"))
	if target != "profiles/quic.json" || s.active != "active" {
		t.Fatalf("%s %s", target, s.active)
	}
}

func TestReconnectRestartsAlreadySelectedProfile(t *testing.T) {
	c, s, _ := fixture(t)
	if _, err := c.handle(context.Background(), 65534, []byte(`{"op":"connect","profile":"quic"}`)); err != nil {
		t.Fatal(err)
	}
	for _, call := range s.calls {
		if call[1] == "stop" {
			t.Fatal("same-profile connect restarted")
		}
	}
	if _, err := c.handle(context.Background(), 65534, []byte(`{"op":"reconnect"}`)); err != nil {
		t.Fatal(err)
	}
	stopped := false
	for _, call := range s.calls {
		if call[1] == "stop" {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("reconnect did not restart")
	}
}
