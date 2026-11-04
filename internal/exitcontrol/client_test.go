package exitcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlClientAuthSelectionAndRedirect(t *testing.T) {
	var redirect bool
	var selected string
	token := strings.Repeat("a", 64)
	revision := strings.Repeat("b", 64)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Path != "/api/v1/devices/device/server-exit" {
			t.Error("wrong authenticated endpoint")
		}
		if redirect {
			http.Redirect(w, r, "/elsewhere", 302)
			return
		}
		if r.Method == "POST" {
			var change Change
			if json.NewDecoder(r.Body).Decode(&change) != nil {
				t.Error("invalid change")
			}
			if change.Revision != revision {
				w.WriteHeader(409)
				return
			}
			selected = change.ID
		}
		json.NewEncoder(w).Encode(View{Revision: revision, Selected: selected, Effective: "west", Choices: []Metadata{{ID: "west", Name: "West", Protocol: "awg"}}})
	}))
	defer server.Close()
	cfg := Config{ConfigURL: server.URL + "/api/v1/devices/device/config", DeviceID: "device", Token: token}
	client := Client{HTTP: server.Client()}
	v, err := client.Fetch(context.Background(), cfg)
	if err != nil || len(v.Choices) != 1 {
		t.Fatal(v, err)
	}
	if _, err := client.Select(context.Background(), cfg, Change{ID: "west", Revision: revision}); err != nil || selected != "west" {
		t.Fatal(selected, err)
	}
	if _, err := client.Select(context.Background(), cfg, Change{ID: "west", Revision: strings.Repeat("c", 64)}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	redirect = true
	if _, err := client.Fetch(context.Background(), cfg); err == nil || strings.Contains(err.Error(), token) {
		t.Fatal("redirect followed or secret exposed", err)
	}
}
func TestControlMetadataCannotChangeDeviceOrEndpoint(t *testing.T) {
	base := Config{ConfigURL: "https://control.example/api/v1/devices/device/config", DeviceID: "device", Token: strings.Repeat("a", 64)}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"http://control.example/api/v1/devices/device/config", "https://control.example/api/v1/devices/other/config", base.ConfigURL + "?token=secret", "https://user@control.example/api/v1/devices/device/config", base.ConfigURL + "#fragment"} {
		c := base
		c.ConfigURL = url
		if c.Validate() == nil {
			t.Fatal("untrusted metadata accepted", url)
		}
	}
	c := base
	c.Token = ""
	if c.Validate() == nil {
		t.Fatal("partial authentication accepted")
	}
}
