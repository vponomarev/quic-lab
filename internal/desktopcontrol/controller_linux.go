package desktopcontrol

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	unit       = "quic-lab-client.service"
	systemctl  = "/usr/bin/systemctl"
	journalctl = "/usr/bin/journalctl"
	vpn        = "/usr/local/bin/quic-lab-client"
)

// Controller serializes operations on the single managed system service.
type Controller struct {
	root string
	uid  uint32
	lock chan struct{}
	run  func(context.Context, []string, time.Duration) (string, error)
}

// Monotonic returns Linux CLOCK_MONOTONIC seconds, matching journal timestamps.
func Monotonic() float64 {
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts) != nil {
		return 0
	}
	return float64(ts.Sec) + float64(ts.Nsec)/1e9
}

// New checks the root-owned installation and authorized non-root user.
func New(root string) (*Controller, error) {
	if root == "" {
		root = "/etc/quic-lab"
	}
	if !filepath.IsAbs(root) {
		return nil, controlError("unsafe_configuration")
	}
	root = filepath.Clean(root)
	if err := trustedDirectory(root); err != nil {
		return nil, err
	}
	if err := trustedDirectory(filepath.Join(root, "profiles")); err != nil {
		return nil, err
	}
	cfg, err := privateJSON(filepath.Join(root, "gui-control.json"))
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(string(cfg["allowed_uid"]), 10, 32)
	if err != nil || uid == 0 {
		return nil, controlError("unsafe_configuration")
	}
	return &Controller{root: root, uid: uint32(uid), lock: make(chan struct{}, 1), run: runCommand}, nil
}

func trustedDirectory(path string) error {
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0022 != 0 {
		return controlError("unsafe_configuration")
	}
	return nil
}

func privateJSON(path string) (map[string]json.RawMessage, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, controlError("unsafe_configuration")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0077 != 0 || st.Size > maxOutput {
		return nil, controlError("unsafe_configuration")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxOutput+1))
	if err != nil || len(raw) > maxOutput {
		return nil, controlError("unsafe_configuration")
	}
	value, err := decodeObject(raw)
	if err != nil {
		return nil, controlError("unsafe_configuration")
	}
	return value, nil
}

// limitedOutput stops the process pipe copy immediately at its allocation limit.
type limitedOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > maxOutput-b.buffer.Len() {
		b.overflow = true
		return 0, controlError("response_too_large")
	}
	return b.buffer.Write(p)
}

func runCommand(ctx context.Context, args []string, timeout time.Duration) (string, error) {
	if len(args) == 0 {
		return "", controlError("operation_failed")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "SYSTEMD_PAGER="}
	cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
	// Kill the command's process group, including children holding stdout open.
	cmd.Cancel = func() error { return unix.Kill(-cmd.Process.Pid, unix.SIGKILL) }
	cmd.WaitDelay = 100 * time.Millisecond
	var out limitedOutput
	cmd.Stdout = &out
	err := cmd.Run()
	if out.overflow {
		return "", controlError("response_too_large")
	}
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok && args[0] == journalctl && e.ExitCode() == 1 && out.buffer.Len() == 0 {
			return "", nil
		}
		return "", controlError("operation_failed")
	}
	return out.buffer.String(), nil
}

func (c *Controller) profile(name string) (string, error) {
	if !knownProfile(name) {
		return "", controlError("invalid_request")
	}
	if err := trustedDirectory(c.root); err != nil {
		return "", err
	}
	if err := trustedDirectory(filepath.Join(c.root, "profiles")); err != nil {
		return "", err
	}
	path := filepath.Join(c.root, "profiles", name+".json")
	cfg, err := privateJSON(path)
	if err != nil {
		return "", err
	}
	if jsonString(cfg["transport"]) != name {
		return "", controlError("unsafe_configuration")
	}
	return path, nil
}

func (c *Controller) selected() string {
	path := filepath.Join(c.root, "client.json")
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFLNK || st.Uid != 0 {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	for _, p := range profiles {
		if resolved == filepath.Join(c.root, "profiles", p.ID+".json") {
			return p.ID
		}
	}
	return ""
}

func (c *Controller) system(ctx context.Context) (map[string]string, error) {
	raw, err := c.run(ctx, []string{systemctl, "show", unit, "--no-pager", "--property=ActiveState,SubState,UnitFileState,InvocationID,Result,ExecMainStatus,ExecStart"}, 8*time.Second)
	if err != nil {
		return nil, err
	}
	props := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			props[key] = value
		}
	}
	if !oneOf(props["ActiveState"], "active", "inactive", "failed", "activating", "deactivating", "reloading") {
		return nil, controlError("service_unavailable")
	}
	return props, nil
}

