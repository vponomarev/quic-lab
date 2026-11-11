//go:build linux

package desktopui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"golang.org/x/sys/unix"
)

// Run must be called from main. Merely importing this package does not create a
// Fyne application, open a display, or contact the privileged controller.
func Run() error {
	if os.Geteuid() == 0 {
		return errors.New("запускайте GUI от обычного пользователя, без sudo")
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("для GUI нужен графический сеанс пользователя")
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Geteuid())
	}
	lock, err := instanceLock(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	application := app.NewWithID("ru.vpnc.QuicLab")
	window := application.NewWindow("QUIC Lab")
	window.Resize(fyne.NewSize(620, 640))
	window.CenterOnScreen()
	view := NewView(window, nil)
	defer view.Close()
	view.Start()
	window.ShowAndRun()
	return nil
}

// The per-user runtime directory is already supplied by the login session. A
// lock survives crashes safely without deleting or replacing another process's
// inode, and is held for the complete GUI lifetime.
func instanceLock(dir string) (*os.File, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("недоступен личный каталог графического сеанса")
	}
	stat := unix.Stat_t{}
	if err := unix.Lstat(dir, &stat); err != nil || stat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("неверный владелец каталога графического сеанса")
	}
	fd, err := unix.Open(filepath.Join(dir, "quic-lab-gui.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("не удалось открыть блокировку окна QUIC Lab")
	}
	file := os.NewFile(uintptr(fd), "quic-lab-gui.lock")
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || stat.Mode&0077 != 0 {
		file.Close()
		return nil, errors.New("небезопасный файл блокировки окна QUIC Lab")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("окно QUIC Lab уже запущено")
	}
	return file, nil
}
