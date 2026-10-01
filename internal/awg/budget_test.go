package awg

import (
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"quiclab/internal/trafficbudget"
	"testing"
)

type gsoFallbackBind struct{ conn.Bind }

func (gsoFallbackBind) Send([][]byte, conn.Endpoint) error { return conn.ErrUDPGSODisabled{} }
func TestAccountingCountsSuccessfulGSOFallback(t *testing.T) {
	m := trafficbudget.NewMeter(trafficbudget.New("gso", 100))
	b := &protectedBind{Bind: gsoFallbackBind{}, meter: m}
	if e := b.Send([][]byte{make([]byte, 3), make([]byte, 4)}, nil); e != nil {
		t.Fatal(e)
	}
	if m.Ledger.Snapshot().Used != 7 {
		t.Fatal(m.Ledger.Snapshot())
	}
}
