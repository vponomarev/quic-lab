package admin

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVLESSSettingsRenderedDraft(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	c := config(t)
	w := NewWeb(c, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	page := call(h, "GET", "/vless", "", cookie)
	if page.Code != 200 || strings.Contains(page.Body.String(), s.vlessConfig().RealityPrivateKey) {
		t.Fatal("settings page invalid or private key exposed")
	}
	if strings.Count(page.Body.String(), `role="tabpanel"`) != 3 {
		t.Fatal("tab panels missing")
	}
	if strings.Contains(page.Body.String(), "<script>") || !strings.Contains(page.Body.String(), "vless-settings.js") {
		t.Fatal("editor violates script-src self CSP")
	}
	if path := os.Getenv("VLESS_UI_FIXTURE"); path != "" {
		if err := os.WriteFile(path, page.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVLESSBrowserHarness(t *testing.T) {
	path := os.Getenv("VLESS_BROWSER_HARNESS")
	if path == "" {
		t.Skip("explicit browser fixture only")
	}
	s, _ := managedVLESSFixture(t)
	c := config(t)
	server := httptest.NewUnstartedServer(nil)
	c.PublicURL = "https://" + server.Listener.Addr().String() + "/"
	w := NewWeb(c, s)
	server.Config.Handler = w.Handler()
	server.StartTLS()
	defer server.Close()
	data, _ := json.Marshal(map[string]string{"url": server.URL, "username": c.Username, "password": c.Password})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path + ".done"); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser harness timeout")
}
