package mobile

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestExitIPValidationAndNoDirectFallback(t *testing.T) {
	for _, value := range []string{"", "127.0.0.1", "192.168.1.1", "169.254.1.1", "::1", "2001:db8::1", "<html>failure</html>", "8.8.8.8\npassword", "224.0.0.1"} {
		if _, e := parseExitIP(value); e == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if ip, e := parseExitIP("8.8.8.8\n"); e != nil || ip != "8.8.8.8" {
		t.Fatal(ip, e)
	}
	called := false
	_, e := fetchExitIP(context.Background(), func(context.Context) (net.Conn, error) { called = true; return nil, errors.New("tunnel unavailable") })
	if e == nil || !called {
		t.Fatal("must use tunnel dialer and fail without fallback")
	}
	g := NewGateway(nil)
	g.CheckExitIP()
	g.Stop()
}
