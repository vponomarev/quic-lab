package admin

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"quiclab/internal/research"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ResearchPolicy controls server permission only; it never grants client consent.
type ResearchPolicy struct {
	Enabled        bool              `json:"enabled"`
	MaxBytes       int64             `json:"max_bytes"`
	RetentionHours int64             `json:"retention_hours"`
	Users          map[string]string `json:"users"`
}

func (w *Web) ResearchSettings() ResearchPolicy {
	w.researchMu.Lock()
	defer w.researchMu.Unlock()
	return w.researchSettingsLocked()
}
func (w *Web) researchSettingsLocked() ResearchPolicy {
	p := ResearchPolicy{RetentionHours: 168, Users: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(w.Config.DataDir, "research-policy.json"))
	if err == nil {
		if json.Unmarshal(data, &p) != nil {
			return ResearchPolicy{}
		}
	}
	return p
}
func (w *Web) ResearchAllowed(userID string) bool {
	p := w.ResearchSettings()
	if !p.Enabled || p.MaxBytes <= 0 || p.RetentionHours <= 0 {
		return false
	}
	switch p.Users[userID] {
	case "disabled":
		return false
	case "enabled":
		return true
	}
	return p.Enabled
}
func (w *Web) researchRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /research", w.researchPage)
	m.HandleFunc("GET /research/records", w.researchRecords)
	m.HandleFunc("GET /research/export", w.researchExport)
	m.HandleFunc("POST /research/settings", w.researchSettingsPost)
	// Preserve old standalone fixture URLs.

	m.HandleFunc("GET /lab/research", w.researchPage)
	m.HandleFunc("GET /lab/research/records", w.researchRecords)
	m.HandleFunc("GET /lab/research/export", w.researchExport)
	m.HandleFunc("POST /lab/research/settings", w.researchSettingsPost)
}
func researchQuery(v url.Values) (research.Query, error) {
	q := research.Query{SNI: v.Get("sni"), Route: v.Get("route"), ExitID: v.Get("exit"), Origin: v.Get("origin"), UserID: v.Get("user"), DeviceID: v.Get("device"), SessionID: v.Get("session"), Application: v.Get("application"), Protocol: v.Get("protocol"), Address: v.Get("address"), Domain: v.Get("domain"), Cursor: v.Get("cursor"), Limit: 100}
	if len(q.SNI) > 253 || strings.ContainsRune(q.SNI, 0) {
		return q, fmt.Errorf("invalid SNI")
	}
	if q.Route != "" && q.Route != "vpn" && q.Route != "direct" && q.Route != "unknown" {
		return q, fmt.Errorf("unknown route")
	}
	if len(q.ExitID) > 256 || strings.ContainsRune(q.ExitID, 0) {
		return q, fmt.Errorf("invalid exit")
	}
	switch q.Origin {
	case "", "legacy_unknown", "client", "server_gateway", "server_awg", "server_vless":
	default:
		return q, fmt.Errorf("unknown origin")
	}
	var err error
	if s := v.Get("from"); s != "" {
		q.From, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return q, fmt.Errorf("from must be RFC3339")
		}
	}
	if s := v.Get("to"); s != "" {
		q.To, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return q, fmt.Errorf("to must be RFC3339")
		}
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return q, fmt.Errorf("from must precede to")
	}
	if s := v.Get("limit"); s != "" {
		q.Limit, err = strconv.Atoi(s)
		if err != nil || q.Limit < 1 || q.Limit > 500 {
			return q, fmt.Errorf("limit must be 1..500")
		}
	}
	return q, nil
}
func (w *Web) researchRecords(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok {
		return
	}
	q, err := researchQuery(r.URL.Query())
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	store, openErr := w.researchStore()
	if openErr != nil || store == nil {
		http.Error(rw, "Research storage is not configured", 503)
		return
	}
	p, err := store.Query(r.Context(), q)
	if err != nil {
		http.Error(rw, "Research query failed", 400)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(p)
}
func (w *Web) researchExport(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok {
		return
	}
	q, err := researchQuery(r.URL.Query())
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	store, openErr := w.researchStore()
	if openErr != nil || store == nil {
		http.Error(rw, "Research storage is not configured", 503)
		return
	}
	q.Cursor = "" // Export the entire matching interval, not the visible page.
	rw.Header().Set("Content-Type", "application/x-ndjson")
	rw.Header().Set("Content-Disposition", `attachment; filename="research.jsonl"`)
	if err := store.Export(r.Context(), q, rw); err != nil {
		return
	}
}
func (w *Web) researchSettingsPost(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	w.researchMu.Lock()
	defer w.researchMu.Unlock()
	p := w.researchSettingsLocked()
	if p.Users == nil {
		p.Users = map[string]string{}
	}
	if id := r.Form.Get("user"); id != "" {
		exists := false
		for _, u := range w.Store.List() {
			if u.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			http.Error(rw, "Unknown user", 400)
			return
		}
		mode := r.Form.Get("mode")
		if mode != "inherit" && mode != "enabled" && mode != "disabled" {
			http.Error(rw, "Invalid mode", 400)
			return
		}
		if mode == "enabled" && (p.MaxBytes <= 0 || p.RetentionHours <= 0) {
			http.Error(rw, "Configure finite quotas first", 400)
			return
		}
		if mode == "inherit" {
			delete(p.Users, id)
		} else {
			p.Users[id] = mode
		}
	} else {
		enabled := r.Form.Get("enabled")
		if enabled != "true" && enabled != "false" {
			http.Error(rw, "Invalid enabled", 400)
			return
		}
		max, e1 := strconv.ParseInt(r.Form.Get("max_bytes"), 10, 64)
		hours, e2 := strconv.ParseInt(r.Form.Get("retention_hours"), 10, 64)
		if e1 != nil || e2 != nil || max <= 0 || hours <= 0 || hours > 87600 {
			http.Error(rw, "Positive disk quota and retention (1..87600 hours) required", 400)
			return
		}
		p.Enabled = enabled == "true"
		p.MaxBytes = max
		p.RetentionHours = hours
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err == nil {
		err = os.MkdirAll(w.Config.DataDir, 0700)
	}
	name := filepath.Join(w.Config.DataDir, "research-policy.json")
	if err == nil {
		err = os.WriteFile(name+".tmp", data, 0600)
	}
	if err == nil {
		err = os.Rename(name+".tmp", name)
	}
	if err != nil {
		http.Error(rw, "Cannot save research settings", 500)
		return
	}
	target := w.base + "research"
	if id := r.Form.Get("user"); id != "" {
		target += "?user=" + url.QueryEscape(id)
	}
	http.Redirect(rw, r, target, 303)
}

type researchRow struct {
	Record           research.Record
	Flow             research.FlowData
	Application, Raw string
	Events           []research.Record
	EventCount       int
}
type researchGap struct {
	Record                  research.Record
	UserID, Dropped, Reason string
}

func researchGapRow(rec research.Record) researchGap {
	row := researchGap{Record: rec, Dropped: "неизвестно", Reason: "причина не указана"}
	var d struct {
		UserID  string          `json:"user_id"`
		Dropped json.RawMessage `json:"dropped"`
		Reason  string          `json:"reason"`
	}
	if json.Unmarshal(rec.Data, &d) == nil {
		row.UserID = d.UserID
		if d.Reason != "" {
			row.Reason = d.Reason
		}
		var count string
		if json.Unmarshal(d.Dropped, &count) == nil {
			if _, err := strconv.ParseUint(count, 10, 64); err == nil {
				row.Dropped = count
			}
		}
	}
	return row
}

type researchAppStats struct {
	research.ApplicationStats
	Name, Source, URL, Up, Down string
	Selected                    bool
	GroupStart                  bool
	GroupSpan                   int
	GroupURL                    string
}

func researchBytes(n uint64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	for _, unit := range units {
		v /= 1024
		if v < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
	}
	return ""
}

type researchView struct {
	Applications     []researchAppStats
	SummaryPartial   bool
	ClearApplication string

	UserName                               string
	Gaps                                   []researchGap
	Base, CSRF, Export, Next, Tab, Message string
	Query                                  research.Query
	Policy                                 ResearchPolicy
	Rows                                   []researchRow
	Coverage                               research.Coverage
	Users                                  []User
}

func (w *Web) researchPage(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	q, err := researchQuery(r.URL.Query())
	if err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	v := researchView{Base: w.base, CSRF: s.CSRF, Query: q, Policy: w.ResearchSettings(), Users: w.Store.List(), Tab: r.URL.Query().Get("tab")}
	if q.UserID != "" {
		v.UserName = q.UserID // Keep retained history accessible after account deletion.
		v.Users = nil
		for _, user := range w.Store.List() {
			if user.ID == q.UserID {
				v.UserName = user.Name
				v.Users = []User{user}
				break
			}
		}
	}
	if v.Tab != "dns" && v.Tab != "events" {
		v.Tab = "flows"
	}
	params := r.URL.Query()
	params.Del("cursor")
	v.Export = w.base + "research/export?" + params.Encode()
	store, openErr := (*research.Store)(nil), error(nil)
	if q.UserID != "" {
		store, openErr = w.researchStore()
	}
	if q.UserID == "" {
		// The account overview does not query or render mixed traffic.
	} else if openErr != nil || store == nil {
		v.Message = "Хранилище недоступно или не настроено. Задайте квоту хранения; при ошибке проверьте состояние сервера."
	} else {
		rawQuery := q
		if v.Tab == "flows" {
			rawQuery.Cursor = ""
			rawQuery.Limit = 1
		}
		p, e := store.Query(r.Context(), rawQuery)
		if e != nil {
			http.Error(rw, "Research query failed", 400)
			return
		}
		summaryQuery := q
		summaryQuery.Application = ""
		summaryContext, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		summary, summaryErr := store.ApplicationSummary(summaryContext, summaryQuery)
		cancel()
		v.SummaryPartial = summary.Partial || summaryErr != nil
		links := r.URL.Query()
		links.Del("application")
		links.Del("cursor")
		v.ClearApplication = w.base + "research?" + links.Encode()
		for _, a := range summary.Applications {
			key := a.App.Attribution
			name := strings.Join(a.App.Labels, ", ")
			if len(a.App.Packages) > 0 {
				key = a.App.Packages[0]
				if name == "" {
					name = strings.Join(a.App.Packages, ", ")
				}
			}
			if key == "" {
				key = "unknown"
			}
			if name == "" {
				name = "Неизвестное приложение"
			}
			source := map[string]string{"client": "Клиент", "server_gateway": "Сервер QUIC/HTTPS", "server_awg": "Сервер AWG", "server_vless": "Сервер VLESS"}[a.Origin]
			if source == "" {
				source = "Старый формат"
			}
			links.Set("application", key)
			links.Set("route", a.ExitRef.Normalized().Route)
			links.Set("exit", a.ID)
			if a.Origin == "" {
				links.Set("origin", "legacy_unknown")
			} else {
				links.Set("origin", a.Origin)
			}
			selected := q.Application == key && (q.Origin == "" || q.Origin == a.Origin)
			v.Applications = append(v.Applications, researchAppStats{ApplicationStats: a, Name: name, Source: source, URL: w.base + "research?" + links.Encode(), Up: researchBytes(a.BytesUp), Down: researchBytes(a.BytesDown), Selected: selected})
		}
		groupResearchApplications(v.Applications, r.URL.Query(), w.base)
		v.Coverage = summary.Coverage
		if v.Tab == "flows" {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			connections, err := store.Connections(ctx, q)
			cancel()
			if err != nil {
				v.Message = "Не удалось прочитать сводку соединений."
			} else {
				v.Coverage = connections.Coverage
				v.SummaryPartial = v.SummaryPartial || connections.Partial
				for _, c := range connections.Rows {
					row := researchRow{Record: c.Record, Flow: c.Flow, Events: c.Events, EventCount: c.EventCount, Application: researchAppName(c.Flow.App)}
					v.Rows = append(v.Rows, row)
				}
				p.NextCursor = connections.NextCursor
			}
		}
		for _, rec := range p.Records {
			if rec.Type == "gap" {
				v.Gaps = append(v.Gaps, researchGapRow(rec))
				continue
			}
			if v.Tab == "flows" {
				continue
			}
			dns := rec.Type == "dns"
			if !dns && !strings.HasPrefix(rec.Type, "flow_") {
				continue
			}
			if (v.Tab == "dns") != dns {
				continue
			}
			row := researchRow{Record: rec, Raw: string(rec.Data), Application: "unknown"}
			if json.Unmarshal(rec.Data, &row.Flow) == nil {
				labels := row.Flow.App.Labels
				if len(labels) == 0 {
					labels = row.Flow.App.Packages
				}
				if len(labels) > 0 {
					row.Application = strings.Join(labels, ", ")
				}
				if row.Flow.App.Attribution != "" {
					row.Application += " · " + row.Flow.App.Attribution
				}
			}
			v.Rows = append(v.Rows, row)
		}
		if p.NextCursor != "" {
			params.Set("cursor", p.NextCursor)
			v.Next = w.base + "research?" + params.Encode()
		}
	}
	var b bytes.Buffer
	if err := researchTemplate.Execute(&b, v); err != nil {
		http.Error(rw, "Research rendering failed", 500)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Write(b.Bytes())
}

func (w *Web) researchStore() (*research.Store, error) {
	if w.ResearchOpen != nil {
		return w.ResearchOpen()
	}
	return w.Research, nil
}

//go:embed research.html
var researchHTML string
var researchTemplate = template.Must(template.New("research").Parse(researchHTML))

func researchAppName(a research.App) string {
	labels := a.Labels
	if len(labels) == 0 {
		labels = a.Packages
	}
	if len(labels) == 0 {
		return "Неизвестное приложение"
	}
	return strings.Join(labels, ", ")
}
func groupResearchApplications(rows []researchAppStats, params url.Values, base string) {
	key := func(a researchAppStats) string {
		p := append([]string(nil), a.App.Packages...)
		sort.Strings(p)
		return a.App.Attribution + "/" + strings.Join(p, ",")
	}
	sort.SliceStable(rows, func(i, j int) bool { return key(rows[i]) < key(rows[j]) })
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && key(rows[j]) == key(rows[i]) {
			j++
		}
		rows[i].GroupStart = true
		rows[i].GroupSpan = j - i
		p := url.Values{}
		for k, v := range params {
			p[k] = append([]string(nil), v...)
		}
		p.Del("cursor")
		app := rows[i].App.Attribution
		if len(rows[i].App.Packages) > 0 {
			app = rows[i].App.Packages[0]
		}
		p.Set("application", app)
		rows[i].GroupURL = base + "research?" + p.Encode()
		i = j
	}
}
