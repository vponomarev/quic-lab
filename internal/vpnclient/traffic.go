package vpnclient

import (
	"encoding/json"
	"sync"
	"time"
)

type Traffic struct {
	Available  bool      `json:"available"`
	Session    string    `json:"session"`
	ObservedAt time.Time `json:"observed_at"`
	TxBytes    uint64    `json:"tx_bytes"`
	RxBytes    uint64    `json:"rx_bytes"`
	TxRate     float64   `json:"tx_bytes_per_second"`
	RxRate     float64   `json:"rx_bytes_per_second"`
}
type RuntimeInfo struct {
	Mode       string   `json:"mode"`
	Transport  string   `json:"transport,omitempty"`
	Endpoint   string   `json:"endpoint,omitempty"`
	RouteCount int      `json:"route_count,omitempty"`
	Targets    []string `json:"targets,omitempty"`
	Traffic    Traffic  `json:"traffic"`
}
type RuntimeProvider interface{ RuntimeInfo() RuntimeInfo }

// TrafficMeter accepts only the aggregate packet engine counters, never flow destinations or credentials.
type TrafficMeter struct {
	mu sync.Mutex
	s  Traffic
}

func (m *TrafficMeter) Reset(session string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.s = Traffic{Session: session}
}
func (m *TrafficMeter) Snapshot() Traffic { m.mu.Lock(); defer m.mu.Unlock(); return m.s }
func (m *TrafficMeter) Observe(raw string, at time.Time) {
	var e struct {
		Event string  `json:"event"`
		Tx    *uint64 `json:"tx_bytes"`
		Rx    *uint64 `json:"rx_bytes"`
	}
	if len(raw) > 65536 || json.Unmarshal([]byte(raw), &e) != nil || e.Event != "traffic" || e.Tx == nil || e.Rx == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.s.Available && !at.After(m.s.ObservedAt) {
		return
	}
	tx, rx := 0.0, 0.0
	if m.s.Available && *e.Tx >= m.s.TxBytes && *e.Rx >= m.s.RxBytes {
		dt := at.Sub(m.s.ObservedAt).Seconds()
		tx = float64(*e.Tx-m.s.TxBytes) / dt
		rx = float64(*e.Rx-m.s.RxBytes) / dt
	}
	m.s = Traffic{Available: true, Session: m.s.Session, ObservedAt: at, TxBytes: *e.Tx, RxBytes: *e.Rx, TxRate: tx, RxRate: rx}
}
