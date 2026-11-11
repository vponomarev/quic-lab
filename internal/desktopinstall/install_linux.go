//go:build linux && !android

// Package desktopinstall migrates an already configured Linux client in place.
package desktopinstall

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Runner func(context.Context, string, ...string) (string, error)

// Install preserves VPN profiles, routing helpers and the VPN boot policy.
func Install(ctx context.Context, login string) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("run install with sudo")
	}
	u, err := user.Lookup(login)
	if err != nil {
		return "", errors.New("unknown desktop user")
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil || uid <= 0 {
		return "", errors.New("choose a non-root desktop user")
	}
	source, err := os.Executable()
	if err != nil {
		return "", err
	}
	return install(ctx, "/", source, uid, command)
}

type record struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Mode   uint32 `json:"mode,omitempty"`
	Link   string `json:"link,omitempty"`
	File   string `json:"file,omitempty"`
}

const controlUnit = `[Unit]
Description=QUIC Lab local desktop control
After=local-fs.target

[Service]
Type=simple
ExecStart=/usr/local/bin/quic-lab control
User=root
UMask=0077
RuntimeDirectory=quic-lab-control
RuntimeDirectoryMode=0755
Restart=on-failure
RestartSec=3
TimeoutStopSec=180
NoNewPrivileges=true
CapabilityBoundingSet=
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_UNIX
ReadWritePaths=/etc/quic-lab /etc/systemd/system

[Install]
WantedBy=multi-user.target
`

const desktopEntry = `[Desktop Entry]
Type=Application
Name=QUIC Lab
Comment=Manage VPN connections
Comment[ru]=Управление VPN-подключениями
Exec=/usr/local/bin/quic-lab gui
Icon=network-vpn
Terminal=false
Categories=Network;
StartupNotify=true
StartupWMClass=ru.vpnc.QuicLab
`

const quickHelp = `QUIC Lab — Linux GUI на Go

Откройте QUIC Lab в меню приложений или выполните quic-lab без sudo.
Выберите профиль, нажмите «Подключить». Для смены активного профиля подтвердите
переподключение. «Отключить» останавливает VPN. Закрытие окна оставляет VPN работать.
Переключатель автозапуска управляет подключением VPN при загрузке компьютера.
Статус, RTT и счётчики трафика обновляются автоматически.

Профили и ключи остаются в /etc/quic-lab и доступны только root.
Редактирование профилей пока выполняется администратором.
Команда quic-lab status показывает безопасный статус без ключей.
Python для работы клиента не требуется. Нужны системные библиотеки OpenGL/X11.
Один исполняемый файл /usr/local/bin/quic-lab обслуживает GUI, VPN и контроллер;
quic-lab-client и quic-lab-gui — совместимые ссылки на этот файл.
Резервные копии установки: /var/backups/quic-lab-client/*-go-gui*/manifest.json.
`

