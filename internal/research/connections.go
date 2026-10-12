package research

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Connection struct {
	Record     Record
	Flow       FlowData
	Events     []Record
	EventCount int
	key        string
	observed   bool
}
type ConnectionPage struct {
	Rows       []Connection
	Coverage   Coverage
	NextCursor string
	Partial    bool
}

// Connections folds deltas within a single observation origin and logical flow.
// Filters on discovered metadata apply AFTER folding. No client/server summation.
func (s *Store) Connections(ctx context.Context, q Query) (ConnectionPage, error) {
	out := ConnectionPage{Coverage: s.coverage()}
	scanQ := q
	scanQ.Cursor = ""
	scanQ.Application = ""
	scanQ.Protocol = ""
	scanQ.Address = ""
	scanQ.Domain = ""
	scanQ.Route = ""
	scanQ.ExitID = ""
	scanQ.SNI = ""
	flows := map[summaryFlowKey]*Connection{}
	size, eventsSize := 0, 0
	err := s.scan(ctx, scanQ, func(r Record, _ string) error {
		includeCoverage(&out.Coverage, r)
		if r.Type != "app" && !strings.HasPrefix(r.Type, "flow_") {
			return nil
		}
		if size+len(r.Data) > 32<<20 {
			out.Partial = true
			return stopScan
		}
		size += len(r.Data)
		k := summaryFlowKey{r.Origin, r.Flow}
		f := flows[k]
		if f == nil {
			if len(flows) >= 20000 {
				out.Partial = true
				return stopScan
			}
			f = &Connection{Record: r}
			f.Record.Data = nil
			flows[k] = f
		}
		var d FlowData
		if json.Unmarshal(r.Data, &d) != nil {
			return nil
		}
		if r.Type == "app" {
			json.Unmarshal(r.Data, &d.App)
			if len(d.App.Packages) > 0 {
				f.Flow.App = d.App
			}
			return nil
		}
		if f.observed && f.Flow.MeasurementLevel != d.MeasurementLevel {
			out.Partial = true
			return nil
		}
		exit, compatible := mergeObservedExit(f.Flow.ExitRef, d.ExitRef)
		if !compatible {
			out.Partial = true
			return nil
		}
		app, tlsInfo := f.Flow.App, f.Flow.TLS
		if len(d.App.Packages) > 0 || app.Attribution == "" {
			app = d.App
		}
		if d.TLS != nil && (tlsInfo == nil || tlsInfo.State != "observed") {
			tlsInfo = d.TLS
		}
		from, to, started := f.Flow.ObservedFrom, f.Flow.ObservedTo, f.Flow.StartedAt
		if from.IsZero() || (!d.ObservedFrom.IsZero() && d.ObservedFrom.Before(from)) {
			from = d.ObservedFrom
		}
		if d.ObservedTo.After(to) {
			to = d.ObservedTo
		}
		if started == nil {
			started = d.StartedAt
		}
		up, down, pu, pd := f.Flow.BytesUp, f.Flow.BytesDown, f.Flow.PacketsUp, f.Flow.PacketsDown
		if d.CounterKind == "delta" {
			up += d.BytesUp
			down += d.BytesDown
			pu += d.PacketsUp
			pd += d.PacketsDown
		} else if d.BytesUp != 0 || d.BytesDown != 0 {
			out.Partial = true
		}
		closed := f.Flow.Status == "closed" || r.Type == "flow_close" || d.Status == "closed"
		incomplete := f.Flow.Incomplete || d.Incomplete
		f.Flow = d
		f.Flow.ExitRef = exit
		f.Flow.App = app
		f.Flow.TLS = tlsInfo
		f.Flow.ObservedFrom = from
		f.Flow.ObservedTo = to
		f.Flow.StartedAt = started
		f.Flow.BytesUp = up
		f.Flow.BytesDown = down
		f.Flow.PacketsUp = pu
		f.Flow.PacketsDown = pd
		f.Flow.Incomplete = incomplete
		f.Flow.CounterKind = "sum_of_observed_deltas"
		if closed {
			f.Flow.Status = "closed"
		}
		if !f.observed || r.At.After(f.Record.At) {
			f.Record = r
		}
		f.Record.Data = nil
		f.Record.Type = "connection"
		f.observed = true
		f.EventCount++
		if len(f.Events) < 8 && eventsSize+len(r.Data) <= 4<<20 {
			f.Events = append(f.Events, r)
			eventsSize += len(r.Data)
		}

		return nil
	})
	if errors.Is(err, stopScan) || errors.Is(err, context.DeadlineExceeded) {
		out.Partial = true
		err = nil
	}
	if out.Partial {
		out.Coverage.Incomplete = true
		out.Coverage.Reason = "aggregation_limit"
	}
	if err != nil {
		failCoverage(&out.Coverage, err)
		return out, err
	}
	cursor := ""
	if q.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil {
			return out, fmt.Errorf("invalid connection cursor")
		}
		cursor = string(b)
	}
	for k, f := range flows {
		if !f.observed {
			continue
		}
		raw, _ := json.Marshal(f.Flow)
		r := f.Record
		r.Data = raw
		if !matches(r, q) {
			continue
		}
		f.key = f.Record.At.UTC().Format("2006-01-02T15:04:05.000000000Z") + "|" + k.Origin + "|" + k.Flow.DeviceID + "|" + k.Flow.SessionID + fmt.Sprintf("|%020d", k.Flow.FlowID)
		if cursor != "" && f.key >= cursor {
			continue
		}
		out.Rows = append(out.Rows, *f)
	}
	sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].key > out.Rows[j].key })
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if len(out.Rows) > limit {
		out.Rows = out.Rows[:limit]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(out.Rows[limit-1].key))
	}
	return out, nil
}
