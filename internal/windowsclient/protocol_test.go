package windowsclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"quiclab/internal/vpnclient"
	"quiclab/internal/vpnmodel"
	"strings"
	"testing"
)

func TestFrameLimitsAndStrictRequest(t *testing.T) {
	for _, raw := range []string{`{"version":2,"id":"a","command":"status"}`, `{"version":1,"id":"a","command":"shell"}`, `{"version":1,"id":"a","command":"status","secret":"private"}`, `{"version":1,"id":"","command":"status"}`} {
		var b bytes.Buffer
		binary.Write(&b, binary.LittleEndian, uint32(len(raw)))
		b.WriteString(raw)
		if _, e := ReadRequest(&b); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(MaxFrame+1))
	if _, e := ReadRequest(&b); e == nil {
		t.Fatal("oversize accepted")
	}
	if e := WriteFrame(&b, map[string]string{"data": strings.Repeat("x", MaxFrame)}); e == nil {
		t.Fatal("oversize output")
	}
	b.Reset()
	r := Request{Version: 1, ID: "a", Command: "status"}
	if e := WriteFrame(&b, r); e != nil {
		t.Fatal(e)
	}
	if got, e := ReadRequest(&b); e != nil || got.ID != "a" {
		t.Fatal(got, e)
	}
}

type memoryStore struct {
	c    *vpnclient.Config
	fail bool
}

func (s *memoryStore) Load(context.Context) (*vpnclient.Config, error) { return s.c, nil }
func (s *memoryStore) Save(_ context.Context, c vpnclient.Config) error {
	if s.fail {
		return errors.New("secret")
	}
	s.c = &c
	return nil
}

type testBackend struct{ fail bool }

func (b *testBackend) Start(context.Context, vpnclient.Config) error {
	if b.fail {
		return errors.New("PRIVATE KEY")
	}
	return nil
}
func (*testBackend) Stop(context.Context) error { return nil }
func config() vpnclient.Config {
	return vpnclient.Config{Revision: 1, Profiles: vpnmodel.Config{Version: 1, Exits: []vpnmodel.Exit{{ID: "e", Name: "E", Kind: "standalone"}}, Profiles: []vpnmodel.Profile{{ID: "p", ExitID: "e", Transport: "quic", Mode: "auto", Endpoint: "example.org:443", PoolSize: 1}}}}
}
func TestCommandsPersistenceAndRedaction(t *testing.T) {
	store := &memoryStore{}
	backend := &testBackend{}
	h := NewHandler(vpnclient.NewController(backend), store)
	ctx := context.Background()
	call := func(cmd string, c *vpnclient.Config) Response {
		return h.Handle(ctx, Request{Version: 1, ID: "a", Command: cmd, Config: c})
	}
	if call("start", nil).OK {
		t.Fatal("start without config")
	}
	c := config()
	if !call("validate_config", &c).OK || store.c != nil {
		t.Fatal("validation persisted")
	}
	store.fail = true
	if call("apply_config", &c).OK {
		t.Fatal("failed persistence accepted")
	}
	store.fail = false
	if !call("apply_config", &c).OK || !call("start", nil).OK {
		t.Fatal("cannot start")
	}
	if call("apply_config", &c).OK {
		t.Fatal("changed running config")
	}
	if !call("stop", nil).OK {
		t.Fatal("cannot stop")
	}
	backend.fail = true
	r := call("start", nil)
	raw, _ := json.Marshal(r)
	if r.OK || strings.Contains(string(raw), "PRIVATE") {
		t.Fatal(string(raw))
	}
	if call("unknown", nil).OK {
		t.Fatal("unknown command")
	}
}
func TestTruncatedFrame(t *testing.T) {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(20))
	b.WriteString("{}")
	if _, e := ReadRequest(&b); e == nil {
		t.Fatal("truncated accepted")
	}
}

func TestSharedProtocolFixture(t *testing.T) {
	raw, e := os.ReadFile("../../desktop/windows/protocol-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(len(raw)))
	b.Write(raw)
	q, e := ReadRequest(&b)
	if e != nil || q.Command != "apply_config" || q.Config == nil {
		t.Fatal(q, e)
	}
	if vpnmodel.Validate(q.Config.Profiles) != nil {
		t.Fatal("invalid fixture")
	}
}
func TestBackendRejectsConfigurationBeforeSaving(t *testing.T) {
	store := &memoryStore{}
	h := NewHandler(vpnclient.NewController(&rejectingBackend{}), store)
	c := config()
	r := h.Handle(context.Background(), Request{Version: 1, ID: "rejected", Command: "apply_config", Config: &c})
	if r.OK || r.Error != "invalid_config" || store.c != nil {
		t.Fatal("invalid backend config persisted", r)
	}
}

type rejectingBackend struct{ testBackend }

func (*rejectingBackend) ValidateConfig(vpnclient.Config) error {
	return errors.New("identity mismatch")
}

func TestProvisionedConfigOverridesOldCache(t *testing.T) {
	old := config()
	current := config()
	current.Profiles.Profiles[0].Endpoint = "192.0.2.1:443"
	saved := &memoryStore{c: &old}
	store := serviceConfigStore(&current, saved)
	got, err := store.Load(context.Background())
	if err != nil || got.Profiles.Profiles[0].Endpoint != current.Profiles.Profiles[0].Endpoint {
		t.Fatal("stale cache overrides engine config")
	}
	if store.Save(context.Background(), old) == nil {
		t.Fatal("accepted incompatible config")
	}
	got.Profiles.Profiles[0].Endpoint = "changed"
	again, _ := store.Load(context.Background())
	if again.Profiles.Profiles[0].Endpoint != "192.0.2.1:443" {
		t.Fatal("mutable fixed config")
	}
	if saved.c.Profiles.Profiles[0].Endpoint != old.Profiles.Profiles[0].Endpoint {
		t.Fatal("fixture cache changed")
	}
	if serviceConfigStore(nil, saved) != saved {
		t.Fatal("fixture store replaced")
	}
}
