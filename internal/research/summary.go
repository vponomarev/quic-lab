package research

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/netobserve"
	"sort"
	"strings"
	"time"
)

type ApplicationStats struct {
	ExitRef
	Origin, MeasurementLevel              string
	App                                   App
	Flows, DNSQueries, BytesUp, BytesDown uint64
	Last                                  time.Time
}
type ApplicationSummary struct {
	Coverage     Coverage
	Applications []ApplicationStats
	Partial      bool
}
type summaryFlowKey struct {
	Origin string
	Flow   FlowKey
}
type summaryFlow struct {
	ApplicationStats
	detected *netobserve.ProtocolInfo
	tls      *netobserve.TLSInfo
	observed bool
}

// ApplicationSummary ignores page/cursor and aggregates one bounded history
// snapshot. Origins remain separate; no client/server byte reconciliation.
func (s *Store) ApplicationSummary(ctx context.Context, q Query) (ApplicationSummary, error) {
	result := ApplicationSummary{Applications: []ApplicationStats{}, Coverage: s.coverage()}
	detectedProtocol := q.DetectedProtocol
	q.DetectedProtocol = ""
	sni := q.SNI
	q.SNI = ""
	q.Cursor = ""
	q.Limit = 0
	flows := map[summaryFlowKey]*summaryFlow{}
	err := s.scan(ctx, q, func(r Record, _ string) error {
		includeCoverage(&result.Coverage, r)
		if r.Type == "gap" {
			result.Partial = true
			return nil
		}
		if r.Type != "app" && r.Type != "dns" && !strings.HasPrefix(r.Type, "flow_") {
			return nil
		}
		var d struct {
			FlowData
			Response *bool `json:"response"`
		}
		if json.Unmarshal(r.Data, &d) != nil {
			result.Partial = true
			return nil
		}
		if r.Type == "app" {
			if json.Unmarshal(r.Data, &d.App) != nil {
				result.Partial = true
				return nil
			}
		}
		key := summaryFlowKey{r.Origin, r.Flow}
		f := flows[key]
		if f == nil {
			if len(flows) >= 20000 {
				result.Partial = true
				return stopScan
			}
			f = &summaryFlow{}
			f.Origin = r.Origin
			flows[key] = f
		}
		if r.Type != "app" && d.ExitRef != (ExitRef{}) {
			merged, compatible := mergeObservedExit(f.ExitRef, d.ExitRef)
			if !compatible {
				result.Partial = true
				return nil
			}
			f.ExitRef = merged
		}
		if len(d.App.Packages) > 0 {
			f.App = d.App
		} else if len(f.App.Packages) == 0 && d.App.Attribution != "" {
			f.App = d.App
		}
		if r.At.After(f.Last) {
			f.Last = r.At
		}
		if f.detected == nil && d.Detected != nil {
			f.detected = d.Detected
		}
		if d.TLS != nil && (f.tls == nil || f.tls.State != "observed") {
			f.tls = d.TLS
		}
		if d.Incomplete {
			result.Partial = true
		}
		if r.Type == "dns" {
			if d.Response != nil && !*d.Response {
				f.DNSQueries++
			}
			return nil
		}
		if !strings.HasPrefix(r.Type, "flow_") {
			return nil
		}
		if f.observed && f.MeasurementLevel != d.MeasurementLevel {
			result.Partial = true
			return nil
		}
		f.observed = true
		f.MeasurementLevel = d.MeasurementLevel
		f.Flows = 1
		if d.CounterKind == "delta" {
			f.BytesUp += d.BytesUp
			f.BytesDown += d.BytesDown
		} else if d.BytesUp != 0 || d.BytesDown != 0 {
			result.Partial = true
		}
		return nil
	})
	if errors.Is(err, stopScan) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		result.Partial = true
		err = nil
	}
	groups := map[string]*ApplicationStats{}
	for _, f := range flows {
		if detectedProtocol != "" && (f.detected == nil || !strings.EqualFold(detectedProtocol, f.detected.Name)) {
			continue
		}
		if sni != "" && (f.tls == nil || (sni == "<ECH>" && !f.tls.ECHOffered) || (sni != "<ECH>" && !strings.EqualFold(sni, f.tls.ServerName))) {
			continue
		}
		if !f.observed && f.DNSQueries == 0 {
			continue
		}
		pkgs := append([]string(nil), f.App.Packages...)
		sort.Strings(pkgs)
		keyBytes, _ := json.Marshal([]any{f.Origin, f.MeasurementLevel, f.ExitRef.Normalized(), pkgs, f.App.Attribution})
		key := string(keyBytes)
		a := groups[key]
		if a == nil {
			a = &ApplicationStats{ExitRef: f.ExitRef.Normalized(), Origin: f.Origin, MeasurementLevel: f.MeasurementLevel, App: f.App}
			groups[key] = a
		}
		a.Flows += f.Flows
		a.DNSQueries += f.DNSQueries
		a.BytesUp += f.BytesUp
		a.BytesDown += f.BytesDown
		if f.Last.After(a.Last) {
			a.Last = f.Last
		}
	}
	for _, a := range groups {
		result.Applications = append(result.Applications, *a)
	}
	sort.Slice(result.Applications, func(i, j int) bool {
		a, b := result.Applications[i], result.Applications[j]
		if a.Origin != b.Origin {
			return a.Origin < b.Origin
		}
		if a.BytesUp+a.BytesDown != b.BytesUp+b.BytesDown {
			return a.BytesUp+a.BytesDown > b.BytesUp+b.BytesDown
		}
		return strings.Join(a.App.Packages, ",") < strings.Join(b.App.Packages, ",")
	})
	return result, err
}

// Unknown fields may be enriched after flow ownership arrives. Known conflicts
// are not merged across exits, but enrichment must not discard byte deltas.
func mergeObservedExit(a, b ExitRef) (ExitRef, bool) {
	fields := [][2]*string{{&a.Route, &b.Route}, {&a.ID, &b.ID}, {&a.Type, &b.Type}}
	for _, f := range fields {
		old, next := *f[0], *f[1]
		if old == "" || old == "unknown" {
			*f[0] = next
		} else if next != "" && next != "unknown" && next != old {
			return a, false
		}
	}
	return a, true
}
