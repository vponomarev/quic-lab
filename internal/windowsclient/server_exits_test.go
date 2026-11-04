package windowsclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/exitcontrol"
	"strings"
	"testing"
)

func TestServerControlSettingsSecretLifecycle(t *testing.T) {
	m, store, _ := managerFixture(t)
	c := draft(m)
	c.ReplaceServerControl = true
	c.Settings.ServerControl = &exitcontrol.Config{DeviceID: "device", ConfigURL: "https://control.example/api/v1/devices/device/config", Token: strings.Repeat("a", 64)}
	if err := m.Apply(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.Settings.ServerControl.Token = "changed"
	view := m.Read()
	raw, _ := json.Marshal(view)
	if !view.HasServerControl || strings.Contains(string(raw), strings.Repeat("a", 64)) || view.Settings.ServerControl.Token != "" {
		t.Fatal("credential exposed")
	}
	next := draft(m)
	next.Settings.Name = "rename"
	if err := m.Apply(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if store.doc.Settings.ServerControl.Token != strings.Repeat("a", 64) {
		t.Fatal("credential not retained")
	}
	next = draft(m)
	next.Settings.ServerControl.ConfigURL = "https://other.example/api/v1/devices/device/config"
	if err := m.Apply(context.Background(), next); err == nil {
		t.Fatal("credential redirected without replacement")
	}
	if strings.Contains(store.doc.Settings.GatewayJSON(), "update_token") {
		t.Fatal("control credentials entered data plane")
	}
	next = draft(m)
	next.ReplaceKey = true
	next.Settings.Key = "DIFFERENT-DEVICE"
	if err := m.Apply(context.Background(), next); err == nil {
		t.Fatal("control identity retained with replacement VPN identity")
	}
}

func TestDesktopServerExitControl(t *testing.T) {
	m, _, _ := managerFixture(t)
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/devices/device/server-exit" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("identity not retained")
		}
		if r.Method == "POST" {
			var c exitcontrol.Change
			if json.NewDecoder(r.Body).Decode(&c) != nil || c.ID != "west" || c.Revision != strings.Repeat("b", 64) {
				t.Error("incorrect change")
			}
		}
		json.NewEncoder(w).Encode(exitcontrol.View{Selected: "west", Effective: "west", Revision: strings.Repeat("b", 64), Choices: []exitcontrol.Metadata{{ID: "west", Name: "West", Protocol: "awg"}}})
	}))
	defer srv.Close()
	c := draft(m)
	c.ReplaceServerControl = true
	c.Settings.ServerControl = &exitcontrol.Config{DeviceID: "device", ConfigURL: srv.URL + "/api/v1/devices/device/config", Token: strings.Repeat("a", 64)}
	if err := m.Apply(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	m.exitClient = exitcontrol.Client{HTTP: srv.Client()}
	q := ServerExitRequest{ExpectedRevision: m.Read().Revision}
	if _, err := m.ServerExits(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	q.Change = &exitcontrol.Change{ID: "west", Revision: strings.Repeat("b", 64)}
	if _, err := m.ServerExits(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	q.ExpectedRevision--
	if _, err := m.ServerExits(context.Background(), q); err == nil {
		t.Fatal("stale profile selected")
	}
	q.ExpectedRevision++
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ServerExits(context.Background(), q); err == nil {
		t.Fatal("control used active tunnel")
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	m.Stop(context.Background())
}
