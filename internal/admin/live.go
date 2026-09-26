package admin

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

//go:embed stats.js
var statsJS []byte

func (w *Web) statsScript(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	rw.Write(statsJS)
}

type liveUserStats struct {
	Rate        string   `json:"rate"`
	Name        string   `json:"name"`
	ID          string   `json:"id"`
	Connections []string `json:"connections"`
	Last        string   `json:"last"`
	TX          string   `json:"tx"`
	RX          string   `json:"rx"`
}

func (w *Web) statsSnapshot() []liveUserStats {
	users := w.Store.List()
	out := make([]liveUserStats, 0, len(users))
	for _, u := range users {
		row := liveUserStats{Rate: u.Stats.RateText(), ID: u.ID, Name: u.Name, Last: "Ещё не подключался", TX: u.Stats.TXText(), RX: u.Stats.RXText(), Connections: []string{}}
		if !u.LastConnected.IsZero() {
			row.Last = u.LastConnected.UTC().Format("02.01.2006 15:04:05") + " UTC\n" + u.LastTransport + " · " + u.LastSource
		}
		for _, c := range u.Stats.Connections {
			row.Connections = append(row.Connections, c.Transport+" · "+c.Source+"\nС "+c.Connected.UTC().Format("02.01 15:04:05")+" UTC")
		}
		out = append(out, row)
	}
	return out
}
func (w *Web) liveStats(rw http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != w.origin {
		http.Error(rw, "Invalid origin", http.StatusForbidden)
		return
	}
	if _, ok := w.getSession(r); !ok {
		http.Error(rw, "Authentication required", http.StatusUnauthorized)
		return
	}
	c, e := websocket.Accept(rw, r, nil)
	if e != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		// Logout and expiration revoke open sockets as well as subsequent HTTP requests.
		if _, ok := w.getSession(r); !ok {
			c.Close(websocket.StatusPolicyViolation, "Session expired; sign in again")
			return
		}
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = wsjson.Write(writeCtx, c, w.statsSnapshot())
		cancel()
		if e != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
