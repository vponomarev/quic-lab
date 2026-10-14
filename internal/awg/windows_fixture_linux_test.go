package awg

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit bounded fixture, no host routes or persistent accounts are modified.
func TestWindowsPeerFixture(t *testing.T) {
	dir := os.Getenv("QUIC_LAB_WINDOWS_AWG_FIXTURE")
	if dir == "" {
		t.Skip("opt-in isolated Windows peer fixture")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute directory required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	a, _ := ecdh.X25519().GenerateKey(rand.Reader)
	b, _ := ecdh.X25519().GenerateKey(rand.Reader)
	c := &Config{Address: "10.55.0.1", DNS: "10.55.0.1", MTU: 1280, private: hex.EncodeToString(b.Bytes()), public: hex.EncodeToString(a.PublicKey().Bytes()), Allowed: []string{"10.55.0.2/32"}, params: map[string]string{"jc": "2", "jmin": "50", "jmax": "100", "s1": "134", "s2": "90", "h1": "101", "h2": "102", "h3": "103", "h4": "104"}}
	server, e := Start(c, "127.0.0.1:9", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	ipc, e := server.device.IpcGet()
	if e != nil {
		t.Fatal(e)
	}
	port := ""
	for _, line := range strings.Split(ipc, "\n") {
		if strings.HasPrefix(line, "listen_port=") {
			port = strings.TrimPrefix(line, "listen_port=")
		}
	}
	raw := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 10.55.0.2/32\nDNS = 10.55.0.1\nJc = 2\nJmin = 50\nJmax = 100\nS1 = 134\nS2 = 90\nH1 = 101\nH2 = 102\nH3 = 103\nH4 = 104\n[Peer]\nPublicKey = %s\nEndpoint = 192.168.5.214:%s\nAllowedIPs = 10.55.0.1/32\n", base64.StdEncoding.EncodeToString(a.Bytes()), base64.StdEncoding.EncodeToString(b.PublicKey().Bytes()), port)
	if e = os.WriteFile(filepath.Join(dir, "client.conf"), []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	ln, e := server.network.ListenTCPAddrPort(netip.MustParseAddrPort("10.55.0.1:8080"))
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, e := server.network.ListenUDPAddrPort(netip.MustParseAddrPort("10.55.0.1:9999"))
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 2048)
		for {
			n, a, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	t.Log("fixture ready; credentials stored privately")
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
