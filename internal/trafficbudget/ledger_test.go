package trafficbudget

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentReservation(t *testing.T) {
	l := New("run", 100)
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := l.Reserve(80, User); ok {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	if won.Load() != 1 || l.Snapshot().Reserved != 80 {
		t.Fatal(won.Load(), l.Snapshot())
	}
}
func TestPartialCommitAndReceive(t *testing.T) {
	l := New("run", 100)
	id, ok := l.Reserve(80, User)
	if !ok {
		t.Fatal("reserve")
	}
	l.Commit(id, 30)
	l.Commit(id, 30)
	if s := l.Snapshot(); s.Used != 30 || s.Reserved != 0 {
		t.Fatal(s)
	}
	id, ok = l.Reserve(70, Control)
	if !ok {
		t.Fatal("unused reservation not released")
	}
	l.Commit(id, 0)
	l.ObserveReceived(75, Copy)
	if s := l.Snapshot(); s.Used != 105 || !s.Blocked || s.ByClass[User] != 30 || s.ByClass[Copy] != 75 {
		t.Fatal(s)
	}
	if _, ok = l.Reserve(1, Config); ok {
		t.Fatal("budget bypass")
	}
}
func TestNoOverflowOrDoubleCommit(t *testing.T) {
	l := New("run", math.MaxUint64)
	l.ObserveReceived(math.MaxUint64-3, User)
	l.ObserveReceived(10, User)
	if s := l.Snapshot(); s.Used != math.MaxUint64 || !s.Blocked {
		t.Fatal(s)
	}
	if _, ok := l.Reserve(1, APK); ok {
		t.Fatal("overflow unblocked")
	}
	u := New("unlimited", 0)
	a, _ := u.Reserve(math.MaxUint64, User)
	if _, ok := u.Reserve(1, User); ok {
		t.Fatal("reservation overflow")
	}
	u.Commit(a, math.MaxUint64)
	u.Commit(a, 1)
	u.ObserveReceived(1, Control)
	if s := u.Snapshot(); s.Used != math.MaxUint64 || s.Reserved != 0 || s.Blocked {
		t.Fatal(s)
	}
	if _, ok := u.Reserve(1, User); !ok {
		t.Fatal("unlimited stopped")
	}
	// Account actual bytes even when a caller reports more than reserved.
	x := New("overrun", 10)
	id, _ := x.Reserve(5, User)
	x.Commit(id, 12)
	if !x.Snapshot().Blocked {
		t.Fatal("overrun hidden")
	}
}
func TestSharedEpoch(t *testing.T) {
	l := New("first", 100)
	exitA, exitB := l, l
	id, _ := exitA.Reserve(20, User)
	exitA.Commit(id, 20)
	if s := exitB.Snapshot(); s.Epoch != "first" || s.Used != 20 {
		t.Fatal(s)
	}
	fresh := New("second", 100)
	if s := fresh.Snapshot(); s.Epoch == l.Snapshot().Epoch || s.Used != 0 {
		t.Fatal(s)
	}
	s := l.Snapshot()
	s.ByClass[User] = 999
	if l.Snapshot().ByClass[User] != 20 {
		t.Fatal("snapshot aliases state")
	}
	if _, ok := l.Reserve(1, Class("invalid")); ok {
		t.Fatal("invalid class")
	}
}
