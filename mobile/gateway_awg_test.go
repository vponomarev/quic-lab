package mobile

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// Opt-in only: no credentials or test deployment settings in the repository.
func TestAWGLive(t *testing.T) {
	path := os.Getenv("QUIC_LAB_AWG_TEST_CONFIG")
	if path == "" {
		t.Skip("set QUIC_LAB_AWG_TEST_CONFIG for an authorized test peer")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	meta, e := ValidateAWGConfig(string(raw))
	if e != nil {
		t.Fatal(e)
	}
	var m struct{ Endpoint, DNS string }
	if e = json.Unmarshal([]byte(meta), &m); e != nil {
		t.Fatal(e)
	}
	h, p, _ := net.SplitHostPort(m.Endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	ips, e := net.DefaultResolver.LookupIP(ctx, "ip4", h)
	if e != nil || len(ips) == 0 {
		t.Fatal("endpoint lookup failed")
	}
	cfg, _ := json.Marshal(gatewayConfig{Transport: "awg", Endpoint: net.JoinHostPort(ips[0].String(), p), AWGConfig: string(raw)})
	g := NewGateway(nil)
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	defer g.Stop()
	d := g.datagramBackend()
	ping, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	e = awgPingProbe(ping, d, net.JoinHostPort(ips[0].String(), p), 100)
	cancelPing()
	if e != nil {
		t.Fatal("endpoint ping through AWG:", e)
	}
	for i := 0; i < 3; i++ {
		probe, c := context.WithTimeout(ctx, 8*time.Second)
		e := awgDNSProbe(probe, d, net.JoinHostPort(m.DNS, "53"), uint16(i+1))
		c()
		if e != nil {
			t.Fatal(e)
		}
		_, e = fetchExitIP(ctx, func(ctx context.Context) (net.Conn, error) {
			s, e := g.dialStream(ctx, "exit-ip", "")
			if e != nil {
				return nil, e
			}
			return diagnosticConn{s}, nil
		})
		if e != nil {
			t.Fatal(e)
		}
		if e = g.MigrateTo("test-rebind", nil); e != nil {
			t.Fatal(e)
		}
	}
	t.Log("DNS and HTTPS through AWG passed across two UDP socket rebinds")
}

func awgDNSProbe(ctx context.Context, d flowDialer, target string, seq uint16) error {
	c, e := d.DialContext(ctx, "udp4", target)
	if e != nil {
		return e
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	c.SetDeadline(deadline)
	// Root NS query: small, cacheable, independent of the exit-IP provider.
	q := []byte{0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 1}
	binary.BigEndian.PutUint16(q, seq)
	if _, e = c.Write(q); e != nil {
		return e
	}
	b := make([]byte, 4096)
	n, e := c.Read(b)
	if e != nil {
		return e
	}
	if n < 12 || binary.BigEndian.Uint16(b) != seq || b[2]&0x80 == 0 {
		return errors.New("invalid DNS probe response")
	}
	return nil
}
