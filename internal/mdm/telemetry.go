package mdm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	serverbackup "quiclab/internal/backup"
	"regexp"
	"sort"
	"time"
)

type TelemetryPolicy struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retentionDays"`
}
type WiFiRadio struct {
	SSID         string `json:"ssid,omitempty"`
	BSSID        string `json:"bssid,omitempty"`
	DBM          *int   `json:"dbm,omitempty"`
	FrequencyMHz int    `json:"frequencyMHz,omitempty"`
}
type CellRadio struct {
	Technology string `json:"technology"`
	Registered bool   `json:"registered"`
	MCC        string `json:"mcc,omitempty"`
	MNC        string `json:"mnc,omitempty"`
	CI         string `json:"ci,omitempty"`
	TAC        string `json:"tac,omitempty"`
	PCI        string `json:"pci,omitempty"`
	DBM        *int   `json:"dbm,omitempty"`
	AgeMillis  int64  `json:"ageMillis"`
}
type RadioSample struct {
	ID         string      `json:"id"`
	MeasuredAt time.Time   `json:"measuredAt"`
	DeviceName string      `json:"deviceName"`
	AppVersion string      `json:"appVersion"`
	WiFi       *WiFiRadio  `json:"wifi,omitempty"`
	Cells      []CellRadio `json:"cells"`
	WiFiStatus string      `json:"wifiStatus,omitempty"`
	CellStatus string      `json:"cellStatus,omitempty"`
}
type TelemetryBatch struct {
	Version   int           `json:"version"`
	BindingID string        `json:"bindingId"`
	Epoch     int64         `json:"epoch"`
	Records   []RadioSample `json:"records"`
	Dropped   int64         `json:"dropped"`
}
type TelemetryRecord struct {
	Sample     RadioSample `json:"sample"`
	ReceivedAt time.Time   `json:"receivedAt"`
}
type BindingSummary struct {
	UserID  string
	Dropped int64
	Binding Binding
	Policy  TelemetryPolicy
}

