package echo

import (
	"context"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"io"
	"log/slog"
	"net/http/httptest"
	"quiclab/internal/protocol"
	"strings"
	"testing"
	"time"
)

func TestWebSocketEchoAndNewServerIdentity(t *testing.T) {
	srv := httptest.NewServer(WebSocketHandler(slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	previous := ""
	for range 2 {
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		sent := protocol.Frame{Seq: 42, SentNS: 123456, ConnectionID: "untrusted-client-id"}
		if err := wsjson.Write(ctx, c, sent); err != nil {
			t.Fatal(err)
		}
		var got protocol.Frame
		if err := wsjson.Read(ctx, c, &got); err != nil {
			t.Fatal(err)
		}
		if got.Seq != sent.Seq || got.SentNS != sent.SentNS {
			t.Fatal("echo changed payload", got)
		}
		if got.ConnectionID == "" || got.ConnectionID == sent.ConnectionID || got.ConnectionID == previous {
			t.Fatal("identity not server-owned", got)
		}
		previous = got.ConnectionID
		c.CloseNow()
	}
}
