package research

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type StoreOptions struct {
	Dir       string
	MaxBytes  int64
	Retention time.Duration
	QueueSize int
}
type Query struct {
	UserID      string    `json:"user_id,omitempty"`
	DeviceID    string    `json:"device_id,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Application string    `json:"application,omitempty"`
	Protocol    string    `json:"protocol,omitempty"`
	Address     string    `json:"address,omitempty"`
	Domain      string    `json:"domain,omitempty"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Cursor      string    `json:"cursor,omitempty"`
	Limit       int       `json:"limit,omitempty"`
}
type Coverage struct {
	Incomplete   bool      `json:"incomplete"`
	Reason       string    `json:"reason,omitempty"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	Dropped      uint64    `json:"dropped,string"`
	RecoveryGaps uint64    `json:"recovery_gaps,string"`
	Truncated    bool      `json:"truncated"`
}
type Page struct {
	Records    []Record `json:"records"`
	NextCursor string   `json:"next_cursor,omitempty"`
	Coverage   Coverage `json:"coverage"`
}
type segment struct {
	path string
	size int64
	at   time.Time
	ids  []string
}
type queued struct {
	r Record
	b []byte
}
type Store struct {
	opts              StoreOptions
	queue             chan queued
	done              chan struct{}
	lifecycle         sync.RWMutex
	closed            bool
	mu                sync.Mutex
	segments          []segment
	ids               map[string]bool
	file              *os.File
	seq               uint64
	total             int64
	dropped, recovery atomic.Uint64
	truncated         atomic.Bool
	lastErr           error
}

func OpenStore(o StoreOptions) (*Store, error) {
	if o.Dir == "" || o.MaxBytes <= 0 {
		return nil, errors.New("research directory and finite positive quota required")
	}
	if o.Retention == 0 {
		o.Retention = 7 * 24 * time.Hour
	}
	if o.Retention < 0 {
		return nil, errors.New("invalid retention")
	}
	if o.QueueSize == 0 {
		o.QueueSize = 256
	}
	if o.QueueSize < 1 || o.QueueSize > 4096 {
		return nil, errors.New("invalid queue size")
	}
	if e := os.MkdirAll(o.Dir, 0700); e != nil {
		return nil, e
	}
	s := &Store{opts: o, queue: make(chan queued, o.QueueSize), done: make(chan struct{}), ids: map[string]bool{}}
	paths, e := filepath.Glob(filepath.Join(o.Dir, "research-*.jsonl"))
	if e != nil {
		return nil, e
	}
	sort.Strings(paths)
	for _, p := range paths {
		info, e := os.Stat(p)
		if e != nil {
			return nil, e
		}
		n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "research-"), ".jsonl"), 10, 64)
		if n >= s.seq {
			s.seq = n + 1
		}
		seg := segment{path: p, at: info.ModTime()}
		f, e := os.OpenFile(p, os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		rd := bufio.NewReaderSize(f, MaxRecordBytes)
		for {
			line, e := rd.ReadSlice('\n')
			if e == io.EOF && len(line) == 0 {
				break
			}
			if e != nil {
				s.recovery.Add(1)
				if e = f.Truncate(seg.size); e != nil {
					f.Close()
					return nil, e
				}
				break
			}
			r, e := DecodeRecord(bytes.NewReader(line))
			if e != nil {
				s.recovery.Add(1)
			} else {
				k := recordKey(r)
				seg.ids = append(seg.ids, k)
				s.ids[k] = true
			}
			seg.size += int64(len(line))
		}
		f.Close()
		s.total += seg.size
		s.segments = append(s.segments, seg)
	}
	s.mu.Lock()
	e = s.prune(time.Now(), 0)
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	go s.run()
	return s, nil
}
func recordKey(r Record) string { return r.Flow.DeviceID + "\x00" + r.Flow.SessionID + "\x00" + r.ID }

// TryAppend performs bounded validation/copying and a nonblocking queue send, never disk I/O.
func (s *Store) TryAppend(r Record) bool {
	b, e := encode(r)
	if e != nil || int64(len(b)) > s.opts.MaxBytes {
		s.dropped.Add(1)
		return false
	}
	r.Data = append(json.RawMessage(nil), r.Data...)
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.closed {
		return false
	}
	select {
	case s.queue <- queued{r, b}:
		return true
	default:
		s.dropped.Add(1)
		return false
	}
}
func (s *Store) run() {
	defer close(s.done)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case q, ok := <-s.queue:
			if !ok {
				s.mu.Lock()
				if s.file != nil {
					if e := s.file.Close(); e != nil {
						s.lastErr = e
					}
				}
				s.mu.Unlock()
				return
			}
			s.mu.Lock()
			if e := s.append(q); e != nil {
				s.lastErr = e
				s.dropped.Add(1)
			}
			s.mu.Unlock()
		case <-tick.C:
			s.mu.Lock()
			if e := s.prune(time.Now(), 0); e != nil {
				s.lastErr = e
			}
			s.mu.Unlock()
		}
	}
}
func (s *Store) append(q queued) error {
	key := recordKey(q.r)
	if s.ids[key] {
		return nil
	}
	if q.r.At.Before(time.Now().Add(-s.opts.Retention)) {
		s.truncated.Store(true)
		return nil
	}
	if e := s.prune(time.Now(), int64(len(q.b))); e != nil {
		return e
	}
	target := s.opts.MaxBytes / 4
	if target > 1<<20 {
		target = 1 << 20
	}
	if s.file != nil && s.segments[len(s.segments)-1].size+int64(len(q.b)) > target {
		s.file.Close()
		s.file = nil
	}
	if s.file == nil {
		p := filepath.Join(s.opts.Dir, fmt.Sprintf("research-%020d.jsonl", s.seq))
		s.seq++
		f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		s.file = f
		s.segments = append(s.segments, segment{path: p, at: time.Now()})
	}
	seg := &s.segments[len(s.segments)-1]
	n, e := s.file.Write(q.b)
	if e == nil && n != len(q.b) {
		e = io.ErrShortWrite
	}
	if e != nil {
		if te := s.file.Truncate(seg.size); te != nil {
			s.file.Close()
			s.file = nil
		} else {
			_, _ = s.file.Seek(seg.size, io.SeekStart)
		}
		return e
	}
	seg.size += int64(n)
	seg.at = time.Now()
	seg.ids = append(seg.ids, key)
	s.ids[key] = true
	s.total += int64(n)
	return nil
}
func (s *Store) prune(now time.Time, incoming int64) error {
	for len(s.segments) > 0 && (s.total+incoming > s.opts.MaxBytes || s.segments[0].at.Before(now.Add(-s.opts.Retention))) {
		seg := s.segments[0]
		if len(s.segments) == 1 && s.file != nil {
			s.file.Close()
			s.file = nil
		}
		if e := os.Remove(seg.path); e != nil {
			return e
		}
		for _, id := range seg.ids {
			delete(s.ids, id)
		}
		s.total -= seg.size
		s.segments = s.segments[1:]
		s.truncated.Store(true)
	}
	return nil
}
func (s *Store) Close() error {
	s.lifecycle.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.lifecycle.Unlock()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}
func (s *Store) coverage() Coverage {
	c := Coverage{Dropped: s.dropped.Load(), RecoveryGaps: s.recovery.Load(), Truncated: s.truncated.Load()}
	switch {
	case c.Dropped > 0:
		c.Incomplete, c.Reason = true, "diagnostic_loss"
	case c.RecoveryGaps > 0:
		c.Incomplete, c.Reason = true, "recovered_archive"
	case c.Truncated:
		c.Incomplete, c.Reason = true, "retention_or_quota"
	}
	return c
}
func matches(r Record, q Query) bool {
	if q.DeviceID != "" && q.DeviceID != r.Flow.DeviceID || q.SessionID != "" && q.SessionID != r.Flow.SessionID {
		return false
	}
	var d FlowData
	json.Unmarshal(r.Data, &d)
	if q.UserID != "" && q.UserID != d.UserID {
		return false
	}
	// A session-wide gap cannot be assigned to a particular application or
	// destination. Retain it under flow filters, but never widen identity/time scope.
	if r.Type != "gap" {
		if q.Protocol != "" && q.Protocol != d.Protocol || q.Address != "" && !strings.Contains(d.Source, q.Address) && !strings.Contains(d.Destination, q.Address) || q.Domain != "" && q.Domain != d.Domain {
			return false
		}
		if q.Application != "" {
			found := q.Application == d.App.Attribution
			for _, p := range d.App.Packages {
				found = found || p == q.Application
			}
			if !found {
				return false
			}
		}
	}
	from, to := r.At, r.At
	if !d.ObservedFrom.IsZero() {
		from = d.ObservedFrom
	}
	if !d.ObservedTo.IsZero() {
		to = d.ObservedTo
	}
	return (q.From.IsZero() || !to.Before(q.From)) && (q.To.IsZero() || !from.After(q.To))
}

// ErrHistoryChanged means retention removed part of a query/export snapshot.
var ErrHistoryChanged = errors.New("research history changed during read")

func includeCoverage(c *Coverage, r Record) {
	if c.From.IsZero() || r.At.Before(c.From) {
		c.From = r.At
	}
	if r.At.After(c.To) {
		c.To = r.At
	}
	if r.Type == "gap" {
		c.Incomplete = true
		if c.Reason == "" {
			c.Reason = "client_reported_loss"
		}
	}
}
func failCoverage(c *Coverage, err error) {
	c.Incomplete = true
	switch {
	case errors.Is(err, ErrHistoryChanged):
		c.Truncated, c.Reason = true, "history_changed"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		c.Reason = "canceled"
	default:
		c.Reason = "read_failed"
	}
}
func (s *Store) scan(ctx context.Context, q Query, fn func(Record, string) error) error {
	s.mu.Lock()
	segs := make([]segment, len(s.segments))
	for i, v := range s.segments {
		segs[i] = segment{path: v.path, size: v.size}
	}
	s.mu.Unlock()
	for _, seg := range segs {
		if e := ctx.Err(); e != nil {
			return e
		}
		f, e := os.Open(seg.path)
		if errors.Is(e, os.ErrNotExist) {
			return ErrHistoryChanged
		}
		if e != nil {
			return e
		}
		rd := bufio.NewScanner(io.LimitReader(f, seg.size))
		rd.Buffer(make([]byte, 4096), MaxRecordBytes)
		var line uint64
		for rd.Scan() {
			line++
			if e = ctx.Err(); e != nil {
				break
			}
			cursor := filepath.Base(seg.path) + ":" + fmt.Sprintf("%020d", line)
			if q.Cursor != "" && cursor <= q.Cursor {
				continue
			}
			r, de := DecodeRecord(bytes.NewReader(rd.Bytes()))
			if de != nil {
				continue
			}
			if r.At.Before(time.Now().Add(-s.opts.Retention)) || !matches(r, q) {
				continue
			}
			if e = fn(r, cursor); e != nil {
				break
			}
		}
		se := rd.Err()
		f.Close()
		if e != nil {
			return e
		}
		if se != nil {
			return se
		}
	}
	return ctx.Err()
}

var stopScan = errors.New("page complete")

func (s *Store) Query(ctx context.Context, q Query) (Page, error) {
	p := Page{Records: []Record{}, Coverage: s.coverage()}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var last string
	e := s.scan(ctx, q, func(r Record, c string) error {
		if len(p.Records) == limit {
			p.NextCursor = last
			return stopScan
		}
		p.Records = append(p.Records, r)
		last = c
		includeCoverage(&p.Coverage, r)
		return nil
	})
	if errors.Is(e, stopScan) {
		e = nil
	}
	if e != nil {
		failCoverage(&p.Coverage, e)
	}
	return p, e
}

// ExportSummary is the required final line of a JSONL download. Complete means
// the snapshot was fully read, not that all original traffic was observed.
type ExportSummary struct {
	SchemaVersion int      `json:"schema_version"`
	Type          string   `json:"type"`
	Complete      bool     `json:"complete"`
	Records       uint64   `json:"records,string"`
	Coverage      Coverage `json:"coverage"`
}

func (s *Store) Export(ctx context.Context, q Query, w io.Writer) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	q.Cursor = ""
	q.Limit = 0
	manifest := struct {
		SchemaVersion     int       `json:"schema_version"`
		Type              string    `json:"type"`
		At                time.Time `json:"at"`
		Filters           Query     `json:"filters"`
		Coverage          Coverage  `json:"coverage"`
		Scope             string    `json:"scope"`
		CounterSemantics  string    `json:"counter_semantics"`
		BoundaryPrecision string    `json:"boundary_precision"`
	}{1, "manifest", time.Now().UTC(), q, s.coverage(), "observed own tunnel logical traffic only; client application names are claims", "delta records are increments; total records must not be added to deltas", "overlapping intervals included in full; no exact allocation inside intervals"}
	if e := json.NewEncoder(w).Encode(manifest); e != nil {
		return e
	}
	summary := ExportSummary{SchemaVersion: 1, Type: "export_summary", Coverage: s.coverage()}
	var writeErr error
	scanErr := s.scan(ctx, q, func(r Record, _ string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := EncodeRecord(w, r); e != nil {
			writeErr = e
			return e
		}
		summary.Records++
		includeCoverage(&summary.Coverage, r)
		return nil
	})
	// A broken writer cannot reliably receive a failure marker. Absence of the
	// required terminal success still makes the download unambiguously incomplete.
	if writeErr != nil {
		return writeErr
	}
	summary.Complete = scanErr == nil
	if scanErr != nil {
		failCoverage(&summary.Coverage, scanErr)
	}
	if e := json.NewEncoder(w).Encode(summary); e != nil {
		return errors.Join(scanErr, e)
	}
	return scanErr
}
