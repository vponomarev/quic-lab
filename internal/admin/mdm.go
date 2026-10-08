package admin

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"quiclab/internal/mdm"
	"strconv"
	"time"
)

//go:embed mdm.html
var mdmHTML string
var mdmTemplate = template.Must(template.New("mdm").Funcs(template.FuncMap{
	"stamp": func(t time.Time) string { return t.UTC().Format("02.01.2006 15:04:05") + " UTC" },
	"signal": func(n *int) string {
		if n == nil {
			return "—"
		}
		return strconv.Itoa(*n) + " dBm"
	},
}).Parse(mdmHTML))

type mdmRow struct {
	ID, Name, Version, Last, UserID, UserName string
	Active, Consent                           bool
}
type mdmView struct {
	UserID                         string
	Users                          []User
	Dropped                        int64
	Base, CSRF, Link, Device, Next string
	QR                             template.URL
	Rows                           []mdmRow
	Records                        []mdm.TelemetryRecord
	Policy                         mdm.TelemetryPolicy
	Consent                        bool
}

func (w *Web) mdmReady(rw http.ResponseWriter) bool {
	if w.MDM == nil {
		http.Error(rw, "MDM unavailable", 503)
		return false
	}
	return true
}
func (w *Web) mdmPage(rw http.ResponseWriter, r *http.Request) {
	session, ok := w.authorized(rw, r, false)
	if !ok || !w.mdmReady(rw) {
		return
	}
	v := mdmView{Base: w.base, CSRF: session.CSRF, Device: r.URL.Query().Get("device"), Users: w.Store.List(), UserID: r.URL.Query().Get("user")}
	now := time.Now().UTC()
	if v.Device != "" {
		found := false
		for _, b := range w.MDM.Bindings() {
			if b.Binding.ID == v.Device {
				found = true
				v.UserID = b.UserID
				v.Policy = b.Policy
				v.Dropped = b.Dropped
				v.Consent = b.Binding.GrantedRights.Geo && b.Binding.GrantedRights.Telemetry
			}
		}
		if !found {
			http.NotFound(rw, r)
			return
		}
		before := time.Time{}
		if r.URL.Query().Get("before") != "" {
			var e error
			before, e = time.Parse(time.RFC3339Nano, r.URL.Query().Get("before"))
			if e != nil {
				http.Error(rw, "Invalid cursor", 400)
				return
			}
		}
		rows, e := w.MDM.TelemetryPage(v.Device, now, before, 200)
		if e != nil {
			http.Error(rw, "Telemetry unavailable", 503)
			return
		}
		v.Records = rows
		if r.URL.Query().Get("format") == "json" {
			rw.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(rw).Encode(rows)
			return
		}
		if len(rows) == 200 {
			v.Next = w.base + "mdm?device=" + url.QueryEscape(v.Device) + "&before=" + url.QueryEscape(rows[len(rows)-1].Sample.MeasuredAt.Format(time.RFC3339Nano))
		}
	} else {
		for _, b := range w.MDM.Bindings() {
			if v.UserID != "" && b.UserID != v.UserID {
				continue
			}
			row := mdmRow{UserID: b.UserID, ID: b.Binding.ID, Name: "Новое устройство", Active: b.Binding.Active, Consent: b.Binding.GrantedRights.Geo && b.Binding.GrantedRights.Telemetry}
			for _, u := range v.Users {
				if u.ID == b.UserID {
					row.UserName = u.Name
				}
			}
			records, e := w.MDM.Telemetry(b.Binding.ID, now, 1)
			if e != nil {
				http.Error(rw, "Telemetry unavailable", 503)
				return
			}
			if len(records) > 0 {
				sample := records[0].Sample
				row.Name = sample.DeviceName
				row.Version = sample.AppVersion
				row.Last = sample.MeasuredAt.Format("02.01.2006 15:04:05") + " UTC"
			}
			v.Rows = append(v.Rows, row)
		}
	}
	w.renderMDM(rw, v)
}
func (w *Web) renderMDM(rw http.ResponseWriter, v mdmView) {
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Referrer-Policy", "no-referrer")
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = mdmTemplate.Execute(rw, v)
}
func (w *Web) mdmInvite(rw http.ResponseWriter, r *http.Request) {
	session, ok := w.authorized(rw, r, true)
	if !ok || !w.mdmReady(rw) {
		return
	}
	w.userCaptureMu.Lock()
	defer w.userCaptureMu.Unlock()
	userID := r.Form.Get("userID")
	if !w.mdmUserExists(userID) {
		http.Error(rw, "Unknown user", 400)
		return
	}
	inv, e := w.MDM.CreateUserInvitation(time.Now().UTC(), mdm.Rights{Config: true, VPN: true, Telemetry: true, Geo: true, LANMode: "deny"}, userID)
	if e != nil {
		http.Error(rw, "Cannot create invitation", 503)
		return
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "endpoint": w.origin, "token": inv.Token, "requestedRights": inv.RequestedRights, "mode": "current"})
	link := "quiclab://mdm/enroll#" + base64.RawURLEncoding.EncodeToString(raw)
	qr, e := qrImage(link)
	if e != nil {
		http.Error(rw, "Cannot generate QR", 500)
		return
	}
	w.renderMDM(rw, mdmView{Base: w.base, CSRF: session.CSRF, Link: link, QR: qr})
}
func (w *Web) mdmPolicy(rw http.ResponseWriter, r *http.Request) {
	_, ok := w.authorized(rw, r, true)
	if !ok || !w.mdmReady(rw) {
		return
	}
	days, e := strconv.Atoi(r.Form.Get("days"))
	if e != nil {
		http.Error(rw, "Invalid retention", 400)
		return
	}
	e = w.MDM.SetTelemetryPolicy(r.Form.Get("id"), mdm.TelemetryPolicy{Enabled: r.Form.Get("enabled") == "on", RetentionDays: days})
	if e != nil {
		http.Error(rw, "Cannot set policy", 400)
		return
	}
	http.Redirect(rw, r, w.base+"mdm?device="+url.QueryEscape(r.Form.Get("id")), 303)
}
func (w *Web) mdmDeleteTelemetry(rw http.ResponseWriter, r *http.Request) {
	_, ok := w.authorized(rw, r, true)
	if !ok || !w.mdmReady(rw) {
		return
	}
	if r.Form.Get("confirm") != "yes" {
		http.Error(rw, "Confirmation required", 400)
		return
	}
	if e := w.MDM.DeleteTelemetry(r.Form.Get("id")); e != nil {
		http.Error(rw, "Cannot delete telemetry", 400)
		return
	}
	http.Redirect(rw, r, w.base+"mdm?device="+url.QueryEscape(r.Form.Get("id")), 303)
}

// Retention is independent of devices remaining online.
func (w *Web) StartMDMMaintenance(ctx context.Context) {
	go func() {
		timer := time.NewTicker(time.Hour)
		defer timer.Stop()
		for {
			if e := w.MDM.PruneTelemetry(time.Now().UTC()); e != nil {
				slog.Warn("mdm_retention_failed", "error", e)
			}
			if e := w.MDM.Prune(time.Now().UTC()); e != nil {
				slog.Warn("mdm_control_retention_failed", "error", e)
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
