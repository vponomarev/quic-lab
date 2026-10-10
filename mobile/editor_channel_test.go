package mobile

import (
	"net/http"
	"sync/atomic"
	"testing"
)

func TestEditorChannelRolePaths(t *testing.T) {
	var hits atomic.Int32
	c, b, s, _ := mdmFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/editor/v1/poll" {
			t.Error(r.URL.Path)
		}
		w.Write([]byte(`{"status":"ready"}`))
	}))
	if _, e := c.ExchangeEditor(s.URL+"/api/editor/v1/poll", "secret", "{}", b.Bind(nil, "cell")); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/mdm/v1/sync", "/api/editor/v1/create", "/api/editor/v1/poll?token=secret"} {
		if _, e := c.ExchangeEditor(s.URL+path, "secret", "{}", b.Bind(nil, "wifi")); e == nil {
			t.Fatal("accepted", path)
		}
	}
	if _, e := c.Exchange(s.URL+"/api/editor/v1/poll", "secret", "{}", b.Bind(nil, "wifi")); e == nil {
		t.Fatal("MDM path expanded")
	}
	if hits.Load() != 1 {
		t.Fatal("invalid path reached server")
	}
}
