package mobile

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestPublicThreeTransportLive(t *testing.T) {
	host := os.Getenv("QUICLAB_PUBLIC_TEST_HOST")
	if host == "" {
		t.Skip("opt-in public server test")
	}
	ip, e := net.ResolveIPAddr("ip4", host)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("POST", "https://"+host+"/lab/echo/awg", nil)
	response, e := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	var cfg map[string]any
	if e = json.NewDecoder(response.Body).Decode(&cfg); e != nil {
		t.Fatal(e)
	}
	cfg["endpoint"] = net.JoinHostPort(ip.String(), "51821")
	raw, _ := json.Marshal(cfg)
	events := make(chan map[string]any, 1024)
	sink := &publicEvents{events}
	a := NewGateway(sink)
	if e = a.Start(string(raw), nil); e != nil {
		t.Fatal(e)
	}
	defer a.Stop()
	wait := func(label string) {
		t.Helper()
		seen := map[string]bool{}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for len(seen) < 2 {
			select {
			case v := <-events:
				k, _ := v["event"].(string)
				if k == "echo" || k == "transit_echo" {
					seen[k] = true
					t.Log(label, k, v["rtt_ms"])
				}
			case <-ctx.Done():
				t.Fatal(label, "missing responses", seen)
			}
		}
	}
	wait("awg")
	a.Stop()
	q := NewClient(sink)
	if e = q.Start(net.JoinHostPort(ip.String(), "4433"), host, "", 50, nil); e != nil {
		t.Fatal(e)
	}
	defer q.Stop()
	wait("quic")
	q.Stop()
	w := NewWebSocketClient(sink)
	if e = w.Start(net.JoinHostPort(ip.String(), "443"), host, 50, nil); e != nil {
		t.Fatal(e)
	}
	defer w.Stop()
	wait("https")
}

type publicEvents struct{ c chan map[string]any }

func (s *publicEvents) OnEvent(raw string) {
	var v map[string]any
	if json.Unmarshal([]byte(raw), &v) == nil {
		select {
		case s.c <- v:
		default:
		}
	}
}
