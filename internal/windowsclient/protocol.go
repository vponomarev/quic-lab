// Package windowsclient contains the desktop service contract and Windows adapters.
package windowsclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"quiclab/internal/vpnclient"

	"regexp"
	"sync"
)

const MaxFrame = 1 << 20

var ErrProtocol = errors.New("invalid_request")
var requestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Request struct {
	Version int               `json:"version"`
	ID      string            `json:"id"`
	Command string            `json:"command"`
	Config  *vpnclient.Config `json:"config,omitempty"`
}
type Response struct {
	Version int                `json:"version"`
	ID      string             `json:"id"`
	OK      bool               `json:"ok"`
	Error   string             `json:"error,omitempty"`
	Status  vpnclient.Snapshot `json:"status"`
}

func validRequest(r Request) bool {
	if r.Version != 1 || !requestID.MatchString(r.ID) {
		return false
	}
	switch r.Command {
	case "status", "start", "stop":
		return r.Config == nil
	case "validate_config", "apply_config":
		return r.Config != nil
	}
	return false
}
func ReadFrame(r io.Reader) ([]byte, error) {
	var n uint32
	if binary.Read(r, binary.LittleEndian, &n) != nil || n == 0 || n > MaxFrame {
		return nil, ErrProtocol
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(r, b); e != nil {
		return nil, ErrProtocol
	}
	return b, nil
}
func ReadRequest(r io.Reader) (Request, error) {
	b, e := ReadFrame(r)
	if e != nil {
		return Request{}, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var q Request
	if d.Decode(&q) != nil || !validRequest(q) {
		return Request{}, ErrProtocol
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Request{}, ErrProtocol
	}
	return q, nil
}
func WriteFrame(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil || len(b) > MaxFrame {
		return ErrProtocol
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(b)))
	if _, e = io.Copy(w, bytes.NewReader(header[:])); e != nil {
		return e
	}
	_, e = io.Copy(w, bytes.NewReader(b))
	return e
}

type ConfigStore interface {
	Load(context.Context) (*vpnclient.Config, error)
	Save(context.Context, vpnclient.Config) error
}
type Handler struct {
	controller *vpnclient.Controller
	store      ConfigStore
	mu         sync.Mutex
}

func NewHandler(c *vpnclient.Controller, s ConfigStore) *Handler {
	return &Handler{controller: c, store: s}
}
func (h *Handler) Handle(ctx context.Context, q Request) Response {
	r := Response{Version: 1, ID: q.ID}
	if !validRequest(q) {
		r.ID = ""
		r.Error = "invalid_request"
		return r
	}
	var err error
	switch q.Command {
	case "status":
	case "stop":
		err = h.controller.Stop(ctx)
	case "validate_config":
		if h.controller.ValidateConfig(*q.Config) != nil {
			err = vpnclient.ErrInvalidConfig
		}
	case "apply_config", "start":
		h.mu.Lock()
		if q.Command == "apply_config" {
			if h.controller.Snapshot().State != vpnclient.Stopped {
				err = errors.New("stop_before_apply")
			} else if h.controller.ValidateConfig(*q.Config) != nil {
				err = vpnclient.ErrInvalidConfig
			} else if h.store.Save(ctx, *q.Config) != nil {
				err = errors.New("config_store_failed")
			}
		}
		if q.Command == "start" {
			c, e := h.store.Load(ctx)
			if e != nil {
				err = errors.New("config_store_failed")
			} else if c == nil {
				err = errors.New("config_required")
			} else {
				err = h.controller.Start(ctx, *c)
			}
		}
		h.mu.Unlock()
	}
	r.Status = h.controller.Snapshot()
	r.OK = err == nil
	if err != nil {
		switch err.Error() {
		case "stop_before_apply", "config_store_failed", "config_required", "invalid_config", "already_running", "cleanup_required", "stop_pending", "backend_start_failed":
			r.Error = err.Error()
		default:
			r.Error = "operation_failed"
		}
	}
	return r
}
