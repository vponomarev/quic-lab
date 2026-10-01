package mobile

import (
	"errors"
	"net"
	"quiclab/internal/trafficbudget"
)

// Wrappers deliberately expose only Conn/PacketConn: optimized raw/batch I/O
// must not bypass accounting. TLS wraps the metered TCP connection, not vice versa.
type meteredConn struct {
	net.Conn
	b          *TrafficBudget
	class      trafficbudget.Class
	unregister func()
}

func meterConn(c net.Conn, network string, b *TrafficBudget, class trafficbudget.Class) net.Conn {
	if b == nil || network != "cell" {
		return c
	}
	m := &meteredConn{Conn: c, b: b, class: class}
	m.unregister = b.meter.Register(func() { c.Close() })
	return m
}
func (m *meteredConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	id, ok := m.b.meter.Reserve(uint64(len(p)), m.class)
	if !ok {
		return 0, trafficbudget.ErrBlocked
	}
	n, e := m.Conn.Write(p)
	m.b.meter.Commit(id, uint64(n))
	return n, e
}
func (m *meteredConn) Read(p []byte) (int, error) {
	if !m.b.meter.Allowed() {
		return 0, trafficbudget.ErrBlocked
	}
	n, e := m.Conn.Read(p)
	m.b.meter.Receive(uint64(n), m.class)
	return n, e
}
func (m *meteredConn) Close() error { m.unregister(); return m.Conn.Close() }

type meteredPacketConn struct {
	net.PacketConn
	b          *TrafficBudget
	class      trafficbudget.Class
	unregister func()
}

func meterPacketConn(c net.PacketConn, network string, b *TrafficBudget, class trafficbudget.Class) net.PacketConn {
	if b == nil || network != "cell" {
		return c
	}
	m := &meteredPacketConn{PacketConn: c, b: b, class: class}
	m.unregister = b.meter.Register(func() { c.Close() })
	return m
}
func (m *meteredPacketConn) WriteTo(p []byte, a net.Addr) (int, error) {
	id, ok := m.b.meter.Reserve(uint64(len(p)), m.class)
	if !ok {
		return 0, trafficbudget.ErrBlocked
	}
	n, e := m.PacketConn.WriteTo(p, a)
	m.b.meter.Commit(id, uint64(n))
	return n, e
}
func (m *meteredPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if !m.b.meter.Allowed() {
		return 0, nil, trafficbudget.ErrBlocked
	}
	n, a, e := m.PacketConn.ReadFrom(p)
	m.b.meter.Receive(uint64(n), m.class)
	if n == 0 && !m.b.meter.Allowed() {
		e = trafficbudget.ErrBlocked
	}
	return n, a, e
}
func (m *meteredPacketConn) Close() error { m.unregister(); return m.PacketConn.Close() }

// budgetBinder keeps physical network identity with every socket/reconnect.
type budgetBinder struct {
	SocketBinder
	budget  *TrafficBudget
	network string
}

func (b *budgetBinder) Bind(fd int64) error {
	if b.network != "cell" && b.network != "wifi" {
		return errors.New("unknown physical network")
	}
	if b.network == "cell" && !b.budget.meter.Allowed() {
		return trafficbudget.ErrBlocked
	}
	if b.SocketBinder != nil {
		return b.SocketBinder.Bind(fd)
	}
	return nil
}
func (b *budgetBinder) CellMeter() *trafficbudget.Meter {
	if b.network == "cell" {
		return b.budget.meter
	}
	return nil
}
func (b *TrafficBudget) Bind(binder SocketBinder, network string) SocketBinder {
	return &budgetBinder{SocketBinder: binder, budget: b, network: network}
}
func (b *TrafficBudget) CellAllowed() bool { return b.meter.Allowed() }
func meterBoundConn(c net.Conn, binder SocketBinder) net.Conn {
	if b, ok := binder.(*budgetBinder); ok {
		return meterConn(c, b.network, b.budget, trafficbudget.User)
	}
	return c
}
func meterBoundPacket(c net.PacketConn, binder SocketBinder) net.PacketConn {
	if b, ok := binder.(*budgetBinder); ok {
		return meterPacketConn(c, b.network, b.budget, trafficbudget.User)
	}
	return c
}
