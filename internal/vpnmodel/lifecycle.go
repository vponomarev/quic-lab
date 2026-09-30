package vpnmodel

import "sync"

type State string

const (
	Stopped      State = "stopped"
	Connecting   State = "connecting"
	Active       State = "active"
	Recovering   State = "recovering"
	Blocked      State = "blocked"
	Incompatible State = "incompatible"
)

type Event struct {
	ExitID     string
	Generation uint64
	Kind       string
}
type exitRuntime struct {
	generation uint64
	state      State
	live       bool
}
type Runtime struct {
	mu     sync.Mutex
	serial uint64
	exits  map[string]exitRuntime
}

func NewRuntime(c Config) *Runtime {
	r := &Runtime{exits: make(map[string]exitRuntime)}
	for _, e := range c.Exits {
		r.exits[e.ID] = exitRuntime{state: Stopped}
	}
	return r
}
func (r *Runtime) Start(id string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.exits[id]; !ok {
		return 0
	}
	r.serial++
	r.exits[id] = exitRuntime{r.serial, Connecting, true}
	return r.serial
}
func (r *Runtime) Apply(e Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	x, ok := r.exits[e.ExitID]
	if !ok || !x.live || x.generation != e.Generation {
		return false
	}
	switch State(e.Kind) {
	case Active, Recovering, Blocked, Incompatible:
	default:
		return false
	}
	x.state = State(e.Kind)
	if x.state == Incompatible {
		x.live = false
	}
	r.exits[e.ExitID] = x
	return true
}
func (r *Runtime) Stop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if x, ok := r.exits[id]; ok {
		x.live = false
		x.state = Stopped
		r.exits[id] = x
	}
}
func (r *Runtime) State(id string) State {
	r.mu.Lock()
	defer r.mu.Unlock()
	if x, ok := r.exits[id]; ok {
		return x.state
	}
	return Stopped
}
