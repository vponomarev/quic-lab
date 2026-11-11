//go:build linux && !android

package linuxcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/exitcontrol"
	"quiclab/internal/linuxclient"
	"strings"
	"testing"
)

func TestServerExitCommandsReadSelectAndDefault(t *testing.T) {
	selected := ""
	writes := 0
	revision := strings.Repeat("b", 64)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
			var change exitcontrol.Change
			if json.NewDecoder(r.Body).Decode(&change) != nil || change.Revision != revision {
				t.Error("missing expected revision")
			}
			selected = change.ID
		}
		json.NewEncoder(w).Encode(exitcontrol.View{Selected: selected, Effective: "west", Revision: revision, Choices: []exitcontrol.Metadata{{ID: "west", Name: "West", Protocol: "awg"}}})
	}))
	defer server.Close()
	cfg := linuxclient.Config{ServerControl: &exitcontrol.Config{ConfigURL: server.URL + "/api/v1/devices/device/config", DeviceID: "device", Token: strings.Repeat("a", 64)}}
	var out bytes.Buffer
	client := exitcontrol.Client{HTTP: server.Client()}
	if err := serverExitCommand(context.Background(), cfg, "", "", &out, client); err != nil || writes != 0 || strings.Contains(out.String(), cfg.ServerControl.Token) {
		t.Fatal(err, writes)
	}
	if err := serverExitCommand(context.Background(), cfg, "west", "", &out, client); err != nil || selected != "west" {
		t.Fatal(err, selected)
	}
	if err := serverExitCommand(context.Background(), cfg, "default", "", &out, client); err != nil || selected != "" {
		t.Fatal(err, selected)
	}
}
