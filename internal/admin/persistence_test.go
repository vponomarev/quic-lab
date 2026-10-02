package admin

import (
	"errors"
	"os"
	"testing"
)

func TestPublishedSaveFailureKeepsMemoryAndRevocation(t *testing.T) {
	for _, op := range []string{"rename", "create-user", "create-device", "disable-device", "disable-user", "delete-user", "configure-awg"} {
		t.Run(op, func(t *testing.T) {
			dir := t.TempDir()
			s, e := OpenStore(dir)
			if e != nil {
				t.Fatal(e)
			}
			u, e := s.CreateWithProtocols("owner", []string{"quic"})
			if e != nil {
				t.Fatal(e)
			}
			closed := 0
			s.RegisterProtocol(deviceTLS(t, u.Certificate), "quic", func() { closed++ })
			calls := 0
			s.syncDir = func(string) error {
				calls++
				if calls == 2 {
					return errors.New("injected directory sync failure")
				}
				return nil
			}
			switch op {
			case "rename":
				e = s.Rename(u.ID, "new name")
			case "create-user":
				_, e = s.CreateWithProtocols("new user", []string{"quic"})
			case "create-device":
				_, e = s.CreateDevice(u.ID, "new phone")
			case "disable-device":
				e = s.DisableDevice(u.ID)
			case "disable-user":
				e = s.SetProtocols(u.ID, []string{"quic"}, true)
			case "delete-user":
				e = s.Delete(u.ID)
			case "configure-awg":
				e = s.ConfigureAWG(awgConfig())
			}
			if e == nil {
				t.Fatal("post-publication directory sync failure hidden")
			}
			disk, e := OpenStore(dir)
			if e != nil {
				t.Fatal(e)
			}
			switch op {
			case "rename":
				if s.state.Users[u.ID].Name != "new name" || disk.state.Users[u.ID].Name != "new name" {
					t.Fatal("published rename rolled memory back")
				}
			case "create-user":
				if len(s.List()) != 2 || len(disk.List()) != 2 {
					t.Fatal("published user missing in memory")
				}
			case "create-device":
				if len(s.Devices(u.ID)) != 2 || len(disk.Devices(u.ID)) != 2 {
					t.Fatal("published device missing in memory")
				}
			case "disable-device", "disable-user", "delete-user":
				if s.Verify(deviceTLS(t, u.Certificate)) == nil || disk.Verify(deviceTLS(t, u.Certificate)) == nil || closed != 1 {
					t.Fatal("published revocation restored authorization or skipped callbacks")
				}
			case "configure-awg":
				if s.state.AWG == nil || disk.state.AWG == nil || s.state.AWG.Private != disk.state.AWG.Private {
					t.Fatal("published AWG identity rolled back")
				}
			}
		})
	}
}
func TestBackupDirectorySyncFailurePreservesPrimary(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateWithProtocols("owner", []string{"quic"})
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(s.path)
	closed := 0
	s.RegisterProtocol(deviceTLS(t, u.Certificate), "quic", func() { closed++ })
	s.syncDir = func(string) error { return errors.New("backup directory sync failed") }
	if e = s.DisableDevice(u.ID); e == nil {
		t.Fatal("backup durability failure hidden")
	}
	after, _ := os.ReadFile(s.path)
	if string(before) != string(after) || s.Verify(deviceTLS(t, u.Certificate)) != nil || closed != 0 {
		t.Fatal("pre-publication failure changed primary or authorization")
	}
}