func install(ctx context.Context, root, source string, uid int, run Runner) (backup string, result error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) || uid <= 0 {
		return "", errors.New("invalid installation target")
	}
	path := func(p string) string { return filepath.Join(root, p) }
	files := []string{"usr/local/bin/quic-lab", "usr/local/bin/quic-lab-client", "usr/local/bin/quic-lab-gui", "etc/systemd/system/quic-lab-control.service", "usr/local/share/applications/quic-lab.desktop", "etc/quic-lab/gui-control.json", "usr/local/share/quic-lab/GUI.md"}
	const helper = "usr/local/libexec/quic-lab-network"
	migrateHelper, err := knownNetworkHelper(path(helper))
	if err != nil {
		return "", err
	}
	if migrateHelper {
		files = append(files, helper)
	}
	for _, p := range append(append([]string{}, files...), "var/backups/quic-lab-client/.probe", "etc/quic-lab/profiles/.probe") {
		if err := safeParents(root, filepath.Dir(path(p))); err != nil {
			return "", err
		}
	}
	for _, p := range files {
		if p == helper {
			continue
		} // Validated as exact legacy source or our alias above.
		if err := safeTarget(path(p), p == "usr/local/bin/quic-lab-client" || p == "usr/local/bin/quic-lab-gui"); err != nil {
			return "", err
		}
	}
	selected := path("etc/quic-lab/client.json")
	info, err := os.Lstat(selected)
	if err != nil || info.Mode()&os.ModeSymlink == 0 || !owned(info) {
		return "", errors.New("client.json must be a root-owned managed profile link")
	}
	target, err := filepath.EvalSymlinks(selected)
	if err != nil || filepath.Dir(target) != path("etc/quic-lab/profiles") {
		return "", errors.New("selected profile is outside profiles directory")
	}
	if err := safeTarget(target, false); err != nil {
		return "", err
	}
	info, err = os.Stat(target)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("selected profile must be private")
	}
	system := func(args ...string) (string, error) { return run(ctx, "/usr/bin/systemctl", args...) }
	state, err := system("show", "quic-lab-client.service", "--property=ActiveState,UnitFileState,ExecStart")
	if err != nil {
		return "", errors.New("cannot inspect VPN service")
	}
	expected := "ExecStart={ path=/usr/local/bin/quic-lab-client ; argv[]=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json ; ignore_errors=no ;"
	if !strings.Contains(state, expected) || strings.Count(state, "argv[]=") != 1 {
		return "", errors.New("unmanaged VPN service; refusing migration")
	}
	active := property(state, "ActiveState") == "active"
	if v := property(state, "ActiveState"); v != "active" && v != "inactive" && v != "failed" {
		return "", errors.New("wait for VPN service transition to finish")
	}
	controlState, err := system("show", "quic-lab-control.service", "--property=ActiveState,UnitFileState,LoadState")
	if err != nil {
		return "", errors.New("cannot inspect control service")
	}
	if _, err := run(ctx, source, "engine", "-validate", "-config", selected); err != nil {
		return "", errors.New("new engine rejected current configuration")
	}
	if err := os.MkdirAll(path("var/backups/quic-lab-client"), 0700); err != nil {
		return "", err
	}
	backup, err = os.MkdirTemp(path("var/backups/quic-lab-client"), time.Now().UTC().Format("20060102T150405Z")+"-go-gui-")
	if err != nil {
		return "", err
	}
	var records []record
	for i, p := range files {
		r := record{Path: p}
		info, e := os.Lstat(path(p))
		if e == nil {
			r.Exists, r.Mode = true, uint32(info.Mode().Perm())
			if info.Mode()&os.ModeSymlink != 0 {
				r.Link, e = os.Readlink(path(p))
			} else {
				r.File = fmt.Sprintf("file-%d", i)
				e = copyFile(path(p), filepath.Join(backup, r.File), 0600)
			}
		}
		if e != nil && !os.IsNotExist(e) {
			return backup, e
		}
		records = append(records, r)
	}
	manifest, _ := json.MarshalIndent(records, "", "  ")
	if err := os.WriteFile(filepath.Join(backup, "manifest.json"), manifest, 0600); err != nil {
		return backup, err
	}
	// Once services or files are touched, any failure restores the prior installation.
	defer func() {
		if result == nil {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		rb := func(args ...string) error { _, e := run(rollbackCtx, "/usr/bin/systemctl", args...); return e }
		var failures []error
		failures = append(failures, rb("stop", "quic-lab-control.service"))
		for i := len(records) - 1; i >= 0; i-- {
			r := records[i]
			p := path(r.Path)
			var e error
			if !r.Exists {
				e = os.Remove(p)
				if os.IsNotExist(e) {
					e = nil
				}
			} else if r.Link != "" {
				e = replaceLink(p, r.Link)
			} else {
				e = copyFile(filepath.Join(backup, r.File), p, os.FileMode(r.Mode))
			}
			failures = append(failures, e)
		}
		failures = append(failures, rb("daemon-reload"))
		if property(controlState, "UnitFileState") != "enabled" {
			failures = append(failures, rb("disable", "quic-lab-control.service"))
		}
		if property(controlState, "ActiveState") == "active" {
			failures = append(failures, rb("restart", "quic-lab-control.service"))
		}
		if active {
			failures = append(failures, rb("restart", "quic-lab-client.service"))
		}
		if errors.Join(failures...) != nil {
			result = fmt.Errorf("migration failed; rollback needs inspection; backup: %s: %w", backup, result)
		} else {
			result = fmt.Errorf("migration failed; previous installation restored; backup: %s: %w", backup, result)
		}
	}()
	if property(controlState, "LoadState") != "not-found" {
		if _, err = system("stop", "quic-lab-control.service"); err != nil {
			return backup, err
		}
	}
	for _, p := range files {
		if err = os.MkdirAll(filepath.Dir(path(p)), 0755); err != nil {
			return backup, err
		}
	}
	if err = copyFile(source, path(files[0]), 0755); err != nil {
		return backup, err
	}
	for _, p := range files[1:3] {
		if err = replaceLink(path(p), "quic-lab"); err != nil {
			return backup, err
		}
	}
	if migrateHelper {
		if err = replaceLink(path(helper), "../bin/quic-lab"); err != nil {
			return backup, err
		}
	}
	settings, _ := json.Marshal(map[string]int{"allowed_uid": uid})
	contents := []string{controlUnit, desktopEntry, string(settings) + "\n", quickHelp}
	for i, content := range contents {
		mode := os.FileMode(0644)
		if i == 2 {
			mode = 0600
		}
		if err = writeFile(path(files[i+3]), []byte(content), mode); err != nil {
			return backup, err
		}
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "quic-lab-control.service"}, {"restart", "quic-lab-control.service"}, {"is-active", "--quiet", "quic-lab-control.service"}} {
		if _, err = system(args...); err != nil {
			return backup, errors.New("control service migration failed")
		}
	}
	if active {
		if _, err = system("restart", "quic-lab-client.service"); err != nil {
			return backup, errors.New("VPN restart failed")
		}
		if _, err = system("is-active", "--quiet", "quic-lab-client.service"); err != nil {
			return backup, errors.New("VPN did not remain active")
		}
	}
	// Type=simple becomes active before application initialization. Require the
	// authenticated controller to answer and confirm the new engine's startup.
	readyCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	for {
		response, e := run(readyCtx, path(files[0]), "status")
		var status struct {
			Selected string `json:"selected"`
			Active   string `json:"active"`
			Started  bool   `json:"started"`
		}
		if e == nil && json.Unmarshal([]byte(response), &status) == nil && status.Selected != "" {
			if active && status.Active == "active" && status.Started {
				break
			}
			if !active && (status.Active == "inactive" || status.Active == "failed") {
				break
			}
		}
		select {
		case <-readyCtx.Done():
			return backup, errors.New("controller or VPN did not confirm readiness")
		case <-time.After(250 * time.Millisecond):
		}
	}
	return backup, nil
}

