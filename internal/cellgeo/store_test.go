package cellgeo

import (
	"path/filepath"
	"quiclab/internal/backup"
	"runtime"
	"testing"
	"time"
)

func TestPersistenceSourcesAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cells.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	c := Cell{Technology: "LTE", MCC: "250", MNC: "099", TAC: "001", CI: "123"}
	if e = s.Enqueue(c, "mylnikov", 100, false); e != nil {
		t.Fatal(e)
	}
	j, e := s.Next(time.Now())
	if e != nil || j == nil {
		t.Fatal(j, e)
	}
	if e = s.Complete(*j, &Position{Lat: 55, Lon: 37, Range: 1000}, nil, time.Now()); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c.MNC = "99"
	c.TAC = "1"
	p, e := s.Lookup(c)
	if e != nil || p == nil || p.Lat != 55 || p.Source != "mylnikov" {
		t.Fatal(p, e)
	}
	if e = s.AddSource(Source{ID: "better", Name: "Better", Version: "1", Priority: 200, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	s.Enqueue(c, "better", 1, false)
	j, _ = s.Next(time.Now())
	s.Complete(*j, &Position{Lat: 56, Lon: 38, Range: 2000}, nil, time.Now())
	p, _ = s.Lookup(c)
	if p.Source != "better" {
		t.Fatal("priority must precede claimed radius", p)
	}
	s.Enqueue(c, "better", 1, true)
	j, _ = s.Next(time.Now())
	s.Complete(*j, nil, errProvider, time.Now())
	p, _ = s.Lookup(c)
	if p.Lat != 56 {
		t.Fatal("error destroyed position")
	}
	stats, _ := s.Stats()
	if stats.History != 3 {
		t.Fatal(stats)
	}
}
func TestPriorityNegativeAndRetry(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "cells.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := Cell{Technology: "LTE", MCC: "250", MNC: "99", TAC: "1", CI: "1"}
	b := a
	b.CI = "2"
	s.Enqueue(a, "mylnikov", 1, false)
	s.Enqueue(b, "mylnikov", 100, false)
	now := time.Now()
	j, _ := s.Next(now)
	if j.Cell.CI != "2" {
		t.Fatal("priority", j)
	}
	s.Complete(*j, nil, nil, now)
	s.Enqueue(b, "mylnikov", 100, false)
	j, _ = s.Next(now)
	if j.Cell.CI != "1" {
		t.Fatal("negative cache requeued", j)
	}
	s.Complete(*j, nil, errProvider, now)
	j, _ = s.Next(now)
	if j != nil {
		t.Fatal("retry without backoff")
	}
	j, _ = s.Next(now.Add(time.Hour))
	if j == nil {
		t.Fatal("lost retry")
	}
}

func TestInterruptedLeaseAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cells.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	c := Cell{Technology: "LTE", MCC: "250", MNC: "99", TAC: "1", CI: "3"}
	s.Enqueue(c, "mylnikov", 1, false)
	now := time.Now()
	j, _ := s.Next(now)
	if j == nil {
		t.Fatal("no job")
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if early, _ := s.Next(now); early != nil {
		t.Fatal("lease duplicated")
	}
	j, _ = s.Next(now.Add(2 * time.Minute))
	if j == nil {
		t.Fatal("lost interrupted work")
	}
	s.Complete(*j, &Position{Lat: 55, Lon: 37, Range: 200}, nil, now)
	s.Enqueue(c, "mylnikov", 1, true)
	j, _ = s.Next(now)
	s.Complete(*j, &Position{Lat: 56, Lon: 38, Range: 300}, nil, now.Add(time.Hour))
	h, e := s.History(c)
	if e != nil || len(h) != 2 || h[0].Position.Lat != 56 || h[1].Position.Lat != 55 {
		t.Fatal(h, e)
	}
	p, _ := s.Lookup(c)
	if p.Lat != 56 {
		t.Fatal("selected old allegedly more precise result", p)
	}
}

func TestIdleQueueDoesNotInvalidateBackups(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("publication coordinator is Linux-only")
	}
	root := t.TempDir()
	if e := backup.EnablePublication(root); e != nil {
		t.Fatal(e)
	}
	s, e := Open(filepath.Join(root, "cells.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	before, _ := backup.PublicationEpoch(root)
	j, e := s.Next(time.Now())
	if e != nil || j != nil {
		t.Fatal(j, e)
	}
	after, _ := backup.PublicationEpoch(root)
	if before != after {
		t.Fatal("idle queue changed backup epoch")
	}
	c := Cell{Technology: "LTE", MCC: "250", MNC: "99", TAC: "1", CI: "1"}
	s.Enqueue(c, "mylnikov", 10, false)
	before, _ = backup.PublicationEpoch(root)
	s.Enqueue(c, "mylnikov", 0, false)
	after, _ = backup.PublicationEpoch(root)
	if before != after {
		t.Fatal("read-only map refresh changed backup epoch")
	}
}
