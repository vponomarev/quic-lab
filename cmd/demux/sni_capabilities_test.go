package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/protocol"
	"testing"
)

func TestDemuxControlBeforeData(t *testing.T) {
	server := httptest.NewUnstartedServer(demuxHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("anonymous data admitted") }), protocol.Capabilities{ControlVersion: 1, DataVersion: 9}))
	server.TLS = &tls.Config{ClientAuth: tls.VerifyClientCertIfGiven, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.Capabilities
	err = json.NewDecoder(response.Body).Decode(&got)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || got.DataVersion != 9 {
		t.Fatalf("%d %+v", response.StatusCode, got)
	}
	response, err = server.Client().Get(server.URL + "/tunnel/bond")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous data status %d", response.StatusCode)
	}
}
