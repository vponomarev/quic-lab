package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestManagementOnlyConfig(t *testing.T) {
	c, e := parseServerConfig([]string{"-ephemeral-cert", "-management-only", "-admin-config", "admin.json", "-https-listen", "127.0.0.1:8443"})
	if e != nil {
		t.Fatal(e)
	}
	if !c.ManagementOnly {
		t.Fatal("management mode missing")
	}
	for _, flag := range []string{"-gateway-quic", "-web-listen", "-demo-listen"} {
		_, e = parseServerConfig([]string{"-ephemeral-cert", "-management-only", "-admin-config", "admin.json", flag, "127.0.0.1:8444"})
		if e == nil {
			t.Fatalf("accepted %s", flag)
		}
	}
}
func TestManagementOnlyRoutes(t *testing.T) {
	h := managementPublicHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "https://example.org/lab/")
	for _, path := range []string{"/lab/mdm", "/mdm/v1/sync"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 204 {
			t.Fatalf("%s: %d", path, r.Code)
		}
	}
	for _, path := range []string{"/echo", "/tunnel", "/tunnel/bond", "/vpn-demo/", "/api/v1/devices/a/config"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 404 {
			t.Fatalf("%s: %d", path, r.Code)
		}
	}
}

func TestManagementOnlyProcess(t *testing.T) {
	if path := os.Getenv("QUIC_TEST_MANAGEMENT_CONFIG"); path != "" {
		os.Args = []string{os.Args[0], "-config", path}
		main()
		return
	}
	dir := t.TempDir()
	// Occupy the configured Echo socket: management must never try to bind it.
	udp, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	free := func() string {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		a := l.Addr().String()
		l.Close()
		return a
	}
	adminAddr, publicAddr := free(), free()
	write := func(name string, v any) string {
		p := filepath.Join(dir, name)
		b, _ := json.Marshal(v)
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	adminPath := write("admin.json", map[string]any{"management_only": true, "listen": adminAddr, "public_url": "https://" + publicAddr + "/lab/", "username": "test", "password": "test-password-at-least-12", "data_dir": filepath.Join(dir, "data")})
	path := write("server.json", map[string]any{"management_only": true, "ephemeral_cert": true, "listen": udp.LocalAddr().String(), "admin_config": adminPath, "https_listen": publicAddr})
	var out bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagementOnlyProcess$")
	cmd.Env = append(os.Environ(), "QUIC_TEST_MANAGEMENT_CONFIG="+path)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			t.Log(out.String())
		}
	}()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ready := false
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
		r, e := client.Get("http://" + adminAddr + "/mdm")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 303 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("management did not start")
	}
	for _, path := range []string{"/users/config", "/vless", "/enroll", "/echo/awg", "/capture", "/api/v1/devices/test/config"} {
		r, e := client.Post("http://"+adminAddr+path, "application/x-www-form-urlencoded", nil)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatalf("transport endpoint %s returned %d", path, r.StatusCode)
		}
	}
}