func knownNetworkHelper(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 && owned(info) {
		link, e := os.Readlink(path)
		if e == nil && link == "../bin/quic-lab" {
			return true, nil
		}
	}
	if err = safeTarget(path, false); err != nil {
		return false, err
	}
	if info.Size() > 8192 {
		return false, errors.New("unrecognized network helper; refusing to replace it")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	// The audited laptop helper has no keys or configuration embedded in it.
	const legacy = "11d0649aa273c73d3d0e078b8cdc0588c102b5b0d3a4dccd998e4e219f41a277"
	if fmt.Sprintf("%x", sha256.Sum256(b)) != legacy {
		return false, errors.New("unrecognized network helper; refusing to replace it")
	}
	return true, nil
}

func property(s, key string) string {
	for _, line := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v
		}
	}
	return ""
}
func owned(info os.FileInfo) bool { s, ok := info.Sys().(*syscall.Stat_t); return ok && s.Uid == 0 }
func safeParents(root, dir string) error {
	for {
		info, err := os.Lstat(dir)
		if err == nil {
			if !info.IsDir() || !owned(info) || info.Mode().Perm()&0022 != 0 {
				return fmt.Errorf("unsafe installation directory: %s", dir)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if dir == root {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return errors.New("path outside installation root")
		}
		dir = parent
	}
}
func safeTarget(path string, alias bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !owned(info) {
		return fmt.Errorf("installation file not owned by root: %s", path)
	}
	if alias && info.Mode()&os.ModeSymlink != 0 {
		target, e := os.Readlink(path)
		if e == nil && target == "quic-lab" {
			return nil
		}
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("unsafe installation file: %s", path)
	}
	return nil
}
func copyFile(from, to string, mode os.FileMode) error {
	f, err := os.Open(from)
	if err != nil {
		return err
	}
	defer f.Close()
	return atomicFile(to, mode, func(w io.Writer) error { _, e := io.Copy(w, f); return e })
}
func writeFile(path string, b []byte, mode os.FileMode) error {
	return atomicFile(path, mode, func(w io.Writer) error { _, e := w.Write(b); return e })
}
func atomicFile(path string, mode os.FileMode, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".quic-install-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = write(f); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func replaceLink(path, target string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".quic-link-")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	if err = os.Remove(name); err != nil {
		return err
	}
	if err = os.Symlink(target, name); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Output is bounded and never printed by the installer; engine validation can
// otherwise expose endpoints or credentials through diagnostic errors.
type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > 65536 {
		return 0, errors.New("command output too large")
	}
	b.data = append(b.data, p...)
	return n, nil
}
func command(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err != nil {
		return "", errors.New("installation command failed")
	}
	return string(out.data), nil
}
