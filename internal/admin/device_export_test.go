package admin

import (
	"strings"
	"testing"
)

func TestDeviceAWGExportUsesOwnPeer(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s.ConfigureAWG(awgConfig())
	u, e := s.CreateWithProtocols("owner", []string{"awg"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.CreateDevice(u.ID, "second")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s.AWGProfile(d.ID)
	if e != nil || !strings.Contains(raw, "Address = "+d.AWG.Address+"/32") {
		t.Fatal("device AWG export unavailable", e)
	}
	if e = s.DisableDevice(d.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AWGProfile(d.ID); e == nil {
		t.Fatal("disabled device exported")
	}
	if _, e = s.AWGProfile(u.ID); e != nil {
		t.Fatal("sibling export lost", e)
	}
}
