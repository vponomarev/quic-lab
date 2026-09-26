//go:build linux

package mobile

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"github.com/quic-go/quic-go"
	"golang.org/x/sys/unix"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type multipleOwner struct{ uid atomic.Int64 }

func (o *multipleOwner) Owner(_ int64, _ string, _ int64, _ string, _ int64) int64 {
	return o.uid.Load()
}

type multipleBinder struct{ calls atomic.Int64 }

func (b *multipleBinder) Bind(_ int64) error { b.calls.Add(1); return nil }
func multipleTestGateway(t *testing.T, mode string) *Gateway {
	t.Helper()
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, _ := gateway.TLS(pair, ca)
	srv, _ := gateway.New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var address string
	if mode == "quic" {
		ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true, MaxIncomingStreams: 128})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { ln.Close() })
		go srv.ServeQUIC(ctx, ln)
		address = ln.Addr().String()
	} else {
		tc.NextProtos = []string{"http/1.1"}
		ln, e := tls.Listen("tcp", "127.0.0.1:0", tc)
		if e != nil {
			t.Fatal(e)
		}
		s := &http.Server{Handler: http.HandlerFunc(srv.WebSocket)}
		t.Cleanup(func() { s.Close() })
		go s.Serve(ln)
		address = ln.Addr().String()
	}
	g := NewGateway(nil)
	cfg, _ := json.Marshal(gatewayConfig{Transport: mode, Endpoint: address, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e := g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(g.Stop)
	return g
}
func TestMultipleIndependentTransportsAndFailClosed(t *testing.T) {
	owner := &multipleOwner{}
	owner.uid.Store(42)
	binder := &multipleBinder{}
	m, e := NewMultiRouter(`[{"id":"home","mode":"subnets","subnets":["192.168.50.0/24"]},{"id":"apps","mode":"apps","uids":[42]}]`, "apps", owner, binder, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	home := multipleTestGateway(t, "quic")
	apps := multipleTestGateway(t, "https")
	if e = m.SetGateway("home", home); e != nil {
		t.Fatal(e)
	}
	if e = m.SetGateway("apps", apps); e != nil {
		t.Fatal(e)
	}
	if m.selectFlow(6, "10.0.0.1", 1000, "192.168.50.1", 80).g != home {
		t.Fatal("subnet priority")
	}
	selected := m.selectFlow(6, "10.0.0.1", 1001, "127.0.0.1", 80)
	if selected.g != apps {
		t.Fatal("app route")
	}
	// Both transports carry real TCP connections concurrently.
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, e := home.dialStream(ctx, "tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := apps.dialStream(ctx, "tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	for _, c := range []gateway.Stream{a, b} {
		c.SetDeadline(time.Now().Add(3 * time.Second))
		if _, e = c.Write([]byte("ok")); e != nil {
			t.Fatal(e)
		}
		buf := make([]byte, 2)
		if _, e = io.ReadFull(c, buf); e != nil || string(buf) != "ok" {
			t.Fatal(e)
		}
	}
	// HTTPS replaces its transport context while retaining the same profile/TUN.
	if e = apps.MigrateTo("replacement", nil); e != nil {
		t.Fatal(e)
	}
	if selected.ctx.Err() != nil {
		t.Fatal("HTTPS migration canceled router profile")
	}
	migrated, e := selected.g.dialStream(ctx, "tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	migrated.SetDeadline(time.Now().Add(3 * time.Second))
	migrated.Write([]byte("new"))
	reply := make([]byte, 3)
	if _, e = io.ReadFull(migrated, reply); e != nil || string(reply) != "new" {
		t.Fatal("HTTPS after migration", e)
	}
	migrated.Close()

	m.RemoveGateway("home")
	if m.selectFlow(6, "10.0.0.1", 1002, "192.168.50.1", 80) != nil {
		t.Fatal("unavailable profile fell back")
	}
	if m.selectFlow(6, "10.0.0.1", 1003, "1.1.1.1", 80).g != apps {
		t.Fatal("other profile stopped")
	}
	owner.uid.Store(-1)
	if m.selectFlow(6, "10.0.0.1", 1004, "1.1.1.1", 80) != nil {
		t.Fatal("unknown UID leaked")
	}
	if m.selectFlow(17, "10.0.0.1", 1005, "1.1.1.1", 53).g != apps {
		t.Fatal("explicit DNS route")
	}
	owner.uid.Store(43)
	if m.selectFlow(6, "10.0.0.1", 1006, "1.1.1.1", 80).g.direct == nil {
		t.Fatal("unmatched not direct")
	}
	m.SetProfileEnabled("apps", false)
	owner.uid.Store(42)
	if m.selectFlow(6, "10.0.0.1", 1010, "1.1.1.1", 80) != nil {
		t.Fatal("paused profile leaked")
	}
	if selected.ctx.Err() == nil {
		t.Fatal("pause did not cancel flows")
	}
	if e = m.SetGateway("apps", apps); e == nil {
		t.Fatal("paused profile accepted gateway")
	}
	m.SetProfileEnabled("apps", true)
	replacement := NewGateway(nil)
	replacement.direct = &net.Dialer{}
	if e = m.SetGateway("apps", replacement); e != nil {
		t.Fatal(e)
	}
	m.DetachGateway("apps", apps)
	owner.uid.Store(42)
	if m.selectFlow(6, "10.0.0.1", 1007, "1.1.1.1", 80).g != replacement {
		t.Fatal("stale detach removed replacement")
	}
	if selected.ctx.Err() == nil {
		t.Fatal("replaced profile flows not canceled")
	}
}
func TestMultipleTUNUDPRoutes(t *testing.T) {
	var target net.IP
	addresses, _ := net.InterfaceAddrs()
	for _, a := range addresses {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip.To4() != nil && !ip.IsLoopback() {
			target = ip.To4()
			break
		}
	}
	if target == nil {
		t.Skip("non-loopback IPv4 required")
	}
	udp, e := net.ListenPacket("udp4", net.JoinHostPort(target.String(), "0"))
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, a, e := udp.ReadFrom(buf)
			if e != nil {
				return
			}
			udp.WriteTo(buf[:n], a)
		}
	}()
	owner := &multipleOwner{}
	binder := &multipleBinder{}
	m, e := NewMultiRouter(`[{"id":"q","mode":"apps","uids":[42]},{"id":"h","mode":"apps","uids":[43]}]`, "q", owner, binder, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	for _, item := range []struct{ id, mode string }{{"q", "quic"}, {"h", "https"}} {
		if e = m.SetGateway(item.id, multipleTestGateway(t, item.mode)); e != nil {
			t.Fatal(e)
		}
	}
	fds, e := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])
	if e = m.Attach(fds[0]); e != nil {
		t.Fatal(e)
	}
	for i, uid := range []int64{42, 43, 44} {
		owner.uid.Store(uid)
		payload := []byte("route-" + strconv.Itoa(i))
		p := make([]byte, 28+len(payload))
		p[0] = 0x45
		p[8] = 64
		p[9] = 17
		binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
		copy(p[12:16], []byte{10, 254, 254, 1})
		copy(p[16:20], target)
		var sum uint32
		for j := 0; j < 20; j += 2 {
			sum += uint32(binary.BigEndian.Uint16(p[j : j+2]))
		}
		for sum > 65535 {
			sum = (sum & 65535) + (sum >> 16)
		}
		binary.BigEndian.PutUint16(p[10:12], ^uint16(sum))
		binary.BigEndian.PutUint16(p[20:22], uint16(12000+i))
		binary.BigEndian.PutUint16(p[22:24], uint16(udp.LocalAddr().(*net.UDPAddr).Port))
		binary.BigEndian.PutUint16(p[24:26], uint16(len(p)-20))
		copy(p[28:], payload)
		if _, e = unix.Write(fds[1], p); e != nil {
			t.Fatal(e)
		}
		poll := []unix.PollFd{{Fd: int32(fds[1]), Events: unix.POLLIN}}
		if n, e := unix.Poll(poll, 5000); e != nil || n == 0 {
			t.Fatalf("route %d timeout: %v", uid, e)
		}
		buf := make([]byte, 1500)
		n, e := unix.Read(fds[1], buf)
		if e != nil || n < 28 || string(buf[28:n]) != string(payload) {
			t.Fatalf("route %d response: %v", uid, e)
		}
	}
	if binder.calls.Load() == 0 {
		t.Fatal("direct bypass socket not protected")
	}
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("multiple stop hung")
	}
}
