package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"quiclab/internal/transit"
	"testing"
	"time"
)

func TestUplinkAuthenticationCacheAndFailedProbe(t *testing.T) {
	w := NewWeb(config(t), nil)
	if got := call(w.Handler(), "GET", "/users/uplink", "", nil); got.Code != 401 {
		t.Fatal("uplink exposed without auth")
	}
	w.sessions["test"] = session{Until: time.Now().Add(time.Hour)}
	w.uplinkState.value = uplinkStatus{Enabled: true, ExitIP: "192.0.2.9", Checked: time.Now()}
	r := call(w.Handler(), "GET", "/users/uplink", "", &http.Cookie{Name: "quiclab_admin", Value: "test"})
	var v uplinkStatus
	if json.Unmarshal(r.Body.Bytes(), &v) != nil || v.ExitIP != "192.0.2.9" {
		t.Fatal("shared cache not used")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v = measureUplink(ctx, &transit.Config{SourceIP: "192.0.2.44", Endpoint: "192.0.2.45:51820", DNS: "192.0.2.46", ProbeSocket: "missing-test-socket"})
	if v.Reachable || v.ExitIP != "" || v.RTT != nil || v.ProbeError == "" || v.ExitError == "" {
		t.Fatal("failed probe reported stale success", v)
	}
	w.Config.PublicURL = "https://lab.example:9443/lab/"
	w.ListenerBindings = map[string]string{"public": ":9443", "echo-quic": ":14433"}
	e := w.entryPoints()
	if e[0].Address != "lab.example:9443/lab/" || e[1].Binding != ":14433" {
		t.Fatal(e)
	}
}
