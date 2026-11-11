package desktopcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var sockErr error
	err = raw.Control(func(fd uintptr) { cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return cred.Uid, nil
}

func readFrame(r io.Reader, limit int) ([]byte, error) {
	raw, err := bufio.NewReader(io.LimitReader(r, int64(limit)+1)).ReadBytes('\n')
	if err != nil || len(raw) > limit || len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return nil, controlError("invalid_request")
	}
	return raw, nil
}

// Serve owns a socket in a root-owned, non-writable runtime directory. It stops
// accepting and drains its bounded handlers when ctx is cancelled.
func Serve(ctx context.Context, path string, c *Controller) error {
	if os.Geteuid() != 0 {
		return controlError("access_denied")
	}
	if c == nil || !filepath.IsAbs(path) {
		return controlError("unsafe_configuration")
	}
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if st, err := os.Lstat(path); err == nil {
		var stat unix.Stat_t
		if st.Mode()&os.ModeSocket == 0 || unix.Lstat(path, &stat) != nil || stat.Uid != 0 {
			return controlError("unsafe_configuration")
		}
		probe, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			probe.Close()
			return controlError("service_busy")
		}
		if !errors.Is(err, unix.ECONNREFUSED) {
			// A full accept queue can time out despite a live owner. Only an
			// explicit refusal proves there is no listener to displace.
			return controlError("service_busy")
		}
		// Only an unused root-owned socket may be reclaimed.
		if err = os.Remove(path); err != nil {
			return controlError("service_unavailable")
		}
	} else if !os.IsNotExist(err) {
		return controlError("service_unavailable")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return controlError("service_unavailable")
	}
	listener.SetUnlinkOnClose(false)
	owned, err := os.Lstat(path)
	if err != nil {
		listener.Close()
		return controlError("service_unavailable")
	}
	defer func() {
		listener.Close()
		if current, e := os.Lstat(path); e == nil && os.SameFile(owned, current) {
			_ = os.Remove(path)
		}
	}()
	if err = os.Chmod(path, 0666); err != nil {
		return controlError("service_unavailable")
	}
	serving, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(serving, func() { _ = listener.Close() })
	defer stop()
	slots := make(chan struct{}, 8)
	var handlers sync.WaitGroup
	defer func() { cancel(); handlers.Wait() }()
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if serving.Err() != nil {
				return nil
			}
			return controlError("service_unavailable")
		}
		select {
		case slots <- struct{}{}:
			handlers.Add(1)
			go func() { defer handlers.Done(); defer func() { <-slots }(); servePeer(serving, conn, c) }()
		default:
			conn.Close()
		}
	}
}

func servePeer(ctx context.Context, conn *net.UnixConn, c *Controller) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	var status Status
	uid, err := peerUID(conn)
	if err == nil && uid != 0 && uid != c.uid {
		err = controlError("access_denied")
	}
	if err == nil {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var raw []byte
		raw, err = readFrame(conn, 4096)
		if err == nil {
			operation, cancel := context.WithTimeout(ctx, 240*time.Second)
			status, err = c.handle(operation, uid, raw)
			cancel()
		}
	}
	var reply any
	if err != nil {
		reply = struct {
			OK    bool         `json:"ok"`
			Error controlError `json:"error"`
		}{false, safeError(err)}
	} else {
		reply = struct {
			OK   bool   `json:"ok"`
			Data Status `json:"data"`
		}{true, status}
	}
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_ = json.NewEncoder(conn).Encode(reply)
}

// Call authenticates the root peer before sending a command to the fixed socket.
func Call(ctx context.Context, command Command) (Status, error) {
	return callPath(ctx, command, DefaultSocket)
}

func callPath(ctx context.Context, command Command, path string) (Status, error) {
	raw, err := json.Marshal(command)
	if err != nil {
		return Status{}, controlError("invalid_request")
	}
	if _, err = decodeCommand(raw); err != nil {
		return Status{}, err
	}
	timeout := 150 * time.Second
	if command.Op == "status" {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := net.Dialer{}
	socket, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return Status{}, controlError("service_unavailable")
	}
	conn := socket.(*net.UnixConn)
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	uid, err := peerUID(conn)
	if err != nil {
		return Status{}, controlError("service_unavailable")
	}
	if uid != 0 {
		return Status{}, controlError("untrusted_server")
	}
	if _, err = conn.Write(append(raw, '\n')); err != nil {
		return Status{}, controlError("service_unavailable")
	}
	raw, err = readFrame(conn, 65536)
	if err != nil {
		if ctx.Err() != nil {
			return Status{}, controlError("service_unavailable")
		}
		return Status{}, controlError("invalid_response")
	}
	reply, err := decodeObject(raw)
	if err != nil {
		return Status{}, controlError("invalid_response")
	}
	if string(reply["ok"]) == "false" {
		code := controlError(jsonString(reply["error"]))
		if _, ok := errorTexts[code]; !ok {
			return Status{}, controlError("invalid_response")
		}
		return Status{}, code
	}
	if string(reply["ok"]) != "true" {
		return Status{}, controlError("invalid_response")
	}
	data, err := decodeObject(reply["data"])
	if err != nil {
		return Status{}, controlError("invalid_response")
	}
	for _, field := range []string{"selected", "profiles", "active", "autostart", "invocation", "failed", "started", "session_state", "readiness", "events", "tx_bytes", "rx_bytes", "transport_state", "rtt_ms", "probe_at", "probe_interval_ms", "probe"} {
		value, ok := data[field]
		if !ok {
			return Status{}, controlError("invalid_response")
		}
		if string(value) == "null" && !oneOf(field, "selected", "rtt_ms", "probe_at") {
			return Status{}, controlError("invalid_response")
		}
	}
	var status Status
	if err = json.Unmarshal(reply["data"], &status); err != nil {
		return Status{}, controlError("invalid_response")
	}
	if !oneOf(status.Active, "active", "inactive", "failed", "activating", "deactivating", "reloading") || len(status.Events) > 20 || len(status.Profiles) > 4 || (status.Invocation != "" && !validInvocation(status.Invocation)) {
		return Status{}, controlError("invalid_response")
	}
	return status, nil
}
