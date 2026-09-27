package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/quic-go/quic-go"
	"quiclab/internal/admin"
	"quiclab/internal/debugcapture"
	"quiclab/internal/labcert"
	"quiclab/internal/protocol"
)

func TestDebugCaptureRealTLS(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("QUICLAB_CAPTURE_INTEGRATION") != "1" {
		t.Skip("opt-in Linux tcpdump integration")
	}
	dump, e := exec.LookPath("tcpdump")
	if e != nil {
		t.Fatal(e)
	}
	cp, kp, e := labcert.Generate()
	if e != nil {
		t.Fatal(e)
	}
	cert, e := tls.X509KeyPair(cp, kp)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, e := debugcapture.New(ctx, debugcapture.Config{Interface: "lo", FirstPort: 59440, Slots: 2, TCPDump: dump}, captureFactory(cert, admin.Config{}, nil, "0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil))))
	if e != nil {
		t.Fatal(e)
	}
	s, e := m.Start("integration", "echo", "")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Stop("test finished")
	address := fmt.Sprintf("127.0.0.1:%d", s.Port)
	c, e := quic.DialAddr(ctx, address, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{protocol.ALPN}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	stream, e := c.OpenStreamSync(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = stream.Write([]byte("{\"seq\":731}\n")); e != nil {
		t.Fatal(e)
	}
	got, e := bufio.NewReader(stream).ReadString('\n')
	if e != nil || !strings.Contains(got, "731") {
		t.Fatalf("echo %s %v", got, e)
	}
	c.CloseWithError(0, "done")
	ws, _, e := websocket.Dial(ctx, "wss://"+address+"/echo", &websocket.DialOptions{HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = ws.Write(ctx, websocket.MessageText, []byte(`{"seq":732}`)); e != nil {
		t.Fatal(e)
	}
	_, reply, e := ws.Read(ctx)
	if e != nil || !strings.Contains(string(reply), "732") {
		t.Fatalf("websocket %s %v", reply, e)
	}
	ws.CloseNow()
	time.Sleep(1200 * time.Millisecond)
	s.Stop("test finished")
	time.Sleep(100 * time.Millisecond)
	blocks, _ := s.Blocks(0)
	if len(blocks) < 10 {
		t.Fatal("no packets", len(blocks))
	}
	output := os.Getenv("QUICLAB_CAPTURE_OUTPUT")
	if output != "" {
		f, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		for _, b := range blocks {
			if _, e = f.Write(b); e != nil {
				t.Fatal(e)
			}
		}
		f.Close()
	}
}
