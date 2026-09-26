package echoawg

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExpiryAndAdmission(t *testing.T) {
	p, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ep := p.LocalAddr().String()
	p.Close()
	s, e := Start(context.Background(), ep, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	request := func() int {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
		return w.Code
	}
	if request() != 200 {
		t.Fatal("enroll")
	}
	s.mu.Lock()
	s.prune(time.Now().Add(Lifetime + time.Second))
	count := len(s.leases)
	s.mu.Unlock()
	if count != 0 {
		t.Fatal("expired lease retained")
	}
	for i := 1; i < 32; i++ {
		if request() != 200 {
			t.Fatal("admit", i)
		}
	}
	if request() != 429 {
		t.Fatal("rate limit missing")
	}
}
