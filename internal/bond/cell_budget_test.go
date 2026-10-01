package bond

import (
	"testing"
	"time"
)

func TestCellBlockNotifiesPeerOverWifi(t *testing.T) {
	a, b, _ := setup(t)
	a.Session.BlockCellAndNotify()
	deadline := time.Now().Add(time.Second)
	for b.Session.CellAllowed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.Session.CellAllowed() || b.Session.CellAllowed() {
		t.Fatal("LTE block did not reach peer")
	}
	if !a.Session.HasPath("wifi") || !b.Session.HasPath("wifi") {
		t.Fatal("Wi-Fi removed")
	}
}
