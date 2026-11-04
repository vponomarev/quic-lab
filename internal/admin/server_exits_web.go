package admin

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"quiclab/internal/serverexit"
	"strings"
)

func (w *Web) serverExitRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /server-exits", w.serverExitPage)
	m.HandleFunc("POST /server-exits/policy", w.serverExitPolicy)
	m.HandleFunc("GET /api/v1/devices/{id}/server-exit", w.deviceServerExit)
	m.HandleFunc("POST /api/v1/devices/{id}/server-exit", w.deviceServerExit)
}

func exitHTTPError(rw http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	message := "Server exit change is not acknowledged; refresh before retrying"
	switch {
	case errors.Is(err, ErrUpdateUnauthorized):
		code, message = 401, "Device authentication required"
	case errors.Is(err, ErrUpdateForbidden), errors.Is(err, serverexit.ErrDenied):
		code, message = 403, "Server exit unavailable or not permitted"
	case errors.Is(err, ErrServerExitRevision):
		code, message = 409, ErrServerExitRevision.Error()
	case errors.Is(err, serverexit.ErrConfig):
		code, message = 400, "Invalid server exit policy"
	}
	http.Error(rw, message, code)
}

func (w *Web) deviceServerExit(rw http.ResponseWriter, r *http.Request) {
	token := ""
	if values := r.Header.Values("Authorization"); len(values) == 1 && strings.HasPrefix(values[0], "Bearer ") {
		token = strings.TrimPrefix(values[0], "Bearer ")
	}
	id := r.PathValue("id")
	v, err := w.Store.DeviceServerExits(id, token)
	if err != nil {
		exitHTTPError(rw, err)
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			ID       string `json:"server_exit_id"`
			Revision string `json:"revision"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			http.Error(rw, "Invalid request", 400)
			return
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(rw, "Invalid request", 400)
			return
		}
		v, err = w.Store.SelectServerExit(id, token, body.ID, body.Revision)
		if err != nil {
			exitHTTPError(rw, err)
			return
		}
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(v)
}

type serverExitRow struct {
	User User
	View ServerExitPolicyView
}

func (w *Web) serverExitPage(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	rows := []serverExitRow{}
	for _, u := range w.Store.List() {
		v, err := w.Store.UserServerExitPolicy(u.ID)
		if err == nil {
			rows = append(rows, serverExitRow{u, v})
		}
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	serverExitTemplate.Execute(rw, struct {
		Base, CSRF string
		Rows       []serverExitRow
	}{w.base, s.CSRF, rows})
}
func (w *Web) serverExitPolicy(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	var p *serverexit.Policy
	switch r.Form.Get("mode") {
	case "legacy":
	case "managed":
		p = &serverexit.Policy{Allowed: r.Form["allowed"], Default: r.Form.Get("default")}
	default:
		http.Error(rw, "Invalid policy mode", 400)
		return
	}
	if err := w.Store.SetServerExitPolicy(r.Form.Get("user_id"), p, r.Form.Get("revision")); err != nil {
		exitHTTPError(rw, err)
		return
	}
	http.Redirect(rw, r, w.base+"server-exits", http.StatusSeeOther)
}

var serverExitTemplate = template.Must(template.New("server-exits").Funcs(template.FuncMap{
	"allowed": func(p *serverexit.Policy, id string) bool {
		if p != nil {
			for _, v := range p.Allowed {
				if v == id {
					return true
				}
			}
		}
		return false
	},
	"defaultExit": func(p *serverexit.Policy) string {
		if p == nil {
			return ""
		}
		return p.Default
	},
}).Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Выходы сервера</title><link rel="stylesheet" href="{{.Base}}ui.css">
<body><main><p><a href="{{.Base}}users">← Пользователи</a></p><h1>Выходы сервера</h1>
<p>Администратор разрешает выходы и назначает выход по умолчанию. Каждое устройство выбирает один из разрешённых выходов. Смена прав завершает текущие подключения.</p>
<p>Список AWG-выходов загружается из конфигурации сервера. Ключи клиентам не передаются. Пустой список разрешений в управляемом режиме блокирует выход.</p>
{{range .Rows}}{{$row := .}}<section><h2>{{.User.Name}}</h2><form method="post" action="{{$.Base}}server-exits/policy">
<input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="user_id" value="{{.User.ID}}"><input type="hidden" name="revision" value="{{.View.Revision}}">
<label>Режим <select name="mode"><option value="legacy" {{if not .View.Policy}}selected{{end}}>Прежний выход сервера</option><option value="managed" {{if .View.Policy}}selected{{end}}>Разрешённые выходы</option></select></label>
<fieldset><legend>Разрешённые выходы</legend>{{range .View.Choices}}<label><input type="checkbox" name="allowed" value="{{.ID}}" {{if allowed $row.View.Policy .ID}}checked{{end}}>{{.Name}} ({{.Protocol}}){{if .Disabled}} — отключён{{end}}</label> {{else}}<p>Выходы ещё не настроены.</p>{{end}}</fieldset>
<label>По умолчанию <select name="default"><option value="">Нет — блокировать</option>{{range .View.Choices}}<option value="{{.ID}}" {{if eq (defaultExit $row.View.Policy) .ID}}selected{{end}}>{{.Name}}</option>{{end}}</select></label>
<button type="submit">Сохранить права</button></form>
<ul>{{range .User.Devices}}<li>{{.Name}}: {{if .ServerExitID}}{{.ServerExitID}}{{else}}по умолчанию{{end}}{{if .Disabled}} — устройство отключено{{end}}</li>{{end}}</ul></section>{{else}}<p>Нет пользователей.</p>{{end}}
</main></body></html>`))