var numericRadio = regexp.MustCompile(`^[0-9]{1,16}$`)
var sampleID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,80}$`)
var radioMAC = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)

func effectiveTelemetry(p TelemetryPolicy) TelemetryPolicy {
	if p.RetentionDays == 0 {
		p.RetentionDays = 90
	}
	return p
}
func (s *Store) Bindings() []BindingSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BindingSummary, 0, len(s.state.Bindings))
	for _, b := range s.state.Bindings {
		out = append(out, BindingSummary{UserID: b.UserID, Dropped: b.TelemetryDropped, Binding: b.Binding, Policy: effectiveTelemetry(b.Telemetry)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Binding.ID < out[j].Binding.ID })
	return out
}
func (s *Store) SetTelemetryPolicy(id string, p TelemetryPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.RetentionDays < 1 || p.RetentionDays > 365 {
		return ErrInvalid
	}
	b, ok := s.state.Bindings[id]
	if !ok {
		return ErrUnauthorized
	}
	n := s.clone()
	b.Telemetry = p
	n.Bindings[id] = b
	return s.save(n)
}
func validRadioSample(r RadioSample, now time.Time) bool {
	if !sampleID.MatchString(r.ID) || r.MeasuredAt.IsZero() || r.MeasuredAt.Before(now.Add(-24*time.Hour)) ||
		r.MeasuredAt.After(now.Add(5*time.Minute)) || len(r.DeviceName) > 128 || len(r.AppVersion) > 64 ||
		len(r.WiFiStatus) > 128 || len(r.CellStatus) > 128 || len(r.Cells) > 32 {
		return false
	}
	dbmOK := func(p *int) bool { return p == nil || (*p >= -200 && *p <= 0) }
	if r.WiFi != nil {
		w := r.WiFi
		if len(w.SSID) > 128 || w.BSSID != "" && !radioMAC.MatchString(w.BSSID) || !dbmOK(w.DBM) || w.FrequencyMHz < 0 || w.FrequencyMHz > 100000 {
			return false
		}
	}
	for _, c := range r.Cells {
		switch c.Technology {
		case "GSM", "WCDMA", "LTE", "NR", "CDMA", "TD-SCDMA":
		default:
			return false
		}
		if !dbmOK(c.DBM) || c.AgeMillis < 0 {
			return false
		}
		for _, n := range []string{c.MCC, c.MNC, c.CI, c.TAC, c.PCI} {
			if n != "" && !numericRadio.MatchString(n) {
				return false
			}
		}
	}
	return true
}

// AppendTelemetry is independent of the control snapshot; a failed/partial batch may
// be retried with the same immutable sample IDs. Credentials never enter this journal.
func (s *Store) AppendTelemetry(batch TelemetryBatch, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		return ErrConflict
	}
	b, ok := s.state.Bindings[batch.BindingID]
	if !ok {
		return ErrUnauthorized
	}
	if batch.Epoch != b.Binding.Epoch {
		return ErrConflict
	}
	if !b.Binding.Active {
		return ErrPaused
	}
	if !b.Binding.GrantedRights.Telemetry || !b.Binding.GrantedRights.Geo {
		return ErrUnauthorized
	}
	if batch.Version != 1 || len(batch.Records) > 64 || batch.Dropped < 0 {
		return ErrInvalid
	}
	for _, r := range batch.Records {
		if !validRadioSample(r, now) {
			return ErrInvalid
		}
	}
	if batch.Dropped > b.TelemetryDropped {
		n := s.clone()
		b.TelemetryDropped = batch.Dropped
		n.Bindings[batch.BindingID] = b
		if e := s.save(n); e != nil {
			return e
		}
	}
	dir := filepath.Join(s.dir, "radio", b.Binding.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := s.pruneRadioLocked(dir, effectiveTelemetry(b.Telemetry).RetentionDays, now); e != nil {
		return e
	}
	groups := map[string][]RadioSample{}
	for _, r := range batch.Records {
		day := r.MeasuredAt.UTC().Format("2006-01-02")
		groups[day] = append(groups[day], r)
	}
	for day, in := range groups {
		path := filepath.Join(dir, day+".json")
		rows, e := readRadioDay(path)
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, r := range rows {
			seen[r.Sample.ID] = true
		}
		for _, r := range in {
			if !seen[r.ID] {
				rows = append(rows, TelemetryRecord{r, now})
				seen[r.ID] = true
			}
		}
		raw, e := json.Marshal(rows)
		if e != nil {
			return e
		}
		if len(raw) > 16<<20 {
			return ErrLimit
		}
		if e = writeRadioAtomic(path, raw); e != nil {
			return e
		}
	}
	return nil
}
func readRadioDay(path string) ([]TelemetryRecord, error) {
	info, e := os.Stat(path)
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, ErrLimit
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var rows []TelemetryRecord
	e = json.Unmarshal(raw, &rows)
	return rows, e
}
func writeRadioAtomic(path string, raw []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".radio-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = serverbackup.Replace(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (s *Store) pruneRadioLocked(dir string, days int, now time.Time) error {
	entries, e := os.ReadDir(dir)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, f := range entries {
		if f.IsDir() {
			continue
		}
		day, e := time.Parse("2006-01-02.json", f.Name())
		if e != nil {
			continue
		}
		if now.After(day.Add(time.Duration(days) * 24 * time.Hour)) {
			path := filepath.Join(dir, f.Name())
			rows, e := readRadioDay(path)
			if e != nil {
				return e
			}
			kept := make([]TelemetryRecord, 0, len(rows))
			for _, row := range rows {
				if !row.ReceivedAt.Before(now.Add(-time.Duration(days) * 24 * time.Hour)) {
					kept = append(kept, row)
				}
			}
			if len(kept) == len(rows) {
				continue
			}
			if len(kept) == 0 {
				if e = serverbackup.Remove(path); e != nil {
					return e
				}
			} else {
				raw, _ := json.Marshal(kept)
				if e = writeRadioAtomic(path, raw); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (s *Store) Telemetry(id string, now time.Time, limit int) ([]TelemetryRecord, error) {
	return s.TelemetryPage(id, now, time.Time{}, limit)
}
func (s *Store) TelemetryPage(id string, now, before time.Time, limit int) ([]TelemetryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return nil, ErrUnauthorized
	}
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalid
	}
	dir := filepath.Join(s.dir, "radio", id)
	days := effectiveTelemetry(b.Telemetry).RetentionDays
	if e := s.pruneRadioLocked(dir, days, now); e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(dir)
	if errors.Is(e, os.ErrNotExist) {
		return []TelemetryRecord{}, nil
	}
	if e != nil {
		return nil, e
	}
	var out []TelemetryRecord
	// Filenames sort chronologically; inspect newest days until the requested page is full.
	for i := len(entries) - 1; i >= 0; i-- {
		f := entries[i]
		if f.IsDir() {
			continue
		}
		if _, e := time.Parse("2006-01-02.json", f.Name()); e != nil {
			continue
		}
		rows, e := readRadioDay(filepath.Join(dir, f.Name()))
		if e != nil {
			return nil, e
		}
		for _, r := range rows {
			if !r.ReceivedAt.Before(now.Add(-time.Duration(days)*24*time.Hour)) && (before.IsZero() || r.Sample.MeasuredAt.Before(before)) {
				out = append(out, r)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Sample.MeasuredAt.After(out[j].Sample.MeasuredAt) })
		if len(out) >= limit {
			return out[:limit], nil
		}
	}
	if out == nil {
		out = []TelemetryRecord{}
	}
	return out, nil
}
func (s *Store) DeleteTelemetry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.Bindings[id]; !ok {
		return ErrUnauthorized
	}
	// id is an existing, server-generated binding, never a user-supplied path.
	return serverbackup.RemoveTree(filepath.Join(s.dir, "radio", id))
}

// PruneTelemetry also runs with no uploads or administrator visits.
func (s *Store) PruneTelemetry(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, b := range s.state.Bindings {
		if e := s.pruneRadioLocked(filepath.Join(s.dir, "radio", id), effectiveTelemetry(b.Telemetry).RetentionDays, now); e != nil {
			return e
		}
	}
	return nil
}
