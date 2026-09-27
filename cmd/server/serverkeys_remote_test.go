package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/coder/websocket"
	"github.com/quic-go/quic-go"
	"net"
	"os"
	"quiclab/internal/protocol"
	"testing"
	"time"
)

// Opt-in against a lab owned by the operator; ordinary trusted TLS, no client key logging.
func TestRemoteEchoWithoutClientSecrets(t *testing.T) {
	host := os.Getenv("QUICLAB_SERVER_SMOKE_HOST")
	if host == "" {
		t.Skip("explicit lab hostname required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, e := quic.DialAddr(ctx, net.JoinHostPort(host, "4433"), &tls.Config{ServerName: host, NextProtos: []string{protocol.ALPN}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseWithError(0, "")
	st, e := c.OpenStreamSync(ctx)
	if e != nil {
		t.Fatal(e)
	}
	ws, _, e := websocket.Dial(ctx, "wss://"+host+"/echo", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ws.CloseNow()
	reader := bufio.NewReader(st)
	for i := 0; i < 10; i++ {
		b := []byte(fmt.Sprintf("{\"seq\":%d,\"sent_ns\":123,\"stream_id\":0}\n", i))
		if _, e = st.Write(b); e != nil {
			t.Fatal(e)
		}
		if _, e = reader.ReadString('\n'); e != nil {
			t.Fatal(e)
		}
		if e = ws.Write(ctx, websocket.MessageText, b); e != nil {
			t.Fatal(e)
		}
		if _, _, e = ws.Read(ctx); e != nil {
			t.Fatal(e)
		}
	}
}
