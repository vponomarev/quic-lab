package mobile

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestWarmStandbyReprobeAndRetirement(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 4096)}
	c := NewClient(sink)
	defer c.Stop()
	if err := c.Start(addr, "", pin, 20, nil); err != nil {
		t.Fatal(err)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	old := c.paths[0].udp
	for range 4 {
		if err := c.PreparePath("backup", nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	before := nextEcho(t, sink, func(e map[string]any) bool { return e["seq"].(float64) > first["seq"].(float64) })
	if before["peer"] != first["peer"] {
		t.Fatal("standby moved traffic")
	}
	if err := c.MigrateTo("backup", nil); err != nil {
		t.Fatal(err)
	}
	next := nextEcho(t, sink, func(e map[string]any) bool { return e["peer"] != first["peer"] })
	if next["connection_id"] != first["connection_id"] {
		t.Fatal("reconnected")
	}
	c.retirement.Wait()
	if _, err := old.WriteTo([]byte("closed-socket-check"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}); err == nil {
		t.Fatal("old socket retained")
	}
	if len(c.paths) != 1 {
		t.Fatal("active transport accumulation")
	}
}

func TestUnavailableStandbyDoesNotKillActiveConnection(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 4096)}
	c := NewClient(sink)
	defer c.Stop()
	if err := c.Start(addr, "", pin, 20, nil); err != nil {
		t.Fatal(err)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	c.newTransport = func(ip net.IP, b SocketBinder, cb func(net.Addr, error)) (transport, error) {
		tr, err := openTransport(ip, b, cb)
		if err == nil {
			socket := tr.q.Conn.(*pathSocket)
			loss := &failingPacketConn{PacketConn: socket.PacketConn, dropReads: true}
			loss.failed.Store(true)
			socket.PacketConn = loss
		}
		return tr, err
	}
	start := time.Now()
	if err := c.PreparePath("unreachable", nil); err == nil {
		t.Fatal("accepted unreachable standby")
	}
	if time.Since(start) > time.Second {
		t.Fatal("standby blocked too long")
	}
	c.retirement.Wait()
	next := nextEcho(t, sink, func(e map[string]any) bool { return e["seq"].(float64) > first["seq"].(float64)+20 })
	if next["connection_id"] != first["connection_id"] {
		t.Fatal("active connection changed")
	}
}

func TestBothPathsTemporarilyUnavailable(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 4096)}
	c := NewClient(sink)
	defer c.Stop()
	var sockets []*failingPacketConn
	c.newTransport = func(ip net.IP, b SocketBinder, cb func(net.Addr, error)) (transport, error) {
		tr, err := openTransport(ip, b, cb)
		if err == nil {
			socket := tr.q.Conn.(*pathSocket)
			loss := &failingPacketConn{PacketConn: socket.PacketConn, dropReads: true}
			socket.PacketConn = loss
			sockets = append(sockets, loss)
		}
		return tr, err
	}
	if err := c.Start(addr, "", pin, 20, nil); err != nil {
		t.Fatal(err)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	if err := c.PreparePath("backup", nil); err != nil {
		t.Fatal(err)
	}
	for _, s := range sockets {
		s.failed.Store(true)
	}
	if err := c.MigrateTo("backup", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	sockets[1].failed.Store(false)
	next := nextEcho(t, sink, func(e map[string]any) bool { return e["peer"] != first["peer"] })
	if next["connection_id"] != first["connection_id"] || next["stream_id"] != first["stream_id"] {
		t.Fatal("did not preserve session")
	}
}

func TestRepeatedFailedProbesRecoverAfterBothPathsOutage(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 4096)}
	c := NewClient(sink)
	defer c.Stop()
	var sockets []*failingPacketConn
	c.newTransport = func(ip net.IP, b SocketBinder, cb func(net.Addr, error)) (transport, error) {
		tr, err := openTransport(ip, b, cb)
		if err == nil {
			socket := tr.q.Conn.(*pathSocket)
			loss := &failingPacketConn{PacketConn: socket.PacketConn, dropReads: true}
			if len(sockets) >= 2 {
				loss.failed.Store(true)
			}
			socket.PacketConn = loss
			sockets = append(sockets, loss)
		}
		return tr, err
	}
	if err := c.Start(addr, "", pin, 20, nil); err != nil {
		t.Fatal(err)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	if err := c.PreparePath("backup", nil); err != nil {
		t.Fatal(err)
	}
	for _, socket := range sockets {
		socket.failed.Store(true)
	}
	for range 8 {
		if err := c.PreparePath("backup", nil); err == nil {
			t.Fatal("accepted an unavailable path")
		}
	}
	for _, socket := range sockets[1:] {
		socket.failed.Store(false)
	}
	if err := c.MigrateTo("backup", nil); err != nil {
		t.Fatalf("backup recovered, but validation is stuck: %v", err)
	}
	next := nextEcho(t, sink, func(e map[string]any) bool { return e["peer"] != first["peer"] })
	if next["connection_id"] != first["connection_id"] {
		t.Fatal("reconnected instead of migrating")
	}
}

type inboundLossConn struct {
	net.PacketConn
	blocked atomic.Bool
}

func (p *inboundLossConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, a, err := p.PacketConn.ReadFrom(b)
		if err != nil || !p.blocked.Load() {
			return n, a, err
		}
	}
}
func TestColdStandbyRecoversAfterLostResponses(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 4096)}
	c := NewClient(sink)
	defer c.Stop()
	var active *failingPacketConn
	var backup *inboundLossConn
	c.newTransport = func(ip net.IP, b SocketBinder, cb func(net.Addr, error)) (transport, error) {
		tr, err := openTransport(ip, b, cb)
		if err == nil {
			socket := tr.q.Conn.(*pathSocket)
			if active == nil {
				active = &failingPacketConn{PacketConn: socket.PacketConn, dropReads: true}
				socket.PacketConn = active
			} else {
				backup = &inboundLossConn{PacketConn: socket.PacketConn}
				backup.blocked.Store(true)
				socket.PacketConn = backup
			}
		}
		return tr, err
	}
	if err := c.Start(addr, "", pin, 20, nil); err != nil {
		t.Fatal(err)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	active.failed.Store(true)
	for range 8 {
		if err := c.PreparePath("backup", nil); err == nil {
			t.Fatal("accepted lost responses")
		}
	}
	backup.blocked.Store(false)
	if err := c.MigrateTo("backup", nil); err != nil {
		t.Fatalf("responses recovered, migration stuck: %v", err)
	}
	next := nextEcho(t, sink, func(e map[string]any) bool { return e["peer"] != first["peer"] })
	if next["connection_id"] != first["connection_id"] {
		t.Fatal("reconnected")
	}
}
