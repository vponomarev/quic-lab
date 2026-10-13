// Package research stores opt-in observations of logical tunnel traffic.
package research

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"quiclab/internal/netobserve"
	"strings"
	"sync"
	"time"
)

const MaxRecordBytes = 64 << 10
const SchemaVersion = 1

type FlowKey struct {
	DeviceID  string `json:"device_id"`
	SessionID string `json:"session_id"`
	FlowID    uint64 `json:"flow_id,string"`
}
type App struct {
	ID          uint64   `json:"id,string"`
	UID         int64    `json:"uid,string"`
	Packages    []string `json:"packages"`
	Labels      []string `json:"labels"`
	Attribution string   `json:"attribution"`
}
type Record struct {
	Origin        string          `json:"origin,omitempty"`
	ReceivedAt    string          `json:"received_at,omitempty"`
	SchemaVersion uint32          `json:"schema_version"`
	Type          string          `json:"type"`
	ID            string          `json:"id"`
	At            time.Time       `json:"at"`
	Generation    uint64          `json:"generation,string"`
	Flow          FlowKey         `json:"flow"`
	Data          json.RawMessage `json:"data"`
}

// ExitRef describes the client's selected egress, independently of observation origin.
// It is metadata, never an authorization or proof of server identity.
type ExitRef struct {
	Route string `json:"route,omitempty"`
	ID    string `json:"exit_id,omitempty"`
	Type  string `json:"exit_type,omitempty"`
}

func (e ExitRef) Normalized() ExitRef {
	if e.Route == "" {
		e.Route = "unknown"
	}
	if e.Type == "" {
		e.Type = "unknown"
	}
	return e
}
func (e ExitRef) validate() error {
	if len(e.ID) > 256 || strings.ContainsRune(e.ID, 0) {
		return errors.New("invalid research exit ID")
	}
	switch e.Route {
	case "", "unknown", "vpn", "direct":
	default:
		return errors.New("invalid research route")
	}
	switch e.Type {
	case "", "unknown", "quic", "https", "direct":
	default:
		return errors.New("invalid research exit type")
	}
	if e.Route == "direct" && (e.ID != "direct" || e.Type != "direct") {
		return errors.New("invalid direct exit")
	}
	if e.Route != "direct" && (e.ID == "direct" || e.Type == "direct") {
		return errors.New("direct exit requires direct route")
	}
	return nil
}

type FlowData struct {
	TLS      *netobserve.TLSInfo      `json:"tls,omitempty"`
	Detected *netobserve.ProtocolInfo `json:"detected_protocol,omitempty"`
	ExitRef
	AppSource        string     `json:"app_source,omitempty"`
	ClientFlow       *FlowKey   `json:"client_flow,omitempty"`
	UserID           string     `json:"user_id"`
	App              App        `json:"app"`
	Protocol         string     `json:"protocol"`
	Source           string     `json:"source"`
	Destination      string     `json:"destination"`
	Domain           string     `json:"domain,omitempty"`
	ObservedFrom     time.Time  `json:"observed_from"`
	ObservedTo       time.Time  `json:"observed_to"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	BytesUp          uint64     `json:"bytes_up,string"`
	BytesDown        uint64     `json:"bytes_down,string"`
	PacketsUp        uint64     `json:"packets_up,string"`
	PacketsDown      uint64     `json:"packets_down,string"`
	CounterKind      string     `json:"counter_kind"`
	MeasurementLevel string     `json:"measurement_level"`
	Status           string     `json:"status,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	Incomplete       bool       `json:"incomplete"`
}

