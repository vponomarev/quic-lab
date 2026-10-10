//go:build linux

package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type CreateResult struct {
	FormatVersion int       `json:"format_version"`
	JobID         string    `json:"job_id"`
	Type          Kind      `json:"type"`
	CreatedAt     time.Time `json:"created_at"`
	Path          string    `json:"path"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	ServerVersion string    `json:"server_version"`
}
type localRequest struct {
	Kind      Kind   `json:"type"`
	Password  []byte `json:"password"`
	RequestID string `json:"request_id"`
}

func peerUID(c net.Conn) (uint32, error) {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.New("local connection required")
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var inner error
	err = raw.Control(func(fd uintptr) { cred, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return 0, err
	}
	if inner != nil {
		return 0, inner
	}
	return cred.Uid, nil
}

type privateListener struct{ net.Listener }

func (l privateListener) Accept() (net.Conn, error) {
	for {
		c, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		uid, e := peerUID(c)
		if e == nil && (uid == 0 || uid == uint32(os.Geteuid())) {
			return c, nil
		}
		c.Close()
	}
}
func ServeLocal(ctx context.Context, listener net.Listener, m *Manager) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /create", func(w http.ResponseWriter, r *http.Request) {
		var in localRequest
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			http.Error(w, "invalid request", 400)
			return
		}
		defer clear(in.Password)
		j, e := m.Start(r.Context(), in.Kind, in.Password, in.RequestID)
		if e != nil {
			status := 400
			if errors.Is(e, ErrBusy) {
				status = 409
			}
			http.Error(w, "backup unavailable", status)
			return
		}
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()
		for j.State != "ready" && j.State != "error" {
			select {
			case <-r.Context().Done():
				return
			case <-timer.C:
			}
			j, e = m.Get(j.ID)
			if e != nil {
				http.Error(w, "backup unavailable", 500)
				return
			}
		}
		if j.State != "ready" {
			http.Error(w, "backup creation failed", 500)
			return
		}
		stream, info, e := m.Open(j.ID)
		if e != nil {
			http.Error(w, "backup unavailable", 500)
			return
		}
		defer stream.Close()
		meta, _ := json.Marshal(info)
		w.Header().Set("X-Backup-Job", base64.StdEncoding.EncodeToString(meta))
		w.Header().Set("Content-Length", fmt.Sprint(info.SizeBytes))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		io.Copy(w, stream)
	})
	mux.HandleFunc("POST /ack/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := m.Delete(r.PathValue("id")); e != nil {
			http.Error(w, "backup unavailable", 404)
			return
		}
		w.WriteHeader(204)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Minute, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	stop := context.AfterFunc(ctx, func() { server.Close() })
	defer stop()
	e := server.Serve(privateListener{listener})
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func CreateLocal(ctx context.Context, socket string, kind Kind, password []byte, output string) (CreateResult, error) {
	var zero CreateResult
	info, e := os.Lstat(socket)
	if e != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 {
		return zero, ErrUnavailable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return zero, errors.New("cannot verify backup socket owner")
	}
	output, e = filepath.Abs(output)
	if e != nil {
		return zero, e
	}
	if _, e = os.Lstat(output); !os.IsNotExist(e) {
		return zero, errors.New("backup output already exists or is inaccessible")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		c, e := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if e != nil {
			return nil, e
		}
		uid, e := peerUID(c)
		if e != nil || uid != stat.Uid {
			c.Close()
			return nil, errors.New("backup socket owner mismatch")
		}
		return c, nil
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Minute}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return zero, e
	}
	raw, e := json.Marshal(localRequest{Kind: kind, Password: password, RequestID: hex.EncodeToString(id[:])})
	if e != nil {
		return zero, e
	}
	defer clear(raw)
	req, e := http.NewRequestWithContext(ctx, "POST", "http://local/create", bytes.NewReader(raw))
	if e != nil {
		return zero, e
	}
	resp, e := client.Do(req)
	if e != nil {
		return zero, fmt.Errorf("local backup request failed: %w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return zero, ErrBusy
	}
	if resp.StatusCode != 200 {
		return zero, errors.New("server backup creation failed")
	}
	meta, e := base64.StdEncoding.DecodeString(resp.Header.Get("X-Backup-Job"))
	if e != nil || len(meta) > 16384 {
		return zero, errors.New("invalid backup response")
	}
	var j Job
	if json.Unmarshal(meta, &j) != nil || !jobIDPattern.MatchString(j.ID) || j.SizeBytes <= 0 || j.SizeBytes > 1<<50 || len(j.SHA256) != 64 || j.Kind != kind {
		return zero, errors.New("invalid backup response")
	}
	f, e := os.CreateTemp(filepath.Dir(output), ".partial-backup-")
	if e != nil {
		return zero, e
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, j.SizeBytes+1))
	if e == nil && (n != j.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != j.SHA256) {
		e = errors.New("backup transfer checksum mismatch")
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return zero, e
	}
	// Same-directory hard link is an atomic no-replace publication.
	if e = os.Link(f.Name(), output); e != nil {
		return zero, e
	}
	directory, e := os.Open(filepath.Dir(output))
	if e != nil {
		return zero, e
	}
	e = directory.Sync()
	directory.Close()
	if e != nil {
		return zero, e
	}
	ack, e := http.NewRequestWithContext(ctx, "POST", "http://local/ack/"+j.ID, nil)
	if e == nil {
		if response, err := client.Do(ack); err == nil {
			response.Body.Close()
		}
	}
	return CreateResult{FormatVersion: 1, JobID: j.ID, Type: j.Kind, CreatedAt: j.CreatedAt, Path: output, SizeBytes: j.SizeBytes, SHA256: j.SHA256, ServerVersion: j.ServerVersion}, nil
}
