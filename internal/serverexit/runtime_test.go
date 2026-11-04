package serverexit

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
)

type fakeProvider struct{ closed atomic.Int32 }

func (*fakeProvider) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("offline")
}
func (*fakeProvider) Resolver() *net.Resolver     { return &net.Resolver{} }
func (*fakeProvider) Probe(context.Context) error { return nil }
func (*fakeProvider) ProbeICMP(context.Context, []byte) ([]byte, error) {
	return nil, errors.New("offline")
}
func (p *fakeProvider) Close() { p.closed.Add(1) }

func TestRuntimeOwnershipAndRollback(t *testing.T) {
	c, _ := NewCatalog(testDefinitions())
	first := &fakeProvider{}
	_, err := startRuntime(c, func(d Definition) (Provider, error) {
		if d.ID == "east" {
			return nil, errors.New("failure")
		}
		return first, nil
	})
	if err == nil || first.closed.Load() != 1 {
		t.Fatal("partial startup leaked provider", err)
	}
	p := map[string]*fakeProvider{"west": {}, "east": {}}
	r, err := startRuntime(c, func(d Definition) (Provider, error) { return p[d.ID], nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"west", "east"} {
		got, err := r.Provider(id)
		if err != nil || got != p[id] {
			t.Fatal(id, err)
		}
	}
	for _, id := range []string{"", "direct", "missing"} {
		if _, err := r.Provider(id); err == nil {
			t.Fatal("implicit fallback", id)
		}
	}
	r.Close()
	r.Close()
	if p["west"].closed.Load() != 1 || p["east"].closed.Load() != 1 {
		t.Fatal("provider ownership lost")
	}
	if _, err := r.Provider("west"); err == nil {
		t.Fatal("closed runtime admitted provider")
	}
}

func TestDisabledProviderIsNotStarted(t *testing.T) {
	defs := testDefinitions()
	defs[1].Disabled = true
	c, _ := NewCatalog(defs)
	calls := 0
	r, err := startRuntime(c, func(d Definition) (Provider, error) { calls++; return &fakeProvider{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if calls != 1 {
		t.Fatal(calls)
	}
	if _, err := r.Provider("east"); err == nil {
		t.Fatal("disabled provider")
	}
}