func managed(system map[string]string) error {
	const prefix = "{ path=/usr/local/bin/quic-lab-client ; argv[]=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json ; ignore_errors=no ;"
	value := system["ExecStart"]
	if !strings.HasPrefix(value, prefix) || strings.Count(value, "argv[]=") != 1 || strings.Count(value, "path=") != 1 || strings.Count(value, "{") != 1 || strings.Count(value, "}") != 1 || !strings.HasSuffix(value, "}") {
		return controlError("unmanaged_service")
	}
	return nil
}

func (c *Controller) status(ctx context.Context) (Status, error) {
	system, err := c.system(ctx)
	if err != nil {
		return Status{}, err
	}
	if err = managed(system); err != nil {
		return Status{}, err
	}
	invocation := system["InvocationID"]
	raw, err := c.run(ctx, []string{journalctl, "-u", unit, "-n", "80", "-o", "json", "--no-pager", "--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID"}, 8*time.Second)
	if err != nil {
		return Status{}, err
	}
	events := publicEvents(raw, invocation)
	probe := emptyProbe()
	if validInvocation(invocation) {
		transitions, err := c.run(ctx, []string{journalctl, "-u", unit, "-n", "30", "-o", "json", "--no-pager", "_SYSTEMD_INVOCATION_ID=" + invocation, `--grep="(state|readiness)"`, "--reverse", "--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID"}, 8*time.Second)
		if err != nil {
			return Status{}, err
		}
		lines := strings.Split(strings.TrimSuffix(transitions, "\n"), "\n")
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		state := publicEvents(strings.Join(lines, "\n"), invocation)
		for _, event := range events.Events {
			if strings.HasPrefix(event, "flow_") || strings.HasPrefix(event, "reload_") {
				state.Events = append(state.Events, event)
			}
		}
		if len(state.Events) > 20 {
			state.Events = state.Events[len(state.Events)-20:]
		}
		events.Started, events.SessionState, events.Readiness, events.Events = state.Started, state.SessionState, state.Readiness, state.Events
		diagnostic, err := c.run(ctx, []string{journalctl, "-u", unit, "-n", "1", "-o", "json", "--no-pager", "_SYSTEMD_INVOCATION_ID=" + invocation, "--reverse", `--grep="event"[[:space:]]*:[[:space:]]*"(echo|health|connected|disconnected|probe_unavailable)"`, "--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID,__MONOTONIC_TIMESTAMP"}, 8*time.Second)
		if err != nil {
			return Status{}, err
		}
		probe = publicProbe(diagnostic, invocation, Monotonic())
	}
	after, err := c.system(ctx)
	if err != nil {
		return Status{}, err
	}
	if err = managed(after); err != nil {
		return Status{}, err
	}
	if after["InvocationID"] != invocation {
		events = emptyEvents()
		probe = emptyProbe()
	}
	system = after
	invocation = system["InvocationID"]
	if system["ActiveState"] != "active" {
		probe = emptyProbe()
	}
	events.Selected = c.selected()
	events.Profiles = []Profile{}
	for _, p := range profiles {
		if _, err := c.profile(p.ID); err == nil {
			events.Profiles = append(events.Profiles, p)
		}
	}
	events.Active = system["ActiveState"]
	events.Autostart = system["UnitFileState"] == "enabled"
	if validInvocation(invocation) {
		events.Invocation = invocation
	}
	if result, ok := system["Result"]; ok {
		events.Failed = result != "success"
	}
	events.TransportState, events.RTTMS, events.ProbeAt, events.ProbeIntervalMS, events.Probe = probe.TransportState, probe.RTTMS, probe.ProbeAt, probe.ProbeIntervalMS, probe.Probe
	return events, nil
}

