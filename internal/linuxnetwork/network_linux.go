// Package linuxnetwork implements the installed client's fixed DNS/IPv6 hooks.
package linuxnetwork

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const dropinName = "90-quic-lab-client.conf"
const maxConfig = 1 << 20

var (
	errUnsafe    = errors.New("unsafe network configuration")
	errOperation = errors.New("network operation failed")
	errExisting  = errors.New("network DNS override already exists")
)

type helper struct {
	configRoot  string
	systemdRoot string
	run         func(context.Context, []string) error
}

// Run applies the installed service's start or stop hook. No caller-supplied
// path, address, executable or system setting can be selected through this API.
func Run(ctx context.Context, action string) error {
	if os.Geteuid() != 0 {
		return errors.New("network control requires root")
	}
	h := helper{configRoot: "/etc/quic-lab", systemdRoot: "/run/systemd", run: runCommand}
	return h.execute(ctx, action)
}

func (h *helper) execute(ctx context.Context, action string) error {
	if action != "start" && action != "stop" {
		return errors.New("unsupported network action")
	}
	if ctx.Err() != nil {
		return errOperation
	}
	if action == "stop" {
		// ExecStopPost must restore networking even when the profile is unreadable.
		if err := h.ipv6(ctx, "0"); err != nil {
			return err
		}
		removed, err := h.removeDropin()
		if err != nil {
			return err
		}
		if removed {
			return h.restartResolver(ctx)
		}
		return nil
	}
	dns, covered, err := h.readConfig()
	if err != nil {
		return err
	}
	// All syntax checks precede file creation and commands.
	if covered {
		if err = h.createDropin(dns); err != nil {
			return err
		}
		if err = h.restartResolver(ctx); err != nil {
			return err
		}
	}
	return h.ipv6(ctx, "1")
}

func (h *helper) command(ctx context.Context, args []string) error {
	if err := h.run(ctx, args); err != nil {
		return errOperation
	}
	return nil
}
func (h *helper) ipv6(ctx context.Context, value string) error {
	return h.command(ctx, []string{"/usr/sbin/sysctl", "-q", "-w", "net.ipv6.conf.all.disable_ipv6=" + value, "net.ipv6.conf.default.disable_ipv6=" + value})
}
func (h *helper) restartResolver(ctx context.Context) error {
	if err := h.command(ctx, []string{"/usr/bin/systemctl", "reset-failed", "systemd-resolved"}); err != nil {
		return err
	}
	return h.command(ctx, []string{"/usr/bin/systemctl", "restart", "systemd-resolved"})
}

func runCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errOperation
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "SYSTEMD_PAGER="}
	// Nil stdout/stderr connect directly to the null device: no captured output
	// allocation or raw command errors can escape this privileged helper.
	cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return unix.Kill(-cmd.Process.Pid, unix.SIGKILL) }
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return errOperation
	}
	return nil
}

func openTrustedDir(parent int, path string) (int, error) {
	fd, err := unix.Openat(parent, path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 {
		unix.Close(fd)
		return -1, errUnsafe
	}
	return fd, nil
}

func readRegular(fd int, limit int64, private bool) ([]byte, error) {
	f := os.NewFile(uintptr(fd), "network configuration")
	defer f.Close()
	var st unix.Stat_t
	forbidden := uint32(0022)
	if private {
		forbidden = 0077
	}
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&forbidden != 0 || st.Size > limit {
		return nil, errUnsafe
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errUnsafe
	}
	return data, nil
}

func configObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errUnsafe
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, errUnsafe
		}
		key, ok := token.(string)
		if !ok {
			return nil, errUnsafe
		}
		if _, exists := fields[key]; exists {
			return nil, errUnsafe
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, errUnsafe
		}
		fields[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, errUnsafe
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errUnsafe
	}
	return fields, nil
}

