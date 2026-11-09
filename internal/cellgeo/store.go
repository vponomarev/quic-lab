package cellgeo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"quiclab/internal/backup"
	"regexp"
	"strconv"
	"time"
)

var errProvider = errors.New("provider unavailable")

type Cell struct {
	Technology string `json:"technology"`
	MCC        string `json:"mcc"`
	MNC        string `json:"mnc"`
	TAC        string `json:"tac"`
	CI         string `json:"ci"`
}

func (c Cell) Normalized() (Cell, error) {
	if c.Technology != "LTE" && c.Technology != "GSM" && c.Technology != "WCDMA" {
		return c, errors.New("unsupported radio")
	}
	fields := []*string{&c.MCC, &c.MNC, &c.TAC, &c.CI}
	limits := []uint64{999, 999, 65535, 268435455}
	if c.Technology == "GSM" {
		limits[3] = 65535
	}
	for i, p := range fields {
		if !digits.MatchString(*p) {
			return c, errors.New("invalid cell")
		}
		n, e := strconv.ParseUint(*p, 10, 64)
		if e != nil || n > limits[i] || i == 0 && n == 0 {
			return c, errors.New("invalid cell")
		}
		*p = strconv.FormatUint(n, 10)
	}
	return c, nil
}

var digits = regexp.MustCompile(`^[0-9]{1,10}$`)

func (c Cell) Key() string {
	return c.Technology + ":" + c.MCC + ":" + c.MNC + ":" + c.TAC + ":" + c.CI
}

