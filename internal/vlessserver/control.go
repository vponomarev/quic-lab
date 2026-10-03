package vlessserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"sync"
	"time"
)

const maxControlMessage = 128 * 1024

type ControlClient struct{ Dir string }
type controlResponse struct {
	Revision uint64 `json:"revision"`
	Applied  bool   `json:"applied"`
}
type admissionRequest struct {
	ID string `json:"id"`
}
type admissionResponse struct {
	Milliseconds int64 `json:"milliseconds"`
}

func readMessage(r io.Reader, out any) error {
	var header [4]byte
	if _, e := io.ReadFull(r, header[:]); e != nil {
		return errApply
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > maxControlMessage {
		return errApply
	}
	raw := make([]byte, n)
	if _, e := io.ReadFull(r, raw); e != nil {
		return errApply
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return errApply
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errApply
	}
	return nil
}
func writeMessage(w io.Writer, in any) error {
	raw, e := json.Marshal(in)
	if e != nil || len(raw) > maxControlMessage {
		return errApply
	}
	var b bytes.Buffer
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	b.Write(header[:])
	b.Write(raw)
	if _, e = b.WriteTo(w); e != nil {
		return errApply
	}
	return nil
}
func request(ctx context.Context, dir, name string, timeout time.Duration, in, out any) error {
	if privateDirectory(dir) != nil {
		return errApply
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c, e := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, name))
	if e != nil {
		return errApply
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	c.SetDeadline(deadline)
	if writeMessage(c, in) != nil || readMessage(c, out) != nil {
		return errApply
	}
	return nil
}
func (c *ControlClient) Apply(ctx context.Context, snapshot Snapshot) error {
	var reply controlResponse
	if c == nil || request(ctx, c.Dir, "vless-control.sock", 10*time.Second, snapshot, &reply) != nil || !reply.Applied || reply.Revision != snapshot.Revision {
		return errApply
	}
	return nil
}
func RequestAdmission(ctx context.Context, dir, id string) (time.Duration, error) {
	if id == "" || len(id) > 128 {
		return 0, errApply
	}
	var reply admissionResponse
	if request(ctx, dir, "vless-admission.sock", 100*time.Millisecond, admissionRequest{ID: id}, &reply) != nil || reply.Milliseconds <= 0 || reply.Milliseconds > 2000 {
		return 0, errApply
	}
	return time.Duration(reply.Milliseconds) * time.Millisecond, nil
}
func serve(ctx context.Context, dir, name string, timeout time.Duration, handle func(context.Context, net.Conn)) (func(), error) {
	listener, cleanup, e := privateListener(dir, name)
	if e != nil {
		return nil, errApply
	}
	child, cancel := context.WithCancel(ctx)
	var once sync.Once
	var mu sync.Mutex
	var pending net.Conn
	finished := make(chan struct{})
	stop := func() {
		once.Do(func() {
			cancel()
			listener.Close()
			mu.Lock()
			if pending != nil {
				pending.Close()
			}
			mu.Unlock()
		})
	}
	go func() { <-child.Done(); stop() }()
	go func() {
		defer close(finished)
		defer cleanup()
		defer stop()
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			if child.Err() != nil {
				mu.Unlock()
				c.Close()
				return
			}
			pending = c
			mu.Unlock()
			req, done := context.WithTimeout(child, timeout)
			c.SetDeadline(time.Now().Add(timeout))
			handle(req, c)
			done()
			c.Close()
			mu.Lock()
			pending = nil
			mu.Unlock()
		}
	}()
	return func() { stop(); <-finished }, nil
}
func ServeControl(ctx context.Context, w *Worker) (func(), error) {
	return serve(ctx, w.dir, "vless-control.sock", 8*time.Second, func(ctx context.Context, c net.Conn) {
		var s Snapshot
		reply := controlResponse{}
		if readMessage(c, &s) == nil && w.Apply(ctx, s) == nil {
			reply = controlResponse{Revision: s.Revision, Applied: true}
		}
		writeMessage(c, reply)
	})
}
func ServeAdmission(ctx context.Context, dir string, renew AdmissionFunc) (func(), error) {
	return serve(ctx, dir, "vless-admission.sock", 100*time.Millisecond, func(ctx context.Context, c net.Conn) {
		var req admissionRequest
		reply := admissionResponse{}
		if readMessage(c, &req) == nil && req.ID != "" && len(req.ID) <= 128 {
			if ttl, e := renew(ctx, req.ID); e == nil && ttl > 0 && ttl <= 2*time.Second {
				reply.Milliseconds = ttl.Milliseconds()
			}
		}
		writeMessage(c, reply)
	})
}
