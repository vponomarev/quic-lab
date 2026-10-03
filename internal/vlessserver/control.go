package vlessserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"sync"
	"time"
)

const maxControlMessage = 128 * 1024

type ControlClient struct{ Dir string }
type controlRequest struct {
	Snapshot
	Status bool
}
type controlResponse struct {
	Revision uint64 `json:"revision"`
	Applied  bool   `json:"applied"`
}
type admissionRequest struct {
	Source string `json:"source,omitempty"`
	UUID   string `json:"uuid"`
	ID     string `json:"id"`
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

type admissionSourceKey struct{}

func AdmissionSource(ctx context.Context) string {
	source, _ := ctx.Value(admissionSourceKey{}).(string)
	return source
}
func validAdmissionSource(source string) bool {
	if source == "" {
		return true
	}
	addr, err := netip.ParseAddrPort(source)
	return err == nil && addr.Port() != 0 && addr.Addr().Zone() == "" && !addr.Addr().IsUnspecified() && !addr.Addr().IsMulticast()
}
func RequestAdmission(ctx context.Context, dir, id, uuid string, sources ...string) (time.Duration, error) {
	source := ""
	if len(sources) > 1 {
		return 0, errApply
	}
	if len(sources) == 1 {
		source = sources[0]
	}
	if !validAdmissionSource(source) {
		return 0, errApply
	}
	if id == "" || len(id) > 128 || len(uuid) != 36 {
		return 0, errApply
	}
	var reply admissionResponse
	if request(ctx, dir, "vless-admission.sock", 100*time.Millisecond, admissionRequest{ID: id, UUID: uuid, Source: source}, &reply) != nil || reply.Milliseconds <= 0 || reply.Milliseconds > 2000 {
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
		var s controlRequest
		reply := controlResponse{}
		if readMessage(c, &s) == nil {
			if s.Status {
				w.mu.Lock()
				reply = controlResponse{Revision: w.record.Desired.Revision, Applied: w.live != nil && !w.closed && !w.writeFailed && w.applied == w.record.Desired.Revision}
				w.mu.Unlock()
			} else if w.Apply(ctx, s.Snapshot) == nil {
				reply = controlResponse{Revision: s.Revision, Applied: true}
			}
		}
		writeMessage(c, reply)
	})
}
func ServeAdmission(ctx context.Context, dir string, renew AdmissionFunc) (func(), error) {
	return serve(ctx, dir, "vless-admission.sock", 100*time.Millisecond, func(ctx context.Context, c net.Conn) {
		var req admissionRequest
		reply := admissionResponse{}
		if readMessage(c, &req) == nil && req.ID != "" && len(req.ID) <= 128 && len(req.UUID) == 36 && validAdmissionSource(req.Source) {
			if ttl, e := renew(context.WithValue(ctx, admissionSourceKey{}, req.Source), req.ID, req.UUID); e == nil && ttl > 0 && ttl <= 2*time.Second {
				reply.Milliseconds = ttl.Milliseconds()
			}
		}
		writeMessage(c, reply)
	})
}

func (c ControlClient) CheckApplied(ctx context.Context, revision uint64) error {
	var reply controlResponse
	if revision == 0 || request(ctx, c.Dir, "vless-control.sock", time.Second, controlRequest{Status: true}, &reply) != nil || !reply.Applied || reply.Revision != revision {
		return errApply
	}
	return nil
}