func validate(r Record) error {
	switch r.Origin {
	case "", "client", "server_gateway", "server_awg", "server_vless":
	default:
		return errors.New("invalid research origin")
	}
	if r.ReceivedAt != "" {
		at, err := time.Parse(time.RFC3339Nano, r.ReceivedAt)
		if len(r.ReceivedAt) > 64 || err != nil || at.IsZero() {
			return errors.New("invalid research receipt time")
		}
	}

	if len(r.Data) > MaxRecordBytes {
		return errors.New("research payload exceeds 64 KiB")
	}
	if r.SchemaVersion != SchemaVersion {
		return errors.New("unsupported research schema")
	}
	switch r.Type {
	case "consent", "app", "flow_open", "flow_delta", "flow_close", "dns", "gap", "capture_manifest":
	default:
		return errors.New("unsupported research record type")
	}
	if r.ID == "" || strings.ContainsRune(r.ID, 0) || strings.ContainsRune(r.Flow.DeviceID, 0) || strings.ContainsRune(r.Flow.SessionID, 0) || len(r.ID) > 256 || r.At.IsZero() || len(r.Flow.DeviceID) > 256 || len(r.Flow.SessionID) > 256 || !json.Valid(r.Data) {
		return errors.New("invalid research envelope")
	}
	if strings.HasPrefix(r.Type, "flow_") || r.Type == "dns" {
		var exit ExitRef
		if err := json.Unmarshal(r.Data, &exit); err != nil {
			return err
		}
		if err := exit.validate(); err != nil {
			return err
		}
	}
	if strings.HasPrefix(r.Type, "flow_") {
		var d FlowData
		if err := json.Unmarshal(r.Data, &d); err != nil {
			return err
		}
		if err := validateApp(d.App); err != nil {
			return err
		}
		if len(d.UserID) > 256 || len(d.Source) > 512 || len(d.Destination) > 512 || len(d.Domain) > 253 || len(d.Protocol) > 16 || len(d.Reason) > 1024 {
			return errors.New("flow strings exceed limits")
		}
		if d.Detected != nil {
			if err := d.Detected.Validate(); err != nil {
				return err
			}
		}
		if d.TLS != nil {
			if err := d.TLS.Validate(); err != nil {
				return err
			}
		}
		if d.CounterKind != "delta" && d.CounterKind != "total" {
			return errors.New("invalid counter kind")
		}
	}
	if r.Type == "app" {
		var a App
		if err := json.Unmarshal(r.Data, &a); err != nil {
			return err
		}
		return validateApp(a)
	}
	return nil
}
func validateApp(a App) error {
	if a.Attribution != "known" && a.Attribution != "unknown" && a.Attribution != "shared_uid" {
		return errors.New("invalid attribution")
	}
	if len(a.Packages) > 32 || len(a.Labels) > 32 {
		return errors.New("too many app names")
	}
	for _, ss := range [][]string{a.Packages, a.Labels} {
		for _, s := range ss {
			if len(s) > 1024 {
				return errors.New("app name exceeds limit")
			}
		}
	}
	return nil
}
func encode(r Record) ([]byte, error) {
	if err := validate(r); err != nil {
		return nil, err
	}
	r.At = r.At.UTC()
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxRecordBytes {
		return nil, errors.New("research record exceeds 64 KiB")
	}
	return append(b, '\n'), nil
}
func EncodeRecord(w io.Writer, r Record) error {
	b, e := encode(r)
	if e != nil {
		return e
	}
	n, e := w.Write(b)
	if e == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return e
}
func DecodeRecord(rd io.Reader) (Record, error) {
	var r Record
	b, e := io.ReadAll(io.LimitReader(rd, MaxRecordBytes+1))
	if e != nil {
		return r, e
	}
	if len(b) > MaxRecordBytes {
		return r, errors.New("research record exceeds 64 KiB")
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if e = validate(r); e != nil {
		return r, fmt.Errorf("research record: %w", e)
	}
	return r, nil
}

// ConsentState belongs to one authenticated device/session. A new session starts denied.
type ConsentState struct {
	mu             sync.RWMutex
	generation     uint64
	known, enabled bool
}

func (c *ConsentState) Apply(g uint64, enabled bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.known && (g < c.generation || (g == c.generation && enabled != c.enabled)) {
		return false
	}
	c.known = true
	c.generation = g
	c.enabled = enabled
	return true
}
func (c *ConsentState) Allows(g uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.known && c.enabled && g == c.generation
}
