package admin

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const diagnosticBodyLimit = 1 << 20

var diagnosticDiskMu sync.Mutex

type diagnosticRecord struct {
	AppVersion     string `json:"app_version,omitempty"`
	AppVersionCode int    `json:"app_version_code,omitempty"`
	ID             string `json:"id"`
	Time           int64  `json:"time"`
	Kind           string `json:"kind"`
	Text           string `json:"text"`
}
type diagnosticBatch struct {
	Version int                `json:"version"`
	Records []diagnosticRecord `json:"records"`
}
type diagnosticFile struct {
	path     string
	size     int64
	modified time.Time
}

func (w *Web) diagnosticLimits() (time.Duration, int64) {
	days := w.Config.DiagnosticsDays
	if days == 0 {
		days = 14
	}
	size := w.Config.DiagnosticsMiB
	if size == 0 {
		size = 500
	}
	return time.Duration(days) * 24 * time.Hour, int64(size) << 20
}
func (w *Web) diagnosticFiles() ([]diagnosticFile, error) {
	root := filepath.Join(w.Config.DataDir, "client-diagnostics")
	var out []diagnosticFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) && path == root {
			return nil
		}
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(path, ".json") {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		out = append(out, diagnosticFile{path, info.Size(), info.ModTime()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].modified.Before(out[j].modified) })
	return out, err
}
func (w *Web) pruneDiagnostics(incoming int64) error {
	files, e := w.diagnosticFiles()
	if e != nil {
		return e
	}
	age, limit := w.diagnosticLimits()
	var total int64
	for _, f := range files {
		total += f.size
	}
	count := len(files)
	for _, f := range files {
		if time.Since(f.modified) > age || total+incoming > limit || count >= 5000 {
			if e := os.Remove(f.path); e != nil {
				return e
			}
			total -= f.size
			count--
		}
	}
	if incoming > limit {
		return fmt.Errorf("batch exceeds disk quota")
	}
	return nil
}
func (w *Web) clientDiagnostics(rw http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if len(r.Header.Values("Authorization")) != 1 || !strings.HasPrefix(auth, "Bearer ") {
		updateHTTPError(rw, r, ErrUpdateUnauthorized)
		return
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	id := r.PathValue("id")
	if _, e := w.Store.AuthenticateUpdate(id, token); e != nil {
		updateHTTPError(rw, r, e)
		return
	}
	if w.Config.DataDir == "" {
		http.Error(rw, "Diagnostics unavailable", 503)
		return
	}
	var reader io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		z, e := gzip.NewReader(r.Body)
		if e != nil {
			http.Error(rw, "Invalid gzip", 400)
			return
		}
		defer z.Close()
		reader = z
	} else if r.Header.Get("Content-Encoding") != "" {
		http.Error(rw, "Unsupported encoding", 415)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(reader, diagnosticBodyLimit+1))
	if e != nil || len(raw) > diagnosticBodyLimit {
		http.Error(rw, "Batch too large or damaged", 413)
		return
	}
	var batch diagnosticBatch
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&batch) != nil || decoder.Decode(new(any)) != io.EOF || batch.Version != 1 || len(batch.Records) == 0 || len(batch.Records) > 128 {
		http.Error(rw, "Invalid batch", 400)
		return
	}
	seen := map[string]bool{}
	for _, v := range batch.Records {
		if len(v.AppVersion) > 128 || v.AppVersionCode < 0 || (v.AppVersionCode == 0) != (v.AppVersion == "") || len(v.ID) < 1 || len(v.ID) > 64 || seen[v.ID] || v.Time <= 0 || len(v.Text) > 2048 || (v.Kind != "event" && v.Kind != "summary" && v.Kind != "detail") {
			http.Error(rw, "Invalid record", 400)
			return
		}
		seen[v.ID] = true
	}
	// Device identity comes from authenticated URL, never from uploaded text.
	sum := sha256.Sum256(raw)
	batchID := hex.EncodeToString(sum[:])
	deviceHash := sha256.Sum256([]byte(id))
	directory := filepath.Join(w.Config.DataDir, "client-diagnostics", hex.EncodeToString(deviceHash[:]))
	path := filepath.Join(directory, batchID+".json")
	diagnosticDiskMu.Lock()
	defer diagnosticDiskMu.Unlock()
	if _, e = w.Store.AuthenticateUpdate(id, token); e != nil {
		updateHTTPError(rw, r, e)
		return
	}
	if existing, e := os.ReadFile(path); e == nil {
		if string(existing) != string(raw) {
			http.Error(rw, "Batch conflict", 409)
			return
		}
	} else {
		if !os.IsNotExist(e) {
			http.Error(rw, "Storage unavailable", 503)
			return
		}
		if e = w.pruneDiagnostics(int64(len(raw))); e != nil {
			http.Error(rw, "Storage unavailable", 503)
			return
		}
		if e = os.MkdirAll(directory, 0700); e != nil {
			http.Error(rw, "Storage unavailable", 503)
			return
		}
		f, e := os.CreateTemp(directory, ".pending-")
		if e != nil {
			http.Error(rw, "Storage unavailable", 503)
			return
		}
		temp := f.Name()
		defer os.Remove(temp)
		_, e = f.Write(raw)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e == nil {
			e = os.Rename(temp, path)
		}
		if e == nil {
			dir, err := os.Open(directory)
			if err == nil {
				err = dir.Sync()
				dir.Close()
			}
			e = err
		}
		if e != nil {
			http.Error(rw, "Storage unavailable", 503)
			return
		}
	}
	if e := saveDeviceVersion(directory, batch.Records); e != nil {
		http.Error(rw, "Storage unavailable", 503)
		return
	}
	// Repeat directory sync even on identical retries after a previous sync error.
	for _, dirPath := range []string{directory, filepath.Dir(directory), w.Config.DataDir} {
		var err error
		if w.diagnosticSync != nil {
			err = w.diagnosticSync(dirPath)
		} else {
			dir, openErr := os.Open(dirPath)
			err = openErr
			if err == nil {
				err = dir.Sync()
				dir.Close()
			}
		}
		if err != nil {
			http.Error(rw, "Storage unavailable", 503)
			return
		}
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{"accepted": true, "batch": batchID})
}
func (w *Web) clientDiagnosticsPage(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok {
		return
	}
	type row struct{ Time, Received, Kind, Text, Source string }
	type device struct{ ID, Name, UserID, UserName string }
	data := struct {
		Base, Selected, UserID, UserName, SelectedName string
		Devices                                        []device
		Rows                                           []row
	}{Base: w.base, Selected: r.URL.Query().Get("device"), UserID: r.URL.Query().Get("user")}
	w.Store.mu.Lock()
	validUser := data.UserID == ""
	if u, ok := w.Store.state.Users[data.UserID]; ok {
		validUser = true
		data.UserName = u.Name
	}
	validDevice := data.Selected == ""
	for id, d := range w.Store.state.Devices {
		if data.UserID != "" && d.UserID != data.UserID {
			continue
		}
		name := w.Store.state.Users[d.UserID].Name
		if name == "" {
			name = d.UserID
		}
		data.Devices = append(data.Devices, device{id, d.Name, d.UserID, name})
		if id == data.Selected {
			validDevice = true
			data.SelectedName = name + " · " + d.Name
		}
	}
	w.Store.mu.Unlock()
	if !validUser || !validDevice {
		http.NotFound(rw, r)
		return
	}
	sort.Slice(data.Devices, func(i, j int) bool {
		a, b := data.Devices[i], data.Devices[j]
		if a.UserName != b.UserName {
			return a.UserName < b.UserName
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	diagnosticDiskMu.Lock()
	e := w.pruneDiagnostics(0)
	if e == nil && data.Selected != "" {
		hash := sha256.Sum256([]byte(data.Selected))
		dir := filepath.Join(w.Config.DataDir, "client-diagnostics", hex.EncodeToString(hash[:]))
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Slice(files, func(i, j int) bool {
			a, _ := os.Stat(files[i])
			b, _ := os.Stat(files[j])
			return a.ModTime().After(b.ModTime())
		})
		seen := map[string]bool{}
		for _, file := range files {
			if len(data.Rows) >= 2000 {
				break
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			var batch diagnosticBatch
			if json.Unmarshal(raw, &batch) != nil {
				continue
			}
			info, _ := os.Stat(file)
			source := "client"
			if strings.HasPrefix(filepath.Base(file), "server-") {
				source = "server"
			}
			for _, v := range batch.Records {
				if !seen[v.ID] {
					seen[v.ID] = true
					data.Rows = append(data.Rows, row{time.UnixMilli(v.Time).UTC().Format(time.RFC3339), info.ModTime().UTC().Format(time.RFC3339), v.Kind, diagnosticVersionText(v), source})
				}
			}
		}
	}
	diagnosticDiskMu.Unlock()
	if e != nil {
		http.Error(rw, "Storage unavailable", 503)
		return
	}
	sort.SliceStable(data.Rows, func(i, j int) bool { return data.Rows[i].Time > data.Rows[j].Time })
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	template.Must(template.New("diagnostics").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><title>Журналы клиентов</title><link rel="stylesheet" href="{{.Base}}ui.css"><main style="padding:24px"><a href="{{.Base}}users">Пользователи</a><h1>Журналы клиентов{{if .UserID}} · {{.UserName}}{{end}}</h1>{{if .UserID}}<p><a href="{{.Base}}diagnostics/clients">Все пользователи</a></p>{{end}}{{if .SelectedName}}<h2>{{.SelectedName}}</h2>{{end}}<p>Время UTC. Последние 2000 событий выбранного устройства; время приёма помогает сопоставить задержанную доставку. Содержимое прислал клиент.</p><form method="get">{{if .UserID}}<input type="hidden" name="user" value="{{.UserID}}">{{end}}<select name="device"><option value="">Выберите устройство</option>{{range .Devices}}<option value="{{.ID}}" {{if eq $.Selected .ID}}selected{{end}}>{{.UserName}} · {{.Name}} · {{.ID}}</option>{{end}}</select><button>Показать</button></form>{{if .UserID}}<h2>Устройства пользователя</h2><ul>{{range .Devices}}<li><a href="{{$.Base}}diagnostics/clients?user={{$.UserID}}&amp;device={{.ID}}">{{.Name}}</a> <small>{{.ID}}</small></li>{{else}}<li>Устройств пока нет.</li>{{end}}</ul>{{end}}<table><thead><tr><th>Событие UTC</th><th>Принято UTC</th><th>Источник</th><th>Тип</th><th>Данные</th></tr></thead><tbody>{{range .Rows}}<tr><td>{{.Time}}</td><td>{{.Received}}</td><td>{{.Source}}</td><td>{{.Kind}}</td><td style="white-space:pre-wrap;overflow-wrap:anywhere">{{.Text}}</td></tr>{{end}}</tbody></table></main></html>`)).Execute(rw, data)
}

func (w *Web) StartDiagnosticsMaintenance(ctx context.Context) {
	type item struct{ id, text string }
	queue := make(chan item, 128)
	w.Store.mu.Lock()
	w.Store.diagnosticEvent = func(id, text string) {
		select {
		case queue <- item{id, text}:
		default:
		}
	}
	w.Store.mu.Unlock()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		defer func() { w.Store.mu.Lock(); w.Store.diagnosticEvent = nil; w.Store.mu.Unlock() }()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				diagnosticDiskMu.Lock()
				w.pruneDiagnostics(0)
				diagnosticDiskMu.Unlock()
			case event := <-queue:
				w.recordServerDiagnostic(event.id, event.text)
			}
		}
	}()
}
func (w *Web) recordServerDiagnostic(device, text string) {
	batch := diagnosticBatch{Version: 1, Records: []diagnosticRecord{{ID: randomID(), Time: time.Now().UnixMilli(), Kind: "event", Text: text}}}
	raw, e := json.Marshal(batch)
	if e != nil {
		return
	}
	diagnosticDiskMu.Lock()
	defer diagnosticDiskMu.Unlock()
	if w.Config.DataDir == "" || w.pruneDiagnostics(int64(len(raw))) != nil {
		return
	}
	hash := sha256.Sum256([]byte(device))
	dir := filepath.Join(w.Config.DataDir, "client-diagnostics", hex.EncodeToString(hash[:]))
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	f, e := os.CreateTemp(dir, ".pending-")
	if e != nil {
		return
	}
	name := f.Name()
	defer os.Remove(name)
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e == nil {
		os.Rename(name, filepath.Join(dir, "server-"+batch.Records[0].ID+".json"))
	}
}

func (w *Web) deviceReportTimes(users []User) map[string]string {
	result := make(map[string]string)
	if len(users) == 0 || w.Config.DataDir == "" {
		return result
	}
	diagnosticDiskMu.Lock()
	defer diagnosticDiskMu.Unlock()
	files, err := w.diagnosticFiles()
	if err != nil {
		return result
	}
	byHash := make(map[string]time.Time)
	for _, f := range files {
		if strings.HasPrefix(filepath.Base(f.path), "server-") {
			continue
		}
		hash := filepath.Base(filepath.Dir(f.path))
		if f.modified.After(byHash[hash]) {
			byHash[hash] = f.modified
		}
	}
	for _, u := range users {
		for _, d := range u.Devices {
			hash := sha256.Sum256([]byte(d.ID))
			if stamp := byHash[hex.EncodeToString(hash[:])]; !stamp.IsZero() {
				result[d.ID] = stamp.UTC().Format(time.RFC3339)
			}
		}
	}
	return result
}

type deviceVersion struct {
	Name string `json:"name"`
	Code int    `json:"code"`
	Time int64  `json:"time"`
}

func diagnosticVersionText(v diagnosticRecord) string {
	if v.AppVersion == "" {
		return v.Text
	}
	return fmt.Sprintf("[App %s (%d)] %s", v.AppVersion, v.AppVersionCode, v.Text)
}
func saveDeviceVersion(directory string, records []diagnosticRecord) error {
	path := filepath.Join(directory, "version.meta")
	var latest deviceVersion
	raw, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(raw, &latest); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	changed := false
	for _, v := range records {
		if v.AppVersionCode > 0 && v.Time > latest.Time {
			latest = deviceVersion{v.AppVersion, v.AppVersionCode, v.Time}
			changed = true
		}
	}
	if !changed {
		return nil
	}
	raw, err = json.Marshal(latest)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".version-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (w *Web) deviceVersions(users []User) (map[string]string, map[string]int) {
	result := map[string]string{}
	counts := map[string]int{}
	diagnosticDiskMu.Lock()
	defer diagnosticDiskMu.Unlock()
	for _, u := range users {
		for _, d := range u.Devices {
			label := "Неизвестна"
			hash := sha256.Sum256([]byte(d.ID))
			raw, err := os.ReadFile(filepath.Join(w.Config.DataDir, "client-diagnostics", hex.EncodeToString(hash[:]), "version.meta"))
			var v deviceVersion
			if err == nil && json.Unmarshal(raw, &v) == nil && v.Code > 0 {
				label = fmt.Sprintf("%s (%d)", v.Name, v.Code)
			}
			result[d.ID] = label
			if !d.Disabled && !d.Legacy {
				counts[label]++
			}
		}
	}
	return result, counts
}
