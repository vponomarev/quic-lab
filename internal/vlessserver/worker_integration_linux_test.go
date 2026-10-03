//go:build linux

package vlessserver

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"quiclab/internal/vless"
	"syscall"
	"testing"
	"time"
)

func TestWorkerProcessRevokeAdmissionLossAndSIGTERM(t *testing.T) {
	binary := os.Getenv("QUICLAB_VLESS_WORKER_BINARY")
	if binary == "" {
		t.Skip("isolated worker binary not supplied")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	brokerDir := t.TempDir()
	os.Chmod(brokerDir, 0700)
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture.invalid"}, DNSNames: []string{"fixture.invalid"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, e := x509.MarshalECPrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	os.WriteFile(filepath.Join(dir, "tls.crt"), cert, 0600)
	os.WriteFile(filepath.Join(dir, "tls.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600)
	slot, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := slot.Addr().String()
	slot.Close()
	s := sampleSnapshot()
	s.Config = tlsConfig()
	s.Config.Listen = addr
	s.Config.Endpoint = addr
	s.Config.ServerName = "fixture.invalid"
	s.Config.Fingerprint = ""
	s.Config.TLSCertificateFile = filepath.Join(dir, "tls.crt")
	s.Config.TLSKeyFile = filepath.Join(dir, "tls.key")
	broker, e := ServeAdmission(context.Background(), brokerDir, func(context.Context, string) (time.Duration, error) { return 200 * time.Millisecond, nil })
	if e != nil {
		t.Fatal(e)
	}
	defer broker()
	var output bytes.Buffer
	process := exec.Command(binary, "-data-dir", dir, "-admission-dir", brokerDir)
	process.Stdout = &output
	process.Stderr = &output
	if e := process.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	finished := false
	defer func() {
		if !finished {
			process.Process.Kill()
			<-done
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(dir, "vless-control.sock")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker control unavailable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	control := &ControlClient{Dir: dir}
	if e := control.Apply(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	exchange := func(c net.Conn) {
		t.Helper()
		c.SetDeadline(time.Now().Add(time.Second))
		c.Write([]byte("echo"))
		b := make([]byte, 4)
		if _, e := io.ReadFull(c, b); e != nil || string(b) != "echo" {
			t.Fatal("worker round trip failed")
		}
	}
	var flows []net.Conn
	for _, d := range s.Devices {
		client, e := vless.New(context.Background(), vless.Config{Endpoint: addr, UUID: d.UUID, Security: "tls", ServerName: "fixture.invalid", RootPEM: cert}, &net.Dialer{})
		if e != nil {
			t.Fatal(e)
		}
		defer client.Close()
		c, e := client.DialContext(context.Background(), "tcp4", echo.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		exchange(c)
		flows = append(flows, c)
	}
	closed := func(c net.Conn) {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, e := c.Read(make([]byte, 1)); e == nil {
			t.Fatal("revoked flow alive")
		} else if n, ok := e.(net.Error); ok && n.Timeout() {
			t.Fatal("flow closure timed out")
		}
	}
	s.Revision++
	s.Devices[0].Disabled = true
	if e := control.Apply(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	closed(flows[0])
	exchange(flows[1])
	broker()
	closed(flows[1])
	process.Process.Signal(syscall.SIGTERM)
	select {
	case e := <-done:
		finished = true
		if e != nil {
			t.Fatal("worker SIGTERM failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker failed to stop")
	}
	check, e := net.Listen("tcp4", addr)
	if e != nil {
		t.Fatal("listener port leaked")
	}
	check.Close()
	for _, d := range s.Devices {
		if bytes.Contains(output.Bytes(), []byte(d.UUID)) {
			t.Fatal("worker leaked UUID")
		}
	}
	if bytes.Contains(output.Bytes(), keyDER) {
		t.Fatal("worker leaked private key")
	}
}
