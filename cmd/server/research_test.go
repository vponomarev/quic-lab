package main

import (
	"context"
	"errors"
	"quiclab/internal/research"
	"sync"
	"testing"
)

func TestResearchStoreLazyConfiguration(t *testing.T) {
	var mu sync.Mutex
	quota := int64(0)
	dir := t.TempDir()
	m := newResearchStoreManager(func() (research.StoreOptions, error) {
		mu.Lock()
		defer mu.Unlock()
		if quota == 0 {
			return research.StoreOptions{}, errors.New("configure quota")
		}
		return research.StoreOptions{Dir: dir, MaxBytes: quota}, nil
	})
	if _, e := m.Open(); e == nil {
		t.Fatal("unconfigured store opened")
	}
	mu.Lock()
	quota = 1 << 20
	mu.Unlock()
	var wg sync.WaitGroup
	stores := make(chan *research.Store, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := m.Open()
			if e != nil {
				t.Error(e)
				return
			}
			stores <- s
		}()
	}
	wg.Wait()
	close(stores)
	var first *research.Store
	for s := range stores {
		if first == nil {
			first = s
		}
		if first != s {
			t.Fatal("more than one store")
		}
	}
	if _, e := first.Query(context.Background(), research.Query{}); e != nil {
		t.Fatal(e)
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Open(); e == nil {
		t.Fatal("reopened after shutdown")
	}
}
