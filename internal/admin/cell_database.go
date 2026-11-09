package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"quiclab/internal/cellgeo"
	"quiclab/internal/mdm"
	"time"
)

func (w *Web) geoStore() (*cellgeo.Store, error) {
	w.cellDBOnce.Do(func() {
		w.cellDB, w.cellDBErr = cellgeo.Open(filepath.Join(w.Config.DataDir, "cellgeo", "cells.sqlite"))
	})
	return w.cellDB, w.cellDBErr
}
func geoCell(c mdm.CellRadio) cellgeo.Cell {
	return cellgeo.Cell{Technology: c.Technology, MCC: c.MCC, MNC: c.MNC, TAC: c.TAC, CI: c.CI}
}
func radioCell(c cellgeo.Cell) mdm.CellRadio {
	return mdm.CellRadio{Technology: c.Technology, MCC: c.MCC, MNC: c.MNC, TAC: c.TAC, CI: c.CI}
}
func (w *Web) StartCellWorker(ctx context.Context) error {
	db, e := w.geoStore()
	if e != nil {
		return e
	}
	go func() {
		defer db.Close()
		for ctx.Err() == nil {
			if e := w.discoverCells(ctx, db); e != nil {
				slog.Warn("cellgeo_discovery", "error", e)
			}
			j, e := db.Next(time.Now())
			if e != nil {
				slog.Warn("cellgeo_queue", "error", e)
			}
			if j != nil {
				var p *cellPosition
				if j.Source == "mylnikov" {
					p, e = w.cellGeo.fetch(ctx, radioCell(j.Cell), false)
				} else {
					e = errors.New("source adapter unavailable")
				}
				if ctx.Err() != nil {
					return
				}
				var v *cellgeo.Position
				if p != nil {
					v = &cellgeo.Position{Lat: p.Lat, Lon: p.Lon, Range: p.Range}
				}
				if err := db.Complete(*j, v, e, time.Now()); err != nil {
					slog.Warn("cellgeo_result", "error", err)
				}
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return nil
}

type cellBackfill struct {
	ID         string    `json:"device"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	State      string    `json:"state"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	Error      string    `json:"error,omitempty"`
}

func (w *Web) discoverCells(ctx context.Context, db *cellgeo.Store) error {
	var job cellBackfill
	if e := db.Meta("backfill", &job); e != nil {
		return e
	}
	if job.State != "pending" && job.State != "discovering" {
		return nil
	}
	job.State = "discovering"
	job.Error = ""
	if e := db.SetMeta("backfill", job); e != nil {
		return e
	}
	e := func() error {
		collect := func(id string, start, end time.Time, priority int) error {
			if e := ctx.Err(); e != nil {
				return e
			}
			rows, e := w.MDM.TelemetryRange(id, time.Now(), start, end, 25000)
			if e != nil {
				return e
			}
			unique := map[string]cellgeo.Cell{}
			for _, row := range rows {
				for _, c := range row.Sample.Cells {
					if !c.Registered || c.AgeMillis > 120000 {
						continue
					}
					v, e := geoCell(c).Normalized()
					if e == nil {
						unique[v.Key()] = v
					}
				}
			}
			cells := make([]cellgeo.Cell, 0, len(unique))
			for _, c := range unique {
				cells = append(cells, c)
			}
			return db.EnqueueMany(cells, "mylnikov", priority, true)
		}
		if e := collect(job.ID, job.Start, job.End, 1000); e != nil {
			return e
		}
		now := time.Now().UTC()
		for _, b := range w.MDM.Bindings() {
			days := b.Policy.RetentionDays
			if days == 0 {
				days = 90
			}
			for start := now.AddDate(0, 0, -days); start.Before(now); start = start.Add(24 * time.Hour) {
				end := start.Add(24 * time.Hour)
				if end.After(now) {
					end = now
				}
				if e := collect(b.Binding.ID, start, end, 10); e != nil {
					return e
				}
			}
		}
		return nil
	}()
	if e != nil {
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return e
		}
		job.State = "failed"
		job.Error = e.Error()
		_ = db.SetMeta("backfill", job)
		return e
	}
	job.State = "queued"
	job.FinishedAt = time.Now().UTC()
	return db.SetMeta("backfill", job)
}
func (w *Web) mdmCellPositions(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "no-store")
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	var cells []cellgeo.Cell
	if json.Unmarshal([]byte(r.Form.Get("cells")), &cells) != nil || len(cells) > 1000 {
		http.Error(rw, "Invalid cells (maximum 1000)", 400)
		return
	}
	db, e := w.geoStore()
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	// Invalid/unsupported inputs remain unresolved; never convert another radio into LTE.
	if e = db.EnqueueMany(cells, "mylnikov", 0, false); e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	positions := map[string]*cellgeo.Position{}
	pending := 0
	for _, c := range cells {
		normalized, e := c.Normalized()
		if e != nil {
			continue
		}
		p, e := db.Lookup(normalized)
		if e != nil {
			http.Error(rw, "Cell database unavailable", 503)
			return
		}
		positions[c.Key()] = p
		waiting, e := db.Pending(normalized)
		if e != nil {
			http.Error(rw, "Cell database unavailable", 503)
			return
		}
		if waiting {
			pending++
		}
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{"positions": positions, "pending": pending})
}
func (w *Web) mdmCellDBStatus(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok {
		return
	}
	db, e := w.geoStore()
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	stats, e := db.Stats()
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	sources, e := db.Sources()
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	var backfill cellBackfill
	if e = db.Meta("backfill", &backfill); e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{"stats": stats, "sources": sources, "backfill": backfill})
}
func (w *Web) mdmCellRescan(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	start, e1 := time.Parse(time.RFC3339, r.Form.Get("start"))
	end, e2 := time.Parse(time.RFC3339, r.Form.Get("end"))
	id := r.Form.Get("id")
	if e1 != nil || e2 != nil || !end.After(start) || end.Sub(start) > 7*24*time.Hour {
		http.Error(rw, "Invalid priority period", 400)
		return
	}
	found := false
	for _, b := range w.MDM.Bindings() {
		if b.Binding.ID == id {
			found = true
		}
	}
	if !found {
		http.NotFound(rw, r)
		return
	}
	db, e := w.geoStore()
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	w.cellScanMu.Lock()
	defer w.cellScanMu.Unlock()
	var old cellBackfill
	if e = db.Meta("backfill", &old); e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	if old.State == "pending" || old.State == "discovering" {
		http.Error(rw, "Discovery already running", 409)
		return
	}
	job := cellBackfill{ID: id, Start: start, End: end, State: "pending", StartedAt: time.Now().UTC()}
	if e = db.SetMeta("backfill", job); e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusAccepted)
	json.NewEncoder(rw).Encode(job)
}

func (w *Web) mdmCellHistory(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok {
		return
	}
	q := r.URL.Query()
	c := cellgeo.Cell{Technology: q.Get("technology"), MCC: q.Get("mcc"), MNC: q.Get("mnc"), TAC: q.Get("tac"), CI: q.Get("ci")}
	if _, e := c.Normalized(); e != nil {
		http.Error(rw, "Invalid cell", 400)
		return
	}
	db, e := w.geoStore()
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	history, e := db.History(c)
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(history)
}
