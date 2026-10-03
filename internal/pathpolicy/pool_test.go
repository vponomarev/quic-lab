package pathpolicy

import "testing"

func TestPoolBoundAcrossNetworks(t *testing.T) {
	p, err := NewPool(3)
	if err != nil {
		t.Fatal(err)
	}
	first, err := p.Reserve("profile", "wifi")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Reserve("profile", "wifi"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Reserve("profile", "cell"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Reserve("profile", "cell"); err == nil {
		t.Fatal("per-profile pool exceeded across networks")
	}
	if _, err = p.Reserve("other", "cell"); err != nil {
		t.Fatal("unrelated profile lost its pool", err)
	}
	p.Release(first)
	p.Release(first)
	if _, err = p.Reserve("profile", "cell"); err != nil {
		t.Fatal("released slot not reusable", err)
	}
	if _, err = p.Reserve("profile", "wifi"); err == nil {
		t.Fatal("double release enlarged pool")
	}
}

func TestPoolRejectsInvalidReservation(t *testing.T) {
	for _, n := range []int{0, 6, -1} {
		if _, err := NewPool(n); err == nil {
			t.Fatalf("accepted limit %d", n)
		}
	}
	p, _ := NewPool(2)
	for _, v := range [][2]string{{"", "wifi"}, {"a", "unknown"}} {
		if _, err := p.Reserve(v[0], v[1]); err == nil {
			t.Fatal("invalid reservation accepted")
		}
	}
}

func TestPoolStaleReleaseCannotFreeNewReservation(t *testing.T) {
	p, _ := NewPool(1)
	old, _ := p.Reserve("p", "wifi")
	p.Release(old)
	current, err := p.Reserve("p", "cell")
	if err != nil {
		t.Fatal(err)
	}
	p.Release(old)
	if _, err = p.Reserve("p", "wifi"); err == nil {
		t.Fatal("stale cancellation released replacement slot")
	}
	p.Release(current)
}
