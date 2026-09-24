package echo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"quiclab/internal/protocol"
)

// WebSocketHandler runs behind the local nginx HTTPS endpoint. Each successful
// upgrade receives a new server-generated identity, never a client session ID.
func WebSocketHandler(log *slog.Logger) http.Handler {
	slots := make(chan struct{}, 128)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "lab busy", http.StatusServiceUnavailable)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(4096)
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return
		}
		identity := hex.EncodeToString(id[:])
		log.Info("wss_connection_accepted", "connection_id", identity)
		defer log.Info("wss_connection_closed", "connection_id", identity)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			var f protocol.Frame
			err = wsjson.Read(ctx, conn, &f)
			cancel()
			if err != nil {
				return
			}
			f.ConnectionID = identity
			f.StreamID = 0
			f.Peer = r.RemoteAddr // reverse proxy peer; do not present this as phone IP
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
			err = wsjson.Write(ctx, conn, f)
			cancel()
			if err != nil {
				return
			}
		}
	})
}
