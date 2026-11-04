package approuting

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWindowsOwnerLiveLoopback(t *testing.T) {
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	c, e := net.Dial("tcp4", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	accepted, e := listener.Accept()
	if e != nil {
		t.Fatal(e)
	}
	defer accepted.Close()
	udp, e := net.ListenPacket("udp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	r := NewWindowsOwnerResolver()
	exe, _ := os.Executable()
	identity, e := ResolveExecutable(exe)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range []FlowKey{
		{6, netip.MustParseAddrPort(c.LocalAddr().String()), netip.MustParseAddrPort(c.RemoteAddr().String())},
		{17, netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(udp.LocalAddr().(*net.UDPAddr).Port)), netip.MustParseAddrPort("127.0.0.1:9999")},
	} {
		a := r.Resolve(context.Background(), f)
		if a.Quality != Resolved || len(a.Owners) != 1 || a.Owners[0].PID != uint32(os.Getpid()) || !strings.EqualFold(a.Owners[0].Path, identity.Path) || a.Owners[0].Created == 0 {
			t.Fatalf("wrong loopback attribution: %+v", a)
		}
	}
}

func TestWindowsOwnerTableDecodeAndBounds(t *testing.T) {
	b := make([]byte, 28)
	binary.LittleEndian.PutUint32(b, 1)
	copy(b[8:], []byte{10, 1, 2, 3})
	binary.BigEndian.PutUint16(b[12:], 4567)
	copy(b[16:], []byte{1, 2, 3, 4})
	binary.BigEndian.PutUint16(b[20:], 443)
	binary.LittleEndian.PutUint32(b[24:], 99)
	rows, e := decodeOwnerTable(b, 6)
	if e != nil || len(rows) != 1 || rows[0].Local.String() != "10.1.2.3:4567" || rows[0].Remote.String() != "1.2.3.4:443" || rows[0].PID != 99 {
		t.Fatal(rows, e)
	}
	for _, bad := range [][]byte{nil, b[:27], {255, 255, 255, 255}} {
		if _, e := decodeOwnerTable(bad, 6); e == nil {
			t.Fatal("accepted truncated/overflow table")
		}
	}
}

func TestWindowsOwnerRejectsProcessNewerThanSnapshot(t *testing.T) {
	if _, e := processIdentity(uint32(os.Getpid()), 1); e == nil {
		t.Fatal("PID reuse accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	a := NewWindowsOwnerResolver().Resolve(ctx, FlowKey{})
	if a.Reason != "canceled" || time.Since(started) > time.Second {
		t.Fatal(a)
	}
}

func TestWindowsOwnerLimitSurvivesResolverReconstruction(t *testing.T) {
	first := NewWindowsOwnerResolver()
	oldLookup, oldTimeout := first.lookup, first.timeout
	gate := make(chan struct{})
	returned := make(chan struct{}, 4)
	first.lookup = func(context.Context, FlowKey) Attribution {
		<-gate
		returned <- struct{}{}
		return Attribution{Quality: Unknown}
	}
	first.timeout = 10 * time.Millisecond
	defer func() {
		close(gate)
		for i := 0; i < 4; i++ {
			<-returned
		}
		first.lookup = oldLookup
		first.timeout = oldTimeout
	}()
	for i := 0; i < 4; i++ {
		if a := first.Resolve(context.Background(), FlowKey{}); a.Reason != "timeout" {
			t.Fatal(a)
		}
	}
	next := NewWindowsOwnerResolver()
	if a := next.Resolve(context.Background(), FlowKey{}); a.Reason != "overloaded" {
		t.Fatalf("new engine escaped native lookup bound: %+v", a)
	}
}
