package desktopcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func startTestServer(t *testing.T, c *Controller, path string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, path, c) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return cancel, done
		}
		select {
		case err := <-done:
			t.Fatal(err)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	t.Fatal("server did not bind")
	return nil, nil
}

func TestSocketProtocolAndShutdown(t *testing.T) {
	c, _, root := fixture(t)
	path := filepath.Join(root, "control.sock")
	cancel, done := startTestServer(t, c, path)
	defer cancel()
	got, err := callPath(context.Background(), Command{Op: "status"}, path)
	if err != nil || got.Selected != "quic" || len(got.Profiles) != 4 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, raw := range []string{strings.Repeat("x", 4097), `{"op":"status","unknown":true}` + "\n"} {
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		fmt.Fprint(conn, raw)
		var reply struct{ Error string }
		if err = json.NewDecoder(conn).Decode(&reply); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		if reply.Error != "invalid_request" {
			t.Fatal(reply)
		}
	}
	idle, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not drain connections")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("socket not removed", err)
	}
}

func TestServeDoesNotRemoveUnrelatedFileOrLiveSocket(t *testing.T) {
	c, _, root := fixture(t)
	path := filepath.Join(root, "control.sock")
	os.WriteFile(path, []byte("preserve"), 0600)
	if err := Serve(context.Background(), path, c); err == nil {
		t.Fatal("regular file overwritten")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "preserve" {
		t.Fatal("file changed")
	}
	os.Remove(path)
	cancel, done := startTestServer(t, c, path)
	defer func() { cancel(); <-done }()
	if err := Serve(context.Background(), path, c); err != controlError("service_busy") {
		t.Fatal("live socket replaced", err)
	}
	if _, err := callPath(context.Background(), Command{Op: "status"}, path); err != nil {
		t.Fatal(err)
	}
}

func TestCallRejectsMalformedOversizedAndCancelledResponse(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root peer")
	}
	for _, raw := range []string{`{"ok":"yes","data":{}}` + "\n", `{"ok":false,"error":"SECRET"}` + "\n", `{"ok":true,"data":null}` + "\n", `{"ok":true,"data":{}}` + "\n", strings.Repeat("x", 65537)} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fake.sock")
			l, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			go func() {
				conn, e := l.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				bufio.NewReader(conn).ReadString('\n')
				fmt.Fprint(conn, raw)
			}()
			if _, err := callPath(context.Background(), Command{Op: "status"}, path); err != controlError("invalid_response") {
				t.Fatal(err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "slow.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, e := l.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		<-done
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := callPath(ctx, Command{Op: "status"}, path); err == nil || time.Since(started) > time.Second {
		t.Fatal("cancellation ignored", err)
	}
}

// A child test process changes real kernel credentials; no mocked UID is involved.
func TestSocketPeerProcess(t *testing.T) {
	mode := os.Getenv("QUIC_TEST_PEER_MODE")
	if mode == "" {
		return
	}
	path := os.Getenv("QUIC_TEST_PEER_PATH")
	if mode == "server" {
		l, err := net.Listen("unix", path)
		if err != nil {
			fmt.Println(err)
			os.Exit(3)
		}
		defer l.Close()
		os.Chmod(path, 0666)
		fmt.Println("ready")
		conn, err := l.Accept()
		if err != nil {
			os.Exit(4)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		defer conn.Close()
		io.Copy(io.Discard, conn)
		return
	}
	status, err := callPath(context.Background(), Command{Op: "status"}, path)
	if mode == "allowed" && err == nil && status.Selected == "quic" {
		return
	}
	if mode == "denied" && err == controlError("access_denied") {
		return
	}
	t.Fatalf("mode=%s: %+v %v", mode, status, err)
}

func TestRealAllowedDeniedAndNonRootServerCredentials(t *testing.T) {
	c, _, _ := fixture(t)
	base, err := os.MkdirTemp("/tmp", "quic-control-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	helper := filepath.Join(base, "peer-test")
	dst, err := os.OpenFile(helper, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	dst.Close()
	path := filepath.Join(base, "control.sock")
	cancel, done := startTestServer(t, c, path)
	defer func() { cancel(); <-done }()
	child := func(mode, path string, uid uint32) *exec.Cmd {
		cmd := exec.Command(helper, "-test.run=^TestSocketPeerProcess$")
		cmd.Env = append(os.Environ(), "QUIC_TEST_PEER_MODE="+mode, "QUIC_TEST_PEER_PATH="+path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: []uint32{}}}
		return cmd
	}
	for _, tt := range []struct {
		mode string
		uid  uint32
	}{{"allowed", 65534}, {"denied", 65533}} {
		if output, err := child(tt.mode, path, tt.uid).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", tt.mode, err, output)
		}
	}
	fakeDir := filepath.Join(base, "untrusted")
	os.Mkdir(fakeDir, 0755)
	os.Chown(fakeDir, 65534, 65534)
	fakePath := filepath.Join(fakeDir, "control.sock")
	cmd := child("server", fakePath, 65534)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatalf("%q %v", ready, err)
	}
	if _, err = callPath(context.Background(), Command{Op: "status"}, fakePath); err != controlError("untrusted_server") {
		t.Fatal("non-root server accepted", err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestServePreservesLiveSocketWithFullAcceptQueue(t *testing.T) {
	c, _, root := fixture(t)
	path := filepath.Join(root, "full.sock")
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err = unix.Bind(fd, &unix.SockaddrUnix{Name: path}); err != nil {
		t.Fatal(err)
	}
	if err = unix.Listen(fd, 1); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err = Serve(ctx, path, c); err != controlError("service_busy") {
		t.Error("busy live socket was replaced", err)
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(original, current) {
		t.Fatal("original listening socket lost", err)
	}
}

func TestServeShutdownPreservesReplacementFile(t *testing.T) {
	c, _, root := fixture(t)
	path := filepath.Join(root, "control.sock")
	cancel, done := startTestServer(t, c, path)
	os.Remove(path)
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatal("replacement file removed", err)
	}
}
