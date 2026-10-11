package admin

import (
	"bytes"
	"encoding/json"
	"net/url"
	"path/filepath"
	"quiclab/internal/research"
	"strings"
	"testing"
	"time"
)

func TestResearchPermissions(t *testing.T) {
	cfg := config(t)
	s, err := OpenStore(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWeb(cfg, s)
	h := w.Handler()
	for _, path := range []string{"/lab/research", "/lab/research/records?device=other", "/lab/research/export?device=other"} {
		if r := call(h, "GET", path, "", nil); r.Code != 303 {
			t.Fatalf("unauthenticated %s: %d", path, r.Code)
		}
	}
	if r := call(h, "POST", "/lab/research/settings", "enabled=true", nil); r.Code != 303 {
		t.Fatal("unauthenticated mutation")
	}
	login := call(h, "POST", "/login", url.Values{"username": {cfg.Username}, "password": {cfg.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	if r := call(h, "POST", "/lab/research/settings", "enabled=true", cookie); r.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	csrf := w.sessions[cookie.Value].CSRF
	if w.ResearchAllowed("arbitrary") {
		t.Fatal("enabled by default")
	}
	form := url.Values{"csrf": {csrf}, "enabled": {"true"}, "max_bytes": {"0"}, "retention_hours": {"24"}, "client_consent": {"true"}}
	if r := call(h, "POST", "/lab/research/settings", form.Encode(), cookie); r.Code != 400 || w.ResearchAllowed("arbitrary") {
		t.Fatal("zero quota enabled")
	}
	form.Set("max_bytes", "1048576")
	if r := call(h, "POST", "/lab/research/settings", form.Encode(), cookie); r.Code != 303 {
		t.Fatal(r.Body.String())
	}
	if !NewWeb(cfg, s).ResearchAllowed("arbitrary") {
		t.Fatal("policy not persisted")
	}
	form.Set("enabled", "false")
	if r := call(h, "POST", "/lab/research/settings", form.Encode(), cookie); r.Code != 303 || w.ResearchAllowed("arbitrary") {
		t.Fatal("disable ineffective")
	}
	form = url.Values{"csrf": {csrf}, "user": {"foreign-device"}, "mode": {"enabled"}}
	if r := call(h, "POST", "/lab/research/settings", form.Encode(), cookie); r.Code != 400 {
		t.Fatal("unknown identity accepted")
	}
	if r := call(h, "GET", "/lab/research", "", cookie); r.Code != 200 || !strings.Contains(r.Body.String(), "полный захват") {
		t.Fatal(r.Body.String())
	}
}
func TestResearchFilters(t *testing.T) {
	v := url.Values{"user": {"u"}, "device": {"d"}, "session": {"s"}, "application": {"unknown"}, "protocol": {"tcp"}, "address": {"1.2.3.4"}, "domain": {"example.test"}, "from": {"2026-10-11T00:00:00Z"}, "to": {"2026-10-12T00:00:00Z"}, "cursor": {"123"}, "limit": {"20"}}
	q, err := researchQuery(v)
	if err != nil {
		t.Fatal(err)
	}
	if q.UserID != "u" || q.DeviceID != "d" || q.SessionID != "s" || q.Application != "unknown" || q.Protocol != "tcp" || q.Address != "1.2.3.4" || q.Domain != "example.test" || q.Limit != 20 || q.Cursor != "123" || q.From.IsZero() || q.To.IsZero() {
		t.Fatalf("lost filters: %+v", q)
	}
	for _, kv := range [][2]string{{"from", "bad"}, {"limit", "0"}, {"limit", "501"}, {"to", "2026-10-10T00:00:00Z"}} {
		bad := url.Values{}
		for k, x := range v {
			bad[k] = x
		}
		bad.Set(kv[0], kv[1])
		if _, e := researchQuery(bad); e == nil {
			t.Fatalf("accepted %v", kv)
		}
	}
}
func TestResearchHTMLData(t *testing.T) {
	attack := "<script>alert(1)</script>"
	view := researchView{Query: research.Query{UserID: "u"}, Policy: ResearchPolicy{Users: map[string]string{}}, Rows: []researchRow{{Flow: research.FlowData{Destination: attack}, Application: attack, Raw: attack}}, Tab: "dns"}
	var b bytes.Buffer
	if err := researchTemplate.Execute(&b, view); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), attack) || !strings.Contains(b.String(), "&lt;script&gt;") {
		t.Fatal("unsafe dynamic HTML")
	}
}

func TestResearchTableAndExportFilters(t *testing.T) {
	cfg := config(t)
	users, err := OpenStore(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	options := research.StoreOptions{Dir: filepath.Join(cfg.DataDir, "research"), MaxBytes: 1 << 20, Retention: 24 * time.Hour, QueueSize: 10}
	archive, err := research.OpenStore(options)
	if err != nil {
		t.Fatal(err)
	}
	for i, app := range []string{"wanted", "other"} {
		data, _ := json.Marshal(research.FlowData{UserID: "u", App: research.App{Packages: []string{app}, Labels: []string{app}, Attribution: "known"}, Protocol: "tcp", Destination: "1.2.3.4:443", Domain: "example.test", CounterKind: "delta", BytesUp: 42})
		if !archive.TryAppend(research.Record{SchemaVersion: research.SchemaVersion, Type: "flow_delta", ID: app, At: time.Now().UTC(), Flow: research.FlowKey{DeviceID: "d", SessionID: "s", FlowID: uint64(i + 1)}, Data: data}) {
			t.Fatal("append")
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err = research.OpenStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	w := NewWeb(cfg, users)
	w.Research = archive
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {cfg.Username}, "password": {cfg.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	filters := "?user=u&device=d&session=s&application=wanted&protocol=tcp&address=1.2.3.4&domain=example.test"
	for _, path := range []string{"/lab/research", "/lab/research/records", "/lab/research/export"} {
		r := call(h, "GET", path+filters, "", cookie)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "wanted") || strings.Contains(r.Body.String(), `"other"`) {
			t.Fatalf("%s filter mismatch: %d %s", path, r.Code, r.Body.String())
		}
	}
}

func TestResearchClientGapsVisibleWithFlowFilters(t *testing.T) {
	cfg := config(t)
	users, err := OpenStore(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	opts := research.StoreOptions{Dir: filepath.Join(cfg.DataDir, "research"), MaxBytes: 1 << 20, Retention: 24 * time.Hour, QueueSize: 16}
	archive, err := research.OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	cases := []struct {
		id, user, device, session, reason string
		at                                time.Time
	}{
		{"wanted", "u", "d", "s", "client_queue_or_delivery_loss", now},
		{"other-user", "another-user", "d", "s", "private-other-user", now},
		{"other-device", "u", "another-device", "s", "private-other-device", now},
		{"other-session", "u", "d", "another-session", "private-other-session", now},
		{"other-time", "u", "d", "s", "outside-selected-time", now.Add(-time.Hour)},
	}
	for _, c := range cases {
		data, _ := json.Marshal(map[string]string{"user_id": c.user, "dropped": "17", "reason": c.reason})
		if !archive.TryAppend(research.Record{SchemaVersion: research.SchemaVersion, Type: "gap", ID: c.id, At: c.at, Flow: research.FlowKey{DeviceID: c.device, SessionID: c.session}, Data: data}) {
			t.Fatal("append gap")
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err = research.OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	w := NewWeb(cfg, users)
	w.Research = archive
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {cfg.Username}, "password": {cfg.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	filters := url.Values{"user": {"u"}, "device": {"d"}, "session": {"s"}, "application": {"wanted-app"}, "protocol": {"tcp"}, "address": {"192.0.2.1"}, "domain": {"test.example"}, "from": {now.Add(-time.Minute).Format(time.RFC3339)}, "to": {now.Add(time.Minute).Format(time.RFC3339)}}
	for _, tab := range []string{"flows", "dns"} {
		filters.Set("tab", tab)
		for _, path := range []string{"/lab/research", "/lab/research/records", "/lab/research/export"} {
			r := call(h, "GET", path+"?"+filters.Encode(), "", cookie)
			if r.Code != 200 || !strings.Contains(r.Body.String(), "client_queue_or_delivery_loss") {
				t.Fatalf("%s/%s missing scoped gap: %d %s", path, tab, r.Code, r.Body.String())
			}
			for _, secret := range []string{"private-other-user", "private-other-device", "private-other-session", "outside-selected-time"} {
				if strings.Contains(r.Body.String(), secret) {
					t.Fatalf("%s leaked %s", path, secret)
				}
			}
			if path == "/lab/research" && (!strings.Contains(r.Body.String(), "Пропуски наблюдений") || !strings.Contains(r.Body.String(), "<strong>17</strong>")) {
				t.Fatal("gap not visibly explained")
			}
		}
	}
}
func TestResearchGapHTMLData(t *testing.T) {
	attack := "<script>gap</script>"
	raw, _ := json.Marshal(map[string]string{"reason": attack, "dropped": "9", "user_id": attack})
	rec := research.Record{At: time.Now(), Data: raw, Flow: research.FlowKey{DeviceID: attack, SessionID: attack}}
	v := researchView{Query: research.Query{UserID: "u"}, Gaps: []researchGap{researchGapRow(rec)}, Policy: ResearchPolicy{Users: map[string]string{}}}
	var b bytes.Buffer
	if err := researchTemplate.Execute(&b, v); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), attack) || !strings.Contains(b.String(), "&lt;script&gt;gap&lt;/script&gt;") {
		t.Fatal("gap HTML not escaped")
	}
}

func TestResearchMountedUnderLab(t *testing.T) {
	cfg := config(t)
	cfg.PublicURL = "https://lab.example/lab/"
	s, err := OpenStore(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWeb(cfg, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {cfg.Username}, "password": {cfg.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	r := call(h, "GET", "/research", "", cookie)
	if r.Code != 200 {
		t.Fatalf("canonical research route: %d", r.Code)
	}
	if strings.Contains(r.Body.String(), "/lab/lab/research") || !strings.Contains(r.Body.String(), "Выберите аккаунт") {
		t.Fatal("invalid mounted research links")
	}
	scoped := call(h, "GET", "/research?user=retained-account", "", cookie)
	if scoped.Code != 200 || !strings.Contains(scoped.Body.String(), `type="hidden" name="user" value="retained-account"`) || !strings.Contains(scoped.Body.String(), "/lab/research/export?user=retained-account") || strings.Contains(scoped.Body.String(), "Дисковая квота") {
		t.Fatal("account page loses scope or includes global controls")
	}
	if r := call(h, "GET", "/lab/research", "", cookie); r.Code != 200 {
		t.Fatal("legacy route removed")
	}
}
