package admin

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"quiclab/internal/research"
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
	m.HandleFunc("GET /lab/research", w.researchPage)
	m.HandleFunc("GET /lab/research/records", w.researchRecords)
	m.HandleFunc("GET /lab/research/export", w.researchExport)
	m.HandleFunc("POST /lab/research/settings", w.researchSettingsPost)
}
func researchQuery(v url.Values) (research.Query, error) {
	q := research.Query{Origin: v.Get("origin"), UserID: v.Get("user"), DeviceID: v.Get("device"), SessionID: v.Get("session"), Application: v.Get("application"), Protocol: v.Get("protocol"), Address: v.Get("address"), Domain: v.Get("domain"), Cursor: v.Get("cursor"), Limit: 100}
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
	http.Redirect(rw, r, w.base+"lab/research", 303)
}

type researchRow struct {
	Record           research.Record
	Flow             research.FlowData
	Application, Raw string
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

type researchView struct {
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
	if v.Tab != "dns" {
		v.Tab = "flows"
	}
	params := r.URL.Query()
	params.Del("cursor")
	v.Export = w.base + "lab/research/export?" + params.Encode()
	store, openErr := w.researchStore()
	if openErr != nil || store == nil {
		v.Message = "Хранилище недоступно или не настроено. Задайте квоту хранения; при ошибке проверьте состояние сервера."
	} else {
		p, e := store.Query(r.Context(), q)
		if e != nil {
			http.Error(rw, "Research query failed", 400)
			return
		}
		v.Coverage = p.Coverage
		for _, rec := range p.Records {
			if rec.Type == "gap" {
				v.Gaps = append(v.Gaps, researchGapRow(rec))
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
			v.Next = w.base + "lab/research?" + params.Encode()
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
