package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/quic-go/quic-go"
	"quiclab/internal/admin"
	"quiclab/internal/debugcapture"
	"quiclab/internal/echo"
	"quiclab/internal/labcert"
	"quiclab/internal/protocol"
)

func TestCaptureExistingPorts(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("QUICLAB_CAPTURE_INTEGRATION") != "1" {
		t.Skip("opt-in Linux tcpdump integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cp, kp, _ := labcert.Generate()
	cert, _ := tls.X509KeyPair(cp, kp)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := new(debugcapture.Router)
	udp, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	udpPort := udp.LocalAddr().(*net.UDPAddr).Port
	ql, e := quic.Listen(udp, &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{protocol.ALPN}, KeyLogWriter: router.Writer(udpPort, true)}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ql.Close()
	go echo.Serve(ctx, ql, log)
	mux := http.NewServeMux()
	server := httptest.NewUnstartedServer(mux)
	defer server.Close()
	server.TLS = &tls.Config{KeyLogWriter: router.Writer(listenerPort(server.Listener.Addr().String()), false)}
	server.StartTLS()
	_, p, _ := net.SplitHostPort(server.Listener.Addr().String())
	tcpPort, _ := strconv.Atoi(p)
	cfg := admin.Config{PublicURL: "https://lab.example/", DataDir: t.TempDir(), Username: "test", Password: "synthetic-test-password"}
	store, _ := admin.OpenStore(cfg.DataDir)
	web := admin.NewWeb(cfg, store)
	dump, _ := exec.LookPath("tcpdump")
	manager, e := debugcapture.New(ctx, debugcapture.Config{Interface: "any", Slots: 2, TCPDump: dump, Ports: map[string][2]int{"echo": {udpPort, tcpPort}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	web.Capture = manager
	router.Set(manager)
	mux.Handle("/echo", echo.WebSocketHandler(log))
	mux.Handle("/", web.Handler())
	s, e := manager.Start("teacher", "echo", "")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Stop("test finished")
	ticket, _ := s.Ticket()
	body, _ := json.Marshal(map[string]string{"id": s.ID, "ticket": ticket})
	httpClient := server.Client()
	resp, e := httpClient.Post(server.URL+"/capture/redeem", "application/json", bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	var auth struct{ Token string }
	json.NewDecoder(resp.Body).Decode(&auth)
	resp.Body.Close()
	viewer, _, e := websocket.Dial(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/capture/stream?id="+s.ID, &websocket.DialOptions{HTTPClient: httpClient, HTTPHeader: http.Header{"Authorization": {"Bearer " + auth.Token}}})
	if e != nil {
		t.Fatal(e)
	}
	defer viewer.CloseNow()
	var live bytes.Buffer
	done := make(chan error, 1)
	go func() {
		for {
			_, b, e := viewer.Read(ctx)
			if e != nil {
				done <- e
				return
			}
			live.Write(b)
		}
	}()
	c, e := quic.DialAddr(ctx, ql.Addr().String(), &tls.Config{InsecureSkipVerify: true, NextProtos: []string{protocol.ALPN}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	stream, _ := c.OpenStreamSync(ctx)
	stream.Write([]byte("{\"seq\":734}\n"))
	if _, e = bufio.NewReader(stream).ReadString('\n'); e != nil {
		t.Fatal(e)
	}
	c.CloseWithError(0, "done")
	tr := httpClient.Transport.(*http.Transport).Clone()
	tr.TLSClientConfig = tr.TLSClientConfig.Clone()
	phone, _, e := websocket.Dial(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/echo", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if e != nil {
		t.Fatal(e)
	}
	phone.Write(ctx, websocket.MessageText, []byte(`{"seq":733}`))
	if _, _, e = phone.Read(ctx); e != nil {
		t.Fatal(e)
	}
	phone.CloseNow()
	tr.CloseIdleConnections()
	if len(s.Keys()) == 0 {
		t.Fatal("server did not export TLS secrets")
	}
	time.Sleep(2500 * time.Millisecond)
	s.Stop("test finished")
	if e = <-done; websocket.CloseStatus(e) != websocket.StatusNormalClosure {
		t.Fatal(e)
	}
	if live.Len() < 5000 || live.Len() > 200000 {
		t.Fatal("missing traffic or self-capture feedback", live.Len())
	}
	if path := os.Getenv("QUICLAB_CAPTURE_EXISTING_OUTPUT"); path != "" {
		if e = os.WriteFile(path, live.Bytes(), 0600); e != nil {
			t.Fatal(e)
		}
	}
	fmt.Printf("Existing ports UDP=%d TCP=%d, live capture=%d bytes\n", udpPort, tcpPort, live.Len())
}

func TestCaptureInnerHTTPSRemainsEncrypted(t *testing.T) {
	const payload = "inner-https-application-payload-must-remain-encrypted"
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) }))
	defer target.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := debugcapture.New(ctx, debugcapture.Config{Interface: "lo", TCPDump: exe, Slots: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.StartUser("teacher", "user", "", "")
	if err != nil {
		t.Fatal(err)
	}
	address := strings.TrimPrefix(target.URL, "https://")
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := tls.Client(manager.Wrap("user", "tcp", address, conn), &tls.Config{InsecureSkipVerify: true})
	defer encrypted.Close()
	if _, err = fmt.Fprintf(encrypted, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(encrypted), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != payload {
		t.Fatal("application HTTPS failed")
	}
	blocks, _ := session.Blocks(0)
	if len(blocks) < 2 {
		t.Fatal("inner TLS transport was not captured")
	}
	for _, block := range blocks {
		if bytes.Contains(block, []byte(payload)) {
			t.Fatal("inner HTTPS application plaintext leaked into capture")
		}
	}
	if len(session.Keys()) != 0 {
		t.Fatal("inner application TLS keys leaked into server capture")
	}
}
