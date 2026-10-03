package vless

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// Opt-in acceptance of one explicitly supplied private profile. No credentials
// are stored in source, logged, fetched or enumerated by this test.
func TestLiveFixedVLESSProfile(t *testing.T) {
	path := os.Getenv("QUICLAB_VLESS_FIXTURE")
	if path == "" {
		t.Skip("private fixture not supplied")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read private fixture")
	}
	var config Config
	if json.Unmarshal(raw, &config) != nil {
		t.Fatal("invalid private fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	e, err := New(ctx, config, &net.Dialer{Timeout: 8 * time.Second})
	if err != nil {
		t.Fatal("private profile rejected")
	}
	defer e.Close()
	tr := &http.Transport{DialContext: e.DialContext, DisableKeepAlives: true, TLSHandshakeTimeout: 8 * time.Second}
	defer tr.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org/", nil)
	response, err := (&http.Client{Transport: tr, Timeout: 12 * time.Second}).Do(request)
	if err != nil {
		t.Fatal("private profile HTTPS failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 64))
	response.Body.Close()
	if readErr != nil || response.StatusCode != 200 || net.ParseIP(string(body)).To4() == nil {
		t.Fatal("private profile IPv4 response invalid")
	}
	udp, err := e.DialContext(ctx, "udp4", "1.1.1.1:53")
	if err != nil {
		t.Fatal("private profile UDP dial failed")
	}
	defer udp.Close()
	udp.SetDeadline(time.Now().Add(8 * time.Second))
	query := []byte{0x41, 0x62, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 101, 120, 97, 109, 112, 108, 101, 3, 99, 111, 109, 0, 0, 1, 0, 1}
	if _, err = udp.Write(query); err != nil {
		t.Fatal("private profile UDP write failed")
	}
	reply := make([]byte, 4096)
	n, err := udp.Read(reply)
	if err != nil || n < 12 || reply[0] != query[0] || reply[1] != query[1] || reply[2]&0x80 == 0 || reply[3]&15 != 0 {
		t.Fatal("private profile UDP DNS response failed")
	}
}
