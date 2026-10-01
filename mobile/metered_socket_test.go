package mobile

import (
	"errors"
	"io"
	"net"
	"quiclab/internal/trafficbudget"
	"testing"
	"time"
)

type budgetTestConn struct {
	net.Conn
	wrote  int
	closed bool
	short  int
}

func (c *budgetTestConn) Write(p []byte) (int, error) {
	n := len(p)
	if c.short > 0 && n > c.short {
		n = c.short
	}
	c.wrote += n
	return n, nil
}
func (c *budgetTestConn) Read(p []byte) (int, error) { copy(p, "123456"); return 6, io.EOF }
func (c *budgetTestConn) Close() error               { c.closed = true; return nil }
func TestSocketAccountingOnce(t *testing.T) {
	b, _ := NewTrafficBudget("run", 100)
	raw := &budgetTestConn{short: 3}
	c := meterConn(raw, "cell", b, trafficbudget.User)
	if n, e := c.Write(make([]byte, 10)); n != 3 || e != nil {
		t.Fatal(n, e)
	}
	if s := b.ledger.Snapshot(); s.Used != 3 || s.Reserved != 0 {
		t.Fatal(s)
	}
	c.Read(make([]byte, 10))
	if b.ledger.Snapshot().Used != 9 {
		t.Fatal(b.Snapshot())
	}
	c.Close()
}
func TestBudgetClosesEveryCellPath(t *testing.T) {
	b, _ := NewTrafficBudget("run", 10)
	a, z, w := &budgetTestConn{}, &budgetTestConn{}, &budgetTestConn{}
	ca := meterConn(a, "cell", b, trafficbudget.User)
	_ = meterConn(z, "cell", b, trafficbudget.Control)
	cw := meterConn(w, "wifi", b, trafficbudget.User)
	ca.Write(make([]byte, 10))
	if !a.closed || !z.closed || w.closed {
		t.Fatal("closure escaped cellular scope")
	}
	if _, e := ca.Write([]byte{1}); !errors.Is(e, trafficbudget.ErrBlocked) {
		t.Fatal(e)
	}
	cw.Write([]byte{1})
	if b.ledger.Snapshot().Used != 10 {
		t.Fatal("counted wifi")
	}
	later := &budgetTestConn{}
	cl := meterConn(later, "cell", b, trafficbudget.User)
	if _, e := cl.Write([]byte{1}); !errors.Is(e, trafficbudget.ErrBlocked) || !later.closed {
		t.Fatal("late socket escaped")
	}
}
func TestBudgetPacketReadOverrun(t *testing.T) {
	b, _ := NewTrafficBudget("run", 3)
	raw, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	c := meterPacketConn(raw, "cell", b, trafficbudget.User)
	defer c.Close()
	sender, e := net.Dial("udp4", raw.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer sender.Close()
	sender.Write([]byte("12345"))
	c.SetReadDeadline(time.Now().Add(time.Second))
	n, _, _ := c.ReadFrom(make([]byte, 20))
	if n != 5 || b.ledger.Snapshot().Used != 5 || !b.ledger.Snapshot().Blocked {
		t.Fatal(n, b.Snapshot())
	}
}

func TestBudgetQUICMigrationKeepsSession(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 4096)}
	c := NewClient(sink)
	defer c.Stop()
	b, _ := NewTrafficBudget("run", 1000000)
	if e := c.Start(addr, "", pin, 20, b.Bind(nil, "cell")); e != nil {
		t.Fatal(e)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	if e := c.PreparePath("wifi", b.Bind(nil, "wifi")); e != nil {
		t.Fatal(e)
	}
	b.meter.Receive(1000000, trafficbudget.User)
	if e := c.MigrateTo("wifi", b.Bind(nil, "wifi")); e != nil {
		t.Fatal(e)
	}
	next := nextEcho(t, sink, func(e map[string]any) bool { return e["peer"] != first["peer"] })
	if next["connection_id"] != first["connection_id"] {
		t.Fatal("logical session ended")
	}
	if _, e := openTransport(net.IPv4(127, 0, 0, 1), b.Bind(nil, "cell"), nil); !errors.Is(e, trafficbudget.ErrBlocked) {
		t.Fatal("new LTE socket allowed", e)
	}
}

func TestBudgetStopsWhenNextDatagramCannotFit(t *testing.T) {
	b, _ := NewTrafficBudget("tail", 3)
	c := meterConn(&budgetTestConn{}, "cell", b, trafficbudget.User)
	if _, e := c.Write(make([]byte, 4)); !errors.Is(e, trafficbudget.ErrBlocked) {
		t.Fatal(e)
	}
	if b.CellAllowed() {
		t.Fatal("unusable remaining budget left LTE active")
	}
	if b.ledger.Snapshot().Used != 0 {
		t.Fatal("unsent bytes charged")
	}
}
