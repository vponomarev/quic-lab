package awgserver

import (
	"context"
	"encoding/json"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"quiclab/internal/awg/netstack"
	"strings"
	"testing"
	"time"
)

func deviceWorker(t *testing.T) *Worker {
	t.Helper()
	tun, _, e := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.77.0.1")}, nil, 1280)
	if e != nil {
		t.Fatal(e)
	}
	d := device.NewDevice(tun, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	t.Cleanup(d.Close)
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	return &Worker{Device: d, Config: Config{Endpoint: "127.0.0.1:51820", Address: "10.77.0.1/29", MTU: 1280}, Dir: dir}
}
func deviceState(t *testing.T, identity *Identity, first, second *Peer, disabled bool) State {
	t.Helper()
	now := time.Now().Add(time.Hour)
	raw, e := json.Marshal(map[string]any{"version": 2, "awg": identity, "users": map[string]any{"owner": User{ID: "owner", Protocols: []string{"awg"}, Expires: now, AWG: first}}, "devices": map[string]any{"first": map[string]any{"id": "first", "user_id": "owner", "expires": now, "awg": first, "disabled": disabled}, "second": map[string]any{"id": "second", "user_id": "owner", "expires": now, "awg": second}}})
	if e != nil {
		t.Fatal(e)
	}
	var state State
	if e = json.Unmarshal(raw, &state); e != nil {
		t.Fatal(e)
	}
	return state
}
func TestWorkerDeviceRevocationPreservesSibling(t *testing.T) {
	w := deviceWorker(t)
	identity, _ := NewIdentity()
	first, _ := NewPeer("10.77.0.2")
	second, _ := NewPeer("10.77.0.3")
	state := deviceState(t, identity, first, second, false)
	if e := w.Apply(state); e != nil {
		t.Fatal(e)
	}
	pub1, _ := KeyHex(first.Public)
	pub2, _ := KeyHex(second.Public)
	raw, e := w.Device.IpcGet()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(raw, "public_key="+pub1) || !strings.Contains(raw, "public_key="+pub2) {
		t.Fatal("per-device peers missing")
	}
	state = deviceState(t, identity, first, second, true)
	if e = w.Apply(state); e != nil {
		t.Fatal(e)
	}
	raw, _ = w.Device.IpcGet()
	if strings.Contains(raw, "public_key="+pub1) || !strings.Contains(raw, "public_key="+pub2) {
		t.Fatal("device revoke retained old alias or removed sibling")
	}
	status, e := w.Snapshot(state)
	if e != nil || len(status.Peers) != 1 || status.Peers[0].ID != "second" {
		t.Fatal("device status ownership", e)
	}
}
func TestWorkerReloadAcknowledgesPeerRemoval(t *testing.T) {
	w := deviceWorker(t)
	identity, _ := NewIdentity()
	first, _ := NewPeer("10.77.0.2")
	second, _ := NewPeer("10.77.0.3")
	state := deviceState(t, identity, first, second, false)
	raw, _ := json.Marshal(state)
	if e := os.WriteFile(filepath.Join(w.Dir, "identities.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := w.Tick(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	path := filepath.Join(w.Dir, "awg-reload.sock")
	var socket net.Conn
	var e error
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		socket, e = net.DialTimeout("unix", path, 50*time.Millisecond)
		if e == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e != nil {
		select {
		case runErr := <-done:
			t.Fatal("worker startup failed", runErr)
		default:
			t.Fatal("worker does not offer acknowledged reload", e)
		}
	}
	defer socket.Close()
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("reload socket exposes control")
	}
	state = deviceState(t, identity, first, second, true)
	raw, _ = json.Marshal(state)
	os.WriteFile(filepath.Join(w.Dir, "identities.json"), raw, 0600)
	socket.SetDeadline(time.Now().Add(time.Second))
	if _, e = socket.Write([]byte("reload\n")); e != nil {
		t.Fatal(e)
	}
	reply := make([]byte, 16)
	n, e := socket.Read(reply)
	if e != nil || string(reply[:n]) != "ok\n" {
		t.Fatal("reload acknowledgment", e, string(reply[:n]))
	}
	runtime, _ := w.Device.IpcGet()
	pub, _ := KeyHex(first.Public)
	if strings.Contains(runtime, "public_key="+pub) {
		t.Fatal("acknowledged before peer removal")
	}
	cancel()
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("worker shutdown blocked")
	}
}
