package admin

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"quiclab/internal/backup"
)

func (w *Web) SetBackupManager(m *backup.Manager) { w.backups = m }
func (w *Web) backupRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /backups", w.backupPage)
	m.HandleFunc("GET /backups/state", w.backupState)
	m.HandleFunc("POST /backups/create", w.backupCreate)
	m.HandleFunc("GET /backups/{id}/download", w.backupDownload)
	m.HandleFunc("POST /backups/{id}/delete", w.backupDelete)
}
func (w *Web) backupAvailable(rw http.ResponseWriter) bool {
	rw.Header().Set("Cache-Control", "no-store")
	if w.backups == nil {
		http.Error(rw, "Backup service unavailable", 503)
		return false
	}
	return true
}
func (w *Web) backupPage(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
	if !ok || !w.backupAvailable(rw) {
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	backupPageTemplate.Execute(rw, struct {
		Base, CSRF, Request string
		Jobs                []backup.Job
	}{w.base, s.CSRF, randomID(), w.backups.List()})
}
func (w *Web) backupState(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok || !w.backupAvailable(rw) {
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(w.backups.List())
}
func (w *Web) backupCreate(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok || !w.backupAvailable(rw) {
		return
	}
	password := r.Form.Get("password")
	if password == "" || password != r.Form.Get("repeat") {
		http.Error(rw, "Passwords must match", 400)
		return
	}
	if _, err := w.backups.Start(r.Context(), backup.Kind(r.Form.Get("type")), []byte(password), r.Form.Get("request_id")); err != nil {
		http.Error(rw, "Не удалось начать копирование. Проверьте тип, свободное место и лимит копий; при необходимости удалите старую копию.", 409)
		return
	}
	http.Redirect(rw, r, w.base+"backups", 303)
}
func (w *Web) backupDownload(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok || !w.backupAvailable(rw) {
		return
	}
	stream, j, err := w.backups.Open(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "Backup unavailable", 404)
		return
	}
	defer stream.Close()
	rw.Header().Set("Content-Type", "application/octet-stream")
	rw.Header().Set("Content-Disposition", `attachment; filename="quic-lab-`+j.ID+`.age"`)
	rw.Header().Set("Content-Length", fmt.Sprint(j.SizeBytes))
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	http.NewResponseController(rw).SetWriteDeadline(j.ExpiresAt)
	io.Copy(rw, stream)
}
func (w *Web) backupDelete(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok || !w.backupAvailable(rw) {
		return
	}
	if err := w.backups.Delete(r.PathValue("id")); err != nil {
		http.Error(rw, "Backup cannot be deleted", 409)
		return
	}
	http.Redirect(rw, r, w.base+"backups", 303)
}

var backupPageTemplate = template.Must(template.New("backups").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Резервные копии</title><style>
body{font:15px system-ui;background:#f4f7f9;color:#163348;margin:0}main{max-width:1400px;margin:auto;padding:24px}section{background:white;border:1px solid #dce6ec;border-radius:14px;padding:20px;margin:18px 0}form,.actions{display:flex;gap:12px;align-items:end;flex-wrap:wrap}label{display:grid;gap:5px}input,select{padding:10px;border:1px solid #bdcfd9;border-radius:6px}button,.button{background:#00877e;color:white;padding:10px 14px;border:0;border-radius:7px;text-decoration:none;cursor:pointer}a{color:#00877e}table{width:100%;border-collapse:collapse}td,th{text-align:left;padding:10px;border-bottom:1px solid #e6eef1}.danger{background:#a33a40}.muted{color:#62798c}small{display:block}.scroll{overflow:auto}</style><main>
<a href="{{.Base}}users">← Пользователи</a><h1>Резервные копии</h1><section><form method="post" action="{{.Base}}backups/create"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="request_id" value="{{.Request}}"><label>Состав<select name="type"><option value="config">Конфигурация и доступ</option><option value="full">Все данные приложения</option></select></label><label>Пароль<input name="password" type="password" autocomplete="new-password" required maxlength="1024"></label><label>Повтор пароля<input name="repeat" type="password" autocomplete="new-password" required maxlength="1024"></label><button>Создать копию</button></form><p>Конфигурация: пользователи, ключи, сертификаты и MDM-привязки. Полная копия дополнительно включает историю MDM, диагностику и захваты.</p><p class="muted">Скачайте копию вне VPS. Без пароля восстановление невозможно. Хранение на сервере — 24 часа, до 3 готовых копий. Восстановление — через CLI.</p></section><section class="scroll"><table><thead><tr><th>Создана</th><th>Тип</th><th>Состояние / размер</th><th>Хранится до</th><th>Действия</th></tr></thead><tbody>{{range .Jobs}}<tr data-job="{{.ID}}"><td>{{.CreatedAt.Format "02.01.2006 15:04 UTC"}}</td><td>{{if eq .Kind "config"}}Конфигурация{{else}}Полная{{end}}</td><td class="state">{{.State}}<small>{{.SizeBytes}} байт</small></td><td>{{.ExpiresAt.Format "02.01.2006 15:04 UTC"}}</td><td><div class="actions"><a class="download button" href="{{$.Base}}backups/{{.ID}}/download" {{if ne .State "ready"}}hidden{{end}}>Скачать</a><form method="post" action="{{$.Base}}backups/{{.ID}}/delete"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="danger" {{if or (eq .State "snapshot") (eq .State "packing")}}disabled{{end}}>Удалить</button></form></div></td></tr>{{else}}<tr><td colspan="5">Копий пока нет.</td></tr>{{end}}</tbody></table></section><script>
const labels={snapshot:'Получение снимка',packing:'Упаковка и шифрование',ready:'Готово',error:'Ошибка создания'};
async function update(){try{const r=await fetch('{{.Base}}backups/state',{cache:'no-store'});if(!r.ok)return;const jobs=await r.json();for(const j of jobs){const row=document.querySelector('[data-job="'+j.id+'"]');if(!row)continue;row.querySelector('.state').textContent=(labels[j.state]||j.state)+(j.state==='ready'?' · '+j.size_bytes+' байт':'');row.querySelector('.download').hidden=j.state!=='ready';row.querySelector('button').disabled=j.state==='snapshot'||j.state==='packing';}}catch(e){} } update();setInterval(update,2000);
</script></main></html>`))
