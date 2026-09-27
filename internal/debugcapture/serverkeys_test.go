package debugcapture

import (
	"context"
	"testing"
	"time"
)

func TestServerKeysOnlyActiveMatchingListeners(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := func(port, tcp int) *Session {
		return &Session{Info: Info{State: "running", Port: port, TCPPort: tcp, Until: time.Now().Add(time.Minute)}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	}
	a, b := session(4433, 443), session(4434, 8443)
	m := &Manager{sessions: map[string]*Session{"echo": a, "vpn": b}}
	r := new(Router)
	w := r.Writer(4433, true)
	key := []byte("synthetic key line\n")
	w.Write(key)
	if len(a.Keys()) != 0 {
		t.Fatal("retained before activation")
	}
	r.Set(m)
	w.Write(key)
	if len(a.Keys()) != len(key) || len(b.Keys()) != 0 {
		t.Fatal("listener selection")
	}
	r.Writer(8443, false).Write(key)
	if len(b.Keys()) != len(key) {
		t.Fatal("HTTPS listener")
	}
	a.Stop("done")
	w.Write(key)
	if len(a.Keys()) != len(key) {
		t.Fatal("retained after stop")
	}
}