type Position struct {
	Lat           float64 `json:"lat"`
	Lon           float64 `json:"lon"`
	Range         float64 `json:"range"`
	Source        string  `json:"source"`
	SourceVersion string  `json:"sourceVersion"`
	FetchedAt     string  `json:"fetchedAt"`
	HistoryID     int64   `json:"historyId"`
}
type Source struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	License  string `json:"license"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}
type Job struct {
	Cell     Cell
	Source   string
	Attempts int
}
type Stats struct {
	Cells   int `json:"cells"`
	Pending int `json:"pending"`
	Done    int `json:"done"`
	Errors  int `json:"errors"`
	History int `json:"history"`
}
type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	e = backup.Publish(path, func() error {
		_, e := db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL;
 CREATE TABLE IF NOT EXISTS sources(id TEXT PRIMARY KEY,name TEXT NOT NULL,version TEXT NOT NULL,url TEXT NOT NULL,license TEXT NOT NULL,priority INTEGER NOT NULL,enabled INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS cells(key TEXT PRIMARY KEY,identity TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS history(id INTEGER PRIMARY KEY AUTOINCREMENT,cell TEXT NOT NULL,source TEXT NOT NULL,source_version TEXT NOT NULL,fetched_at TEXT NOT NULL,status TEXT NOT NULL,lat REAL,lon REAL,radius REAL,error TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS history_lookup ON history(cell,source,status,id DESC);
 CREATE TABLE IF NOT EXISTS jobs(cell TEXT NOT NULL,source TEXT NOT NULL,priority INTEGER NOT NULL,state TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,next_at INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(cell,source));
 CREATE INDEX IF NOT EXISTS jobs_queue ON jobs(state,next_at,priority DESC);
 CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 PRAGMA user_version=1;`)
		return e
	})
	if e != nil {
		db.Close()
		return nil, e
	}
	e = s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR IGNORE INTO sources VALUES('mylnikov','Mylnikov open cell API','1.1/data=open','https://www.mylnikov.org/archives/1056','OpenCellID CC-BY-SA; openBmap ODbL; MLS CC0',100,1)`)
		return err
	})
	if e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) write(fn func(*sql.Tx) error) error {
	return backup.Publish(s.path, func() error {
		tx, e := s.db.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = fn(tx); e != nil {
			return e
		}
		return tx.Commit()
	})
}
func (s *Store) AddSource(v Source) error {
	if v.ID == "" || v.Name == "" || v.Version == "" {
		return errors.New("invalid source")
	}
	return s.write(func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO sources VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,version=excluded.version,url=excluded.url,license=excluded.license,priority=excluded.priority,enabled=excluded.enabled`, v.ID, v.Name, v.Version, v.URL, v.License, v.Priority, v.Enabled)
		return e
	})
}
func (s *Store) Sources() ([]Source, error) {
	rows, e := s.db.Query(`SELECT id,name,version,url,license,priority,enabled FROM sources ORDER BY priority DESC,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var v Source
		if e = rows.Scan(&v.ID, &v.Name, &v.Version, &v.URL, &v.License, &v.Priority, &v.Enabled); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Enqueue(c Cell, source string, priority int, force bool) error {
	return s.EnqueueMany([]Cell{c}, source, priority, force)
}
func (s *Store) EnqueueMany(cells []Cell, source string, priority int, force bool) error {
	// Avoid publication writes on map reads when every cell is already known.
	changed := make([]Cell, 0, len(cells))
	for _, c := range cells {
		v, e := c.Normalized()
		if e != nil {
			continue
		}
		var oldPriority int
		var state string
		e = s.db.QueryRow(`SELECT priority,state FROM jobs WHERE cell=? AND source=?`, v.Key(), source).Scan(&oldPriority, &state)
		if errors.Is(e, sql.ErrNoRows) || e == nil && (oldPriority < priority || force && state == "done") {
			changed = append(changed, v)
		} else if e != nil {
			return e
		}
	}
	if len(changed) == 0 {
		return nil
	}
	cells = changed
	return s.write(func(tx *sql.Tx) error {
		var enabled bool
		if e := tx.QueryRow(`SELECT enabled FROM sources WHERE id=?`, source).Scan(&enabled); e != nil {
			return e
		}
		if !enabled {
			return errors.New("disabled source")
		}
		for _, c := range cells {
			var e error
			c, e = c.Normalized()
			if e != nil {
				continue
			}
			raw, _ := json.Marshal(c)
			if _, e = tx.Exec(`INSERT OR IGNORE INTO cells VALUES(?,?)`, c.Key(), string(raw)); e != nil {
				return e
			}
			if _, e = tx.Exec(`INSERT INTO jobs(cell,source,priority,state) VALUES(?,?,?,'pending') ON CONFLICT(cell,source) DO UPDATE SET priority=excluded.priority WHERE priority<excluded.priority`, c.Key(), source, priority); e != nil {
				return e
			}
			if force {
				if _, e = tx.Exec(`UPDATE jobs SET state='pending',attempts=0,next_at=0 WHERE cell=? AND source=? AND state='done'`, c.Key(), source); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (s *Store) Next(now time.Time) (job *Job, err error) {
	var due int
	err = s.db.QueryRow(`SELECT count(*) FROM jobs j JOIN sources s ON s.id=j.source WHERE s.enabled=1 AND j.state!='done' AND j.next_at<=?`, now.Unix()).Scan(&due)
	if err != nil || due == 0 {
		return nil, err
	}
	err = s.write(func(tx *sql.Tx) error {
		var raw, key string
		j := Job{}
		e := tx.QueryRow(`SELECT j.cell,c.identity,j.source,j.attempts FROM jobs j JOIN cells c ON c.key=j.cell JOIN sources s ON s.id=j.source WHERE s.enabled=1 AND j.state!='done' AND j.next_at<=? ORDER BY j.priority DESC,j.cell LIMIT 1`, now.Unix()).Scan(&key, &raw, &j.Source, &j.Attempts)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if e = json.Unmarshal([]byte(raw), &j.Cell); e != nil {
			return e
		}
		_, e = tx.Exec(`UPDATE jobs SET state='running',next_at=? WHERE cell=? AND source=?`, now.Add(time.Minute).Unix(), key, j.Source)
		if e != nil {
			return e
		}
		job = &j
		return nil
	})
	return
}
func (s *Store) Complete(j Job, p *Position, providerErr error, now time.Time) error {
	if p != nil && (math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || math.IsNaN(p.Range) || math.IsInf(p.Range, 0) || math.Abs(p.Lat) > 90 || math.Abs(p.Lon) > 180 || p.Range <= 0) {
		return errors.New("invalid position")
	}
	return s.write(func(tx *sql.Tx) error {
		status := "not_found"
		errText := ""
		var lat, lon, radius any
		if providerErr != nil {
			status = "error"
			errText = "provider request failed"
		} else if p != nil {
			status = "found"
			lat = p.Lat
			lon = p.Lon
			radius = p.Range
		}
		_, e := tx.Exec(`INSERT INTO history(cell,source,source_version,fetched_at,status,lat,lon,radius,error) SELECT ?,id,version,?,?,?,?,?,? FROM sources WHERE id=?`, j.Cell.Key(), now.UTC().Format(time.RFC3339Nano), status, lat, lon, radius, errText, j.Source)
		if e != nil {
			return e
		}
		state := "done"
		next := int64(0)
		attempts := j.Attempts + 1
		if providerErr != nil {
			state = "retry"
			delay := time.Minute * time.Duration(1<<min(attempts, 10))
			if delay > 6*time.Hour {
				delay = 6 * time.Hour
			}
			next = now.Add(delay).Unix()
		}
		_, e = tx.Exec(`UPDATE jobs SET state=?,attempts=?,next_at=? WHERE cell=? AND source=?`, state, attempts, next, j.Cell.Key(), j.Source)
		return e
	})
}
func (s *Store) Lookup(c Cell) (*Position, error) {
	c, e := c.Normalized()
	if e != nil {
		return nil, e
	}
	var p Position
	e = s.db.QueryRow(`SELECT h.lat,h.lon,h.radius,h.source,h.source_version,h.fetched_at,h.id FROM history h JOIN sources s ON s.id=h.source WHERE h.cell=? AND h.status='found' AND s.enabled=1 AND h.id=(SELECT max(h2.id) FROM history h2 WHERE h2.cell=h.cell AND h2.source=h.source AND h2.status='found') ORDER BY s.priority DESC,h.radius ASC,h.id DESC LIMIT 1`, c.Key()).Scan(&p.Lat, &p.Lon, &p.Range, &p.Source, &p.SourceVersion, &p.FetchedAt, &p.HistoryID)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &p, nil
}
func (s *Store) Stats() (Stats, error) {
	v := Stats{}
	e := s.db.QueryRow(`SELECT (SELECT count(*) FROM cells),(SELECT count(*) FROM jobs WHERE state!='done'),(SELECT count(*) FROM jobs WHERE state='done'),(SELECT count(*) FROM jobs WHERE state='retry'),(SELECT count(*) FROM history)`).Scan(&v.Cells, &v.Pending, &v.Done, &v.Errors, &v.History)
	return v, e
}
func (s *Store) Pending(c Cell) (bool, error) {
	c, e := c.Normalized()
	if e != nil {
		return false, e
	}
	var n int
	e = s.db.QueryRow(`SELECT count(*) FROM jobs j JOIN sources s ON s.id=j.source WHERE cell=? AND state!='done' AND s.enabled=1`, c.Key()).Scan(&n)
	return n > 0, e
}
func (s *Store) SetMeta(key string, v any) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return s.write(func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO metadata VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(raw))
		return e
	})
}
func (s *Store) Meta(key string, v any) error {
	var raw string
	e := s.db.QueryRow(`SELECT value FROM metadata WHERE key=?`, key).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = json.Unmarshal([]byte(raw), v); e != nil {
		return fmt.Errorf("metadata: %w", e)
	}
	return nil
}

type HistoricalResult struct {
	ID            int64     `json:"id"`
	Source        string    `json:"source"`
	SourceVersion string    `json:"sourceVersion"`
	FetchedAt     string    `json:"fetchedAt"`
	Status        string    `json:"status"`
	Position      *Position `json:"position,omitempty"`
	Error         string    `json:"error,omitempty"`
}

func (s *Store) History(c Cell) ([]HistoricalResult, error) {
	c, e := c.Normalized()
	if e != nil {
		return nil, e
	}
	rows, e := s.db.Query(`SELECT id,source,source_version,fetched_at,status,lat,lon,radius,error FROM history WHERE cell=? ORDER BY id DESC LIMIT 100`, c.Key())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []HistoricalResult{}
	for rows.Next() {
		var v HistoricalResult
		var lat, lon, radius sql.NullFloat64
		if e = rows.Scan(&v.ID, &v.Source, &v.SourceVersion, &v.FetchedAt, &v.Status, &lat, &lon, &radius, &v.Error); e != nil {
			return nil, e
		}
		if v.Status == "found" {
			v.Position = &Position{Lat: lat.Float64, Lon: lon.Float64, Range: radius.Float64, Source: v.Source, SourceVersion: v.SourceVersion, FetchedAt: v.FetchedAt, HistoryID: v.ID}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
