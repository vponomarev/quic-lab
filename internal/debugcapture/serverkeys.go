package debugcapture

import (
	"io"
	"sync/atomic"
)

// Router is installed before listeners start; no keys are retained without an active capture.
type Router struct{ manager atomic.Pointer[Manager] }

func (r *Router) Set(m *Manager)                      { r.manager.Store(m) }
func (r *Router) Writer(port int, udp bool) io.Writer { return &serverKeys{r, port, udp} }

type serverKeys struct {
	router *Router
	port   int
	udp    bool
}

func (w *serverKeys) Write(b []byte) (int, error) {
	m := w.router.manager.Load()
	if m == nil {
		return len(b), nil
	}
	m.mu.Lock()
	var selected []*Session
	for _, s := range m.sessions {
		v := s.Snapshot()
		if v.User == "" && v.State == "running" && ((w.udp && v.Port == w.port) || (!w.udp && v.TCPPort == w.port)) {
			selected = append(selected, s)
		}
	}
	m.mu.Unlock()
	for _, s := range selected {
		_, _ = s.Write(b)
	}
	return len(b), nil
}
