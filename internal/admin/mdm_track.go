package admin

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"quiclab/internal/mdm"
	"strconv"
	"strings"
	"time"
)

//go:embed mdm_track.js vendor/leaflet/*
var trackAssets embed.FS

type cellPosition struct {
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
	Range float64 `json:"range"`
}
type cellCacheEntry struct {
	position *cellPosition
	until    time.Time
}
type cellResolver struct {
	gate     chan struct{}
	cache    map[string]cellCacheEntry
	endpoint string
	client   *http.Client
	interval time.Duration
	last     time.Time
}

func newCellResolver() *cellResolver {
	return &cellResolver{gate: make(chan struct{}, 1), cache: make(map[string]cellCacheEntry), endpoint: "https://api.mylnikov.org/geolocation/cell", client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, interval: 500 * time.Millisecond}
}
func cellQuery(c mdm.CellRadio) (url.Values, error) {
	// The provider does not document NR/CDMA identifiers. Never truncate NCI to LTE CI.
	if c.Technology != "LTE" && c.Technology != "GSM" && c.Technology != "WCDMA" {
		return nil, errors.New("unsupported radio")
	}
	vals := []string{c.MCC, c.MNC, c.TAC, c.CI}
	max := []uint64{999, 999, 65535, 268435455}
	if c.Technology == "GSM" {
		max[3] = 65535
	}
	for i, v := range vals {
		if v == "" || len(v) > 10 || strings.Trim(v, "0123456789") != "" {
			return nil, errors.New("invalid cell")
		}
		n, e := strconv.ParseUint(v, 10, 64)
		if e != nil || n > max[i] || i == 0 && n == 0 {
			return nil, errors.New("invalid cell")
		}
	}
	return url.Values{"v": {"1.1"}, "data": {"open"}, "mcc": {c.MCC}, "mnc": {c.MNC}, "lac": {c.TAC}, "cellid": {c.CI}}, nil
}
func (g *cellResolver) resolve(ctx context.Context, c mdm.CellRadio) (*cellPosition, error) {
	return g.fetch(ctx, c, true)
}
func (g *cellResolver) fetch(ctx context.Context, c mdm.CellRadio, useCache bool) (*cellPosition, error) {
	q, e := cellQuery(c)
	if e != nil {
		return nil, e
	}
	select {
	case g.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-g.gate }()
	key := c.Technology + ":" + q.Encode()
	if v, ok := g.cache[key]; useCache && ok && time.Now().Before(v.until) {
		return v.position, nil
	}
	if wait := time.Until(g.last.Add(g.interval)); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	req, e := http.NewRequestWithContext(ctx, "GET", g.endpoint+"?"+q.Encode(), nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "quic-lab-cell-map/1.0 (+https://github.com/vponomarev/quic-lab)")
	g.last = time.Now()
	resp, e := g.client.Do(req)
	if e != nil {
		return nil, errors.New("location provider unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("location provider unavailable")
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(body) > 65536 {
		return nil, errors.New("invalid location response")
	}
	var raw struct {
		Result int                        `json:"result"`
		Data   map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return nil, errors.New("invalid location response")
	}
	var p *cellPosition
	ttl := time.Hour
	if raw.Result == 200 {
		values := make([]float64, 3)
		for i, k := range []string{"lat", "lon", "range"} {
			data, ok := raw.Data[k]
			if !ok || string(data) == "null" {
				return nil, errors.New("incomplete location response")
			}
			var v float64
			if json.Unmarshal(data, &v) != nil {
				var s string
				if json.Unmarshal(data, &s) != nil {
					return nil, errors.New("invalid coordinate")
				}
				v, e = strconv.ParseFloat(s, 64)
				if e != nil {
					return nil, errors.New("invalid coordinate")
				}
			}
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, errors.New("invalid coordinate")
			}
			values[i] = v
		}
		if math.Abs(values[0]) > 90 || math.Abs(values[1]) > 180 || values[2] <= 0 || values[2] > 20000000 {
			return nil, errors.New("invalid location range")
		}
		p = &cellPosition{values[0], values[1], values[2]}
		ttl = 24 * time.Hour
	} else if raw.Result != 404 {
		return nil, errors.New("location provider unavailable")
	}
	if len(g.cache) >= 10000 {
		g.cache = make(map[string]cellCacheEntry)
	}
	if useCache {
		g.cache[key] = cellCacheEntry{p, time.Now().Add(ttl)}
	}
	return p, nil
}
func (w *Web) trackRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /mdm/track", w.mdmTrack)
	m.HandleFunc("POST /mdm/cell-db/positions", w.mdmCellPositions)
	m.HandleFunc("POST /mdm/cell-db/rescan", w.mdmCellRescan)
	m.HandleFunc("GET /mdm/cell-db/status", w.mdmCellDBStatus)
	m.HandleFunc("GET /mdm/cell-db/history", w.mdmCellHistory)
	m.HandleFunc("POST /mdm/cell-location", w.mdmCellLocation)
	m.HandleFunc("GET /mdm-track.js", func(rw http.ResponseWriter, r *http.Request) {
		b, _ := trackAssets.ReadFile("mdm_track.js")
		rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		rw.Write(b)
	})
	m.Handle("GET /vendor/leaflet/", http.FileServer(http.FS(trackAssets)))
}
func (w *Web) mdmTrack(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "no-store")
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	start, e1 := time.Parse(time.RFC3339, r.Form.Get("start"))
	end, e2 := time.Parse(time.RFC3339, r.Form.Get("end"))
	if e1 != nil || e2 != nil {
		http.Error(rw, "Некорректный период", 400)
		return
	}
	rows, e := w.MDM.TelemetryRange(r.Form.Get("id"), time.Now().UTC(), start, end, 25000)
	if e != nil {
		http.Error(rw, "История недоступна: проверьте устройство и период (до 7 суток / 25000 измерений)", 400)
		return
	}
	// Only return fields needed by the map, not SSID/BSSID or client identity.
	type observation struct {
		At    time.Time       `json:"at"`
		Cells []mdm.CellRadio `json:"cells"`
	}
	out := make([]observation, 0, len(rows))
	for _, row := range rows {
		out = append(out, observation{row.Sample.MeasuredAt, row.Sample.Cells})
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(out)
}
func (w *Web) mdmCellLocation(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "no-store")
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	c := mdm.CellRadio{Technology: r.Form.Get("technology"), MCC: r.Form.Get("mcc"), MNC: r.Form.Get("mnc"), TAC: r.Form.Get("tac"), CI: r.Form.Get("ci")}
	if _, e := cellQuery(c); e != nil {
		http.Error(rw, "Неподдерживаемая или неполная сота", 400)
		return
	}
	db, e := w.geoStore()
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	if e = db.Enqueue(geoCell(c), "mylnikov", 0, false); e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	p, e := db.Lookup(geoCell(c))
	if e != nil {
		http.Error(rw, "Cell database unavailable", 503)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(p)
}
