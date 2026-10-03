package vlessserver

import (
	"context"
	"net"
	"testing"
)

type changingResolver struct {
	calls     int
	addresses []net.IPAddr
}

func (r *changingResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	r.calls++
	return r.addresses, nil
}
func TestRuntimePinsDemuxDestination(t *testing.T) {
	c := realityConfig()
	c.Mode = "demux-only"
	c.DemuxEndpoint = "demux.example:2443"
	r := &changingResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.2")}}}
	o, e := c.serverOptions(context.Background(), nil, r)
	if e != nil {
		t.Fatal(e)
	}
	r.addresses = []net.IPAddr{{IP: net.ParseIP("127.0.0.3")}}
	if o.DemuxEndpoint != "127.0.0.2:2443" || r.calls != 1 {
		t.Fatal("destination not pinned")
	}
	next, e := c.serverOptions(context.Background(), nil, r)
	if e != nil || next.DemuxEndpoint != "127.0.0.3:2443" {
		t.Fatal("new revision does not resolve afresh")
	}
}
func TestRuntimeRejectsNonIPv4Demux(t *testing.T) {
	c := realityConfig()
	c.Mode = "demux-only"
	c.DemuxEndpoint = "demux.example:2443"
	for _, ip := range []string{"::1", "0.0.0.0", "224.0.0.1"} {
		_, e := c.serverOptions(context.Background(), nil, &changingResolver{addresses: []net.IPAddr{{IP: net.ParseIP(ip)}}})
		if e == nil {
			t.Fatal("invalid resolved destination")
		}
	}
}
func TestRuntimeDoesNotExposeFileError(t *testing.T) {
	c := tlsConfig()
	c.TLSKeyFile = "/secret-nonexistent.key"
	if _, e := c.serverOptions(context.Background(), nil, net.DefaultResolver); e != errConfig {
		t.Fatalf("error must be sanitized: %v", e)
	}
}
