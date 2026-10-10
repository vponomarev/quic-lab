package admin

import (
	"encoding/base64"
	"encoding/json"
	"github.com/skip2/go-qrcode"
	"net/http"
	"net/url"
	"quiclab/internal/configtransfer"
	"quiclab/internal/editor"
	"strconv"
	"time"
)

func (w *Web) editorRoutes(m *http.ServeMux) {
	m.Handle("/api/editor/v1/", editor.NewHandler(w.Editor))
	m.HandleFunc("GET /editor", w.editorPage)
	m.HandleFunc("GET /editor/audit", w.editorAudit)
	m.HandleFunc("POST /editor/transfer", w.editorTransfer)
	for _, op := range []string{"create", "state", "submit", "revoke"} {
		m.HandleFunc("POST /editor/"+op, w.editorAction)
	}
}
func (w *Web) editorPage(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = mdmEditorTemplate.Execute(rw, mdmView{Base: w.base, CSRF: s.CSRF, Temporary: true})
}
func editorReply(rw http.ResponseWriter, value any, e error) {
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "application/json")
	if e != nil {
		code := 400
		switch e {
		case editor.ErrUnauthorized:
			code = 403
		case editor.ErrConflict:
			code = 409
		case editor.ErrAudit:
			code = 503
		case editor.ErrLimit:
			code = 429
		}
		http.Error(rw, http.StatusText(code), code)
		return
	}
	_ = json.NewEncoder(rw).Encode(value)
}
func (w *Web) editorAction(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	// Credentials are accepted only in POST bodies, never in URLs/access logs.
	if r.URL.RawQuery != "" {
		editorReply(rw, nil, editor.ErrInvalid)
		return
	}
	id, token := r.PostForm.Get("id"), r.PostForm.Get("token")
	switch r.URL.Path {
	case "/editor/create":
		minutes, e := strconv.Atoi(r.PostForm.Get("duration"))
		if e != nil {
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		ticket, e := w.Editor.Create(s.CSRF, time.Duration(minutes)*time.Minute)
		if e != nil {
			editorReply(rw, nil, e)
			return
		}
		fragment := url.Values{"server": {w.origin}, "id": {ticket.ID}, "token": {ticket.PairToken}, "minutes": {strconv.Itoa(minutes)}}.Encode()
		link := "quiclab://editor/pair#" + fragment
		png, e := qrcode.Encode(link, qrcode.Medium, 320)
		if e != nil {
			_ = w.Editor.Revoke(ticket.ID, s.CSRF, ticket.BrowserToken)
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		editorReply(rw, struct {
			editor.Ticket
			Link string `json:"link"`
			QR   string `json:"qr"`
		}{ticket, link, "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}, nil)
	case "/editor/state":
		state, e := w.Editor.Read(id, s.CSRF, token)
		editorReply(rw, state, e)
	case "/editor/submit":
		gen, e := strconv.ParseInt(r.PostForm.Get("generation"), 10, 64)
		if e != nil {
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		rev, e := w.Editor.Submit(id, s.CSRF, token, gen, json.RawMessage(r.PostForm.Get("document")))
		editorReply(rw, map[string]any{"revision": rev}, e)
	case "/editor/revoke":
		editorReply(rw, map[string]bool{"accepted": true}, w.Editor.Revoke(id, s.CSRF, token))
	}
}

func (w *Web) editorTransfer(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	if r.URL.RawQuery != "" {
		editorReply(rw, nil, editor.ErrInvalid)
		return
	}
	document := []byte(r.PostForm.Get("document"))
	switch r.PostForm.Get("action") {
	case "export":
		var selection configtransfer.Selection
		if configtransfer.DecodeOptions([]byte(r.PostForm.Get("selection")), &selection) != nil {
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		raw, e := configtransfer.Export(document, selection, false)
		if e != nil {
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		editorReply(rw, map[string]any{"bundle": json.RawMessage(raw)}, nil)
	case "import":
		raw := []byte(r.PostForm.Get("bundle"))
		var envelope struct {
			Credentials string `json:"credentials"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Credentials != "omitted" {
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		var options configtransfer.ImportOptions
		if configtransfer.DecodeOptions([]byte(r.PostForm.Get("options")), &options) != nil {
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		out, e := configtransfer.Prepare(document, raw, options)
		if e != nil {
			editorReply(rw, nil, editor.ErrInvalid)
			return
		}
		editorReply(rw, out, nil)
	default:
		editorReply(rw, nil, editor.ErrInvalid)
	}
}

func (w *Web) editorAudit(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok {
		return
	}
	out, e := w.Editor.AuditEvents(r.URL.Query().Get("id"), r.URL.Query().Get("day"))
	if e != nil {
		http.Error(rw, "Audit unavailable", 503)
		return
	}
	editorReply(rw, out, nil)
}