func (c *Controller) replace(target string) error {
	if err := trustedDirectory(c.root); err != nil {
		return err
	}
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return controlError("operation_failed")
	}
	temporary := filepath.Join(c.root, fmt.Sprintf(".gui-link-%x", suffix))
	if err := os.Symlink(target, temporary); err != nil {
		return controlError("operation_failed")
	}
	defer os.Remove(temporary)
	if err := os.Rename(temporary, filepath.Join(c.root, "client.json")); err != nil {
		return controlError("operation_failed")
	}
	return nil
}

func (c *Controller) start(ctx context.Context) error {
	state, err := c.system(ctx)
	if err != nil {
		return err
	}
	if err = managed(state); err != nil {
		return err
	}
	if state["ActiveState"] == "failed" || (state["Result"] != "" && state["Result"] != "success") {
		if _, err = c.run(ctx, []string{systemctl, "reset-failed", unit}, 8*time.Second); err != nil {
			return err
		}
	}
	if _, err = c.run(ctx, []string{systemctl, "start", unit}, 35*time.Second); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		current, err := c.status(ctx)
		if err != nil {
			return err
		}
		if current.Active == "active" && current.Started {
			return nil
		}
		if current.Active == "inactive" || current.Active == "failed" {
			break
		}
		select {
		case <-ctx.Done():
			return controlError("start_not_ready")
		case <-time.After(250 * time.Millisecond):
		}
	}
	return controlError("start_not_ready")
}

func (c *Controller) connect(ctx context.Context, name string, force bool) error {
	path, err := c.profile(name)
	if err != nil {
		return err
	}
	old := c.selected()
	if old == "" {
		return controlError("unmanaged_profile")
	}
	if _, err = c.profile(old); err != nil {
		return err
	}
	system, err := c.system(ctx)
	if err != nil {
		return err
	}
	if err = managed(system); err != nil {
		return err
	}
	if !oneOf(system["ActiveState"], "active", "inactive", "failed") {
		return controlError("service_busy")
	}
	running := system["ActiveState"] == "active"
	if running && name == old && !force {
		return nil
	}
	if _, err = c.run(ctx, []string{vpn, "-validate", "-config", path}, 15*time.Second); err != nil {
		return err
	}
	previous, err := os.Readlink(filepath.Join(c.root, "client.json"))
	if err != nil {
		return controlError("unmanaged_profile")
	}
	_, err = c.run(ctx, []string{systemctl, "stop", unit}, 35*time.Second)
	if err == nil {
		err = c.replace(path)
	}
	if err == nil {
		err = c.start(ctx)
	}
	if err == nil {
		return nil
	}
	// Cancellation must not strand a changed profile; restoration has its own limit.
	rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 120*time.Second)
	defer cancel()
	if _, err = c.run(rollback, []string{systemctl, "stop", unit}, 35*time.Second); err != nil {
		return controlError("rollback_failed")
	}
	if err = c.replace(previous); err != nil {
		return controlError("rollback_failed")
	}
	if running {
		if err = c.start(rollback); err != nil {
			return controlError("rollback_failed")
		}
	}
	return controlError("switch_failed_rolled_back")
}

func (c *Controller) handle(ctx context.Context, uid uint32, raw []byte) (Status, error) {
	if uid != 0 && uid != c.uid {
		return Status{}, controlError("access_denied")
	}
	command, err := decodeCommand(raw)
	if err != nil {
		return Status{}, err
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case c.lock <- struct{}{}:
		defer func() { <-c.lock }()
	case <-timer.C:
		return Status{}, controlError("service_busy")
	case <-ctx.Done():
		return Status{}, controlError("service_unavailable")
	}
	if command.Op != "status" {
		system, err := c.system(ctx)
		if err != nil {
			return Status{}, err
		}
		if err = managed(system); err != nil {
			return Status{}, err
		}
	}
	switch command.Op {
	case "connect":
		err = c.connect(ctx, command.Profile, false)
	case "reconnect":
		name := c.selected()
		if name == "" {
			err = controlError("unmanaged_profile")
		} else {
			err = c.connect(ctx, name, true)
		}
	case "disconnect":
		_, err = c.run(ctx, []string{systemctl, "stop", unit}, 35*time.Second)
	case "autostart":
		op := "disable"
		if *command.Enabled {
			op = "enable"
		}
		_, err = c.run(ctx, []string{systemctl, op, unit}, 8*time.Second)
	}
	if err != nil {
		return Status{}, safeError(err)
	}
	return c.status(ctx)
}
