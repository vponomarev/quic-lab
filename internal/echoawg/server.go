// Package echoawg runs an isolated userspace AWG lab, with no host forwarding.
package echoawg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"net"
	"net/http"
	"net/netip"
	"quiclab/internal/awg/netstack"
	"quiclab/internal/awgserver"
	"strconv"
	"sync"
	"time"
)

const Address = "10.253.253.1"
const ProbePort = 9000
const Lifetime = 10 * time.Minute
const MaxPeers = 64

type lease struct {
	public string
	until  time.Time
}
type Server struct {
	mu         sync.Mutex
	device     *device.Device
	udp        net.PacketConn
	identity   *awgserver.Identity
	endpoint   string
	leases     map[int]lease
	probe      func(context.Context) error
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	admissions int
	window     time.Time
	closed     bool
}

func Start(parent context.Context, endpoint string, probe func(context.Context) error) (*Server, error) {
	host, port, e := net.SplitHostPort(endpoint)
	n, x := strconv.Atoi(port)
	if e != nil || x != nil || host == "" || n < 1 || n > 65535 {
		return nil, errors.New("echo AWG endpoint must be host:port")
	}
	identity, e := awgserver.NewIdentity()
	if e != nil {
		return nil, e
	}
	tun, stack, e := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr(Address)}, nil, 1280)
	if e != nil {
		return nil, e
	}
	d := device.NewDevice(tun, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	key, _ := awgserver.KeyHex(identity.Private)
	if e = d.IpcSet(fmt.Sprintf("private_key=%s\nlisten_port=%d\n%s", key, n, identity.Parameters())); e != nil {
		d.Close()
		return nil, e
	}
	if e = d.Up(); e != nil {
		d.Close()
		return nil, e
	}
	udp, e := stack.ListenUDPAddrPort(netip.AddrPortFrom(netip.MustParseAddr(Address), ProbePort))
	if e != nil {
		d.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Server{device: d, udp: udp, identity: identity, endpoint: endpoint, leases: map[int]lease{}, probe: probe, cancel: cancel}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.mu.Lock()
				s.prune(time.Now())
				s.mu.Unlock()
			}
		}
	}()
	go s.serveProbe(ctx)
	return s, nil
}
func (s *Server) prune(now time.Time) {
	for id, l := range s.leases {
		if !now.Before(l.until) {
			if s.device.IpcSet("public_key="+l.public+"\nremove=true\n") == nil {
				delete(s.leases, id)
			}
		}
	}
}
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cancel()
	s.udp.Close()
	s.device.Close()
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST required", 405)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		http.Error(w, "echo unavailable", 503)
		return
	}
	now := time.Now()
	s.prune(now)
	if now.Sub(s.window) >= time.Minute {
		s.window = now
		s.admissions = 0
	}
	if s.admissions >= 32 || len(s.leases) >= MaxPeers {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "echo busy; retry later", 429)
		return
	}
	s.admissions++
	id := 2
	for ; id < MaxPeers+2; id++ {
		if _, ok := s.leases[id]; !ok {
			break
		}
	}
	peer, e := awgserver.NewPeer(fmt.Sprintf("10.253.253.%d", id))
	if e != nil {
		http.Error(w, "provisioning failed", 500)
		return
	}
	pub, _ := awgserver.KeyHex(peer.Public)
	psk, _ := awgserver.KeyHex(peer.PSK)
	if e = s.device.IpcSet(fmt.Sprintf("public_key=%s\npreshared_key=%s\nallowed_ip=%s/32\n", pub, psk, peer.Address)); e != nil {
		http.Error(w, "provisioning failed", 500)
		return
	}
	until := now.Add(Lifetime)
	s.leases[id] = lease{pub, until}
	c := awgserver.Config{Endpoint: s.endpoint, DNS: Address, MTU: 1280, AllowedIPs: []string{Address + "/32"}}
	response := map[string]any{"transport": "awg", "endpoint": s.endpoint, "awg_config": c.Client(*s.identity, *peer), "awg_probe_endpoint": net.JoinHostPort(Address, "0"), "expires_at": until.Unix(), "probe_exit_ip": false}
	if s.probe != nil {
		response["transit_endpoint"] = net.JoinHostPort(Address, strconv.Itoa(ProbePort))
		response["transit_udp"] = true
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Only a fixed transit probe is exposed. No destination supplied by the client is used.
func (s *Server) serveProbe(ctx context.Context) {
	defer s.wg.Done()
	slots := make(chan struct{}, 4)
	var work sync.WaitGroup
	defer work.Wait()
	last := map[string]time.Time{}
	for {
		b := make([]byte, 32)
		n, addr, e := s.udp.ReadFrom(b)
		if e != nil {
			return
		}
		if n != 24 || string(b[:8]) != "QLPROBE1" || s.probe == nil {
			continue
		}
		host, _, _ := net.SplitHostPort(addr.String())
		now := time.Now()
		if now.Sub(last[host]) < time.Second {
			continue
		}
		last[host] = now
		select {
		case slots <- struct{}{}:
		default:
			continue
		}
		work.Add(1)
		go func(payload []byte, a net.Addr) {
			defer work.Done()
			defer func() { <-slots }()
			p, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if s.probe(p) == nil {
				s.udp.WriteTo(payload, a)
			}
		}(b[:n], addr)
	}
}
