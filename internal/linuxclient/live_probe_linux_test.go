//go:build linux && !android

package linuxclient

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Holds the very same TCP and UDP sockets while the script changes policies and
// suspends/resumes the VPN fixture. Fresh connections would hide a direct reset.
func TestLinuxPersistentDirectProbe(t *testing.T) {
	dir := os.Getenv("QUIC_APP_HOLD_DIR")
	if dir == "" {
		t.Skip("private namespace fixture only")
	}
	var conns []net.Conn
	for _, p := range []struct{ network, address string }{{"tcp4", "192.0.2.2:9011"}, {"udp4", "192.0.2.2:9012"}} {
		c, e := net.DialTimeout(p.network, p.address, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		conns = append(conns, c)
	}
	payload := bytes.Repeat([]byte("per-app-payload"), 50)
	expected := append([]byte("198.18.0.1|"), payload...)
	deadline := time.Now().Add(200 * time.Second)
	for count := 1; ; count++ {
		for _, c := range conns {
			c.SetDeadline(time.Now().Add(2 * time.Second))
			if _, e := c.Write(payload); e != nil {
				t.Fatal(e)
			}
			response := make([]byte, len(expected))
			if _, e := io.ReadFull(c, response); e != nil || !bytes.Equal(response, expected) {
				t.Fatal("persistent direct source/payload", e)
			}
		}
		os.WriteFile(filepath.Join(dir, "hold.count"), []byte(fmt.Sprint(count)), 0600)
		if _, e := os.Stat(filepath.Join(dir, "hold.stop")); e == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("hold fixture timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
