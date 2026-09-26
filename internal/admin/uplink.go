package admin

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"quiclab/internal/transit"
	"strings"
	"sync"
	"time"
)

type uplinkStatus struct {
	Enabled    bool      `json:"enabled"`
	Gateway    string    `json:"gateway,omitempty"`
	Reachable  bool      `json:"reachable"`
	RTT        *float64  `json:"rtt_ms"`
	ExitIP     string    `json:"exit_ip,omitempty"`
	ProbeError string    `json:"probe_error,omitempty"`
	ExitError  string    `json:"exit_error,omitempty"`
	Checked    time.Time `json:"checked"`
}
type uplinkCache struct {
	mu    sync.Mutex
	value uplinkStatus
}

func (w *Web) uplink(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.getSession(r); !ok {
		http.Error(rw, "Authentication required", 401)
		return
	}
	// One shared result per server, regardless of the number of browser tabs.
	w.uplinkState.mu.Lock()
	defer w.uplinkState.mu.Unlock()
	if time.Since(w.uplinkState.value.Checked) >= 30*time.Second {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		w.uplinkState.value = measureUplink(ctx, w.Config.Transit)
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(w.uplinkState.value)
}
func measureUplink(ctx context.Context, c *transit.Config) uplinkStatus {
	result := uplinkStatus{Checked: time.Now().UTC()}
	if c == nil {
		return result
	}
	result.Enabled = true
	result.Gateway = c.Endpoint
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		if c.Probe(ctx) == nil {
			ms := float64(time.Since(start)) / float64(time.Millisecond)
			result.RTT = &ms
		} else {
			result.ProbeError = "Нет ответа gateway на ICMP"
		}
	}()
	go func() {
		defer wg.Done()
		ip, e := uplinkExitIP(ctx, c)
		if e != nil {
			result.ExitError = "Не удалось проверить внешний IP через туннель"
		} else {
			result.ExitIP = ip
		}
	}()
	wg.Wait()
	result.Reachable = result.RTT != nil || result.ExitIP != ""
	return result
}
func uplinkExitIP(ctx context.Context, c *transit.Config) (string, error) {
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := c.Resolver().LookupIP(ctx, "ip4", host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, &net.DNSError{Err: "no IPv4 address"}
		}
		return c.DialContext(ctx, "tcp4", net.JoinHostPort(ips[0].String(), port))
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org", nil)
	if e != nil {
		return "", e
	}
	response, e := client.Do(req)
	if e != nil {
		return "", e
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, 65))
	if e != nil {
		return "", e
	}
	ip, e := netip.ParseAddr(strings.TrimSpace(string(b)))
	if e != nil {
		return "", e
	}
	if response.StatusCode != 200 || !ip.Is4() {
		return "", &net.AddrError{Err: "invalid exit IP"}
	}
	return ip.String(), nil
}