func (h *helper) readConfig() (netip.Addr, bool, error) {
	root, err := openTrustedDir(unix.AT_FDCWD, h.configRoot)
	if err != nil {
		return netip.Addr{}, false, errUnsafe
	}
	defer unix.Close(root)
	profiles, err := openTrustedDir(root, "profiles")
	if err != nil {
		return netip.Addr{}, false, errUnsafe
	}
	defer unix.Close(profiles)
	var st unix.Stat_t
	if unix.Fstatat(root, "client.json", &st, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Mode&unix.S_IFMT != unix.S_IFLNK || st.Uid != 0 {
		return netip.Addr{}, false, errUnsafe
	}
	link := make([]byte, 4096)
	n, err := unix.Readlinkat(root, "client.json", link)
	if err != nil || n == len(link) {
		return netip.Addr{}, false, errUnsafe
	}
	target := string(link[:n])
	if !filepath.IsAbs(target) {
		target = filepath.Join(h.configRoot, target)
	}
	target = filepath.Clean(target)
	name := ""
	for _, id := range []string{"quic", "https", "awg", "vless"} {
		if target == filepath.Join(h.configRoot, "profiles", id+".json") {
			name = id
			break
		}
	}
	if name == "" {
		return netip.Addr{}, false, errUnsafe
	}
	fd, err := unix.Openat(profiles, name+".json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return netip.Addr{}, false, errUnsafe
	}
	raw, err := readRegular(fd, maxConfig, true)
	if err != nil {
		return netip.Addr{}, false, err
	}
	cfg, err := configObject(raw)
	if err != nil {
		return netip.Addr{}, false, err
	}
	var transport string
	if json.Unmarshal(cfg["transport"], &transport) != nil || transport != name {
		return netip.Addr{}, false, errUnsafe
	}
	dnsText := "1.1.1.1"
	if raw, exists := cfg["dns"]; exists {
		if string(raw) == "null" || json.Unmarshal(raw, &dnsText) != nil {
			return netip.Addr{}, false, errUnsafe
		}
		if dnsText == "" {
			dnsText = "1.1.1.1"
		}
	}
	dns, err := netip.ParseAddr(dnsText)
	if err != nil || !dns.Is4() {
		return netip.Addr{}, false, errUnsafe
	}
	var targets []string
	if raw, exists := cfg["targets"]; exists {
		if string(raw) == "null" || json.Unmarshal(raw, &targets) != nil {
			return netip.Addr{}, false, errUnsafe
		}
	}
	covered := false
	for _, target := range targets {
		prefix, err := netip.ParsePrefix(target)
		if err != nil || !prefix.Addr().Is4() {
			return netip.Addr{}, false, errUnsafe
		}
		if prefix.Contains(dns) {
			covered = true
		}
	}
	return dns, covered, nil
}

func (h *helper) dropinDir(create bool) (int, error) {
	parent, err := openTrustedDir(unix.AT_FDCWD, h.systemdRoot)
	if err != nil {
		return -1, errUnsafe
	}
	defer unix.Close(parent)
	if create {
		if err = unix.Mkdirat(parent, "resolved.conf.d", 0755); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, errOperation
		}
	}
	return openTrustedDir(parent, "resolved.conf.d")
}

func dropinContent(dns netip.Addr) string {
	return "[Resolve]\nDNS=" + dns.String() + "\nFallbackDNS=\nDomains=~.\n"
}

func (h *helper) createDropin(dns netip.Addr) error {
	dir, err := h.dropinDir(true)
	if err != nil {
		return errUnsafe
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, dropinName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644)
	if errors.Is(err, unix.EEXIST) {
		return errExisting
	}
	if err != nil {
		return errOperation
	}
	file := os.NewFile(uintptr(fd), "network DNS override")
	complete := false
	defer func() {
		file.Close()
		if !complete {
			_ = unix.Unlinkat(dir, dropinName, 0)
		}
	}()
	if err = file.Chmod(0644); err != nil {
		return errOperation
	}
	if _, err = io.WriteString(file, dropinContent(dns)); err != nil {
		return errOperation
	}
	if err = file.Close(); err != nil {
		return errOperation
	}
	complete = true
	return nil
}

func (h *helper) removeDropin() (bool, error) {
	dir, err := h.dropinDir(false)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, errUnsafe
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, dropinName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, errUnsafe
	}
	raw, err := readRegular(fd, 256, false)
	if err != nil {
		return false, err
	}
	// The exact legacy template is the ownership marker; never remove an
	// administrator's arbitrary drop-in or a symlink, including during cleanup.
	text := string(raw)
	const prefix = "[Resolve]\nDNS="
	const suffix = "\nFallbackDNS=\nDomains=~.\n"
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
		return false, errUnsafe
	}
	dns, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(text, prefix), suffix))
	if err != nil || !dns.Is4() || text != dropinContent(dns) {
		return false, errUnsafe
	}
	if unix.Unlinkat(dir, dropinName, 0) != nil {
		return false, errOperation
	}
	return true, nil
}
