//go:build linux && !android

package linuxclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"quiclab/internal/approuting"
	"quiclab/mobile"
	"sync"
	"time"
)

type Binder string

func (b Binder) Bind(fd int64) error {
	return unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, string(b))
}

// A single foreground owner; callers serialize Start/Stop. TUN is nonpersistent.
type Client struct {
	op                   sync.Mutex
	requested, effective Config
	recovery             bool
	health               *healthMonitor
	vpnCancel            context.CancelFunc
	mu                   sync.Mutex
	dev                  *os.File
	gateway              *mobile.Gateway
	router               *mobile.DesktopAppRouter
	appStats             *approuting.Statistics
	endpointPin          []string
	Name                 string
	Sink                 mobile.EventSink
}

func command(ctx context.Context, args ...string) ([]byte, error) {
	out, e := exec.CommandContext(ctx, "ip", args...).Output()
	if e != nil {
		return nil, errors.New("iproute2 operation failed")
	}
	return out, nil
}
func (c *Client) Start(ctx context.Context, cfg Config) (err error) {
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dev != nil {
		return errors.New("client already started")
	}
	if e := cfg.Validate(); e != nil {
		return e
	}
	policy, e := cfg.PrepareApplications()
	if e != nil {
		return e
	}
	c.appStats = nil
	c.requested = cloneConfig(cfg)
	c.recovery = (cfg.AppRouting != nil && cfg.AppRouting.Enabled) || cfg.HealthDNS != ""
	// Policy routing could redirect gateway sockets through unrelated tunnels.
	raw, e := command(ctx, "-j", "-4", "rule", "show")
	if e != nil {
		return e
	}
	var rules []struct {
		Priority int    `json:"priority"`
		Table    string `json:"table"`
	}
	if json.Unmarshal(raw, &rules) != nil {
		return errors.New("cannot inspect routing policy")
	}
	for _, r := range rules {
		if !(r.Priority == 0 && r.Table == "local" || r.Priority == 32766 && r.Table == "main" || r.Priority == 32767 && r.Table == "default") {
			return errors.New("custom IPv4 policy routing is not supported yet")
		}
	}
	raw, e = command(ctx, "-j", "-4", "route", "show", "table", "main")
	if e != nil {
		return e
	}
	if e = CheckRoutes(cfg.Targets, raw); e != nil {
		return e
	}
	mainRoutes := raw
	raw, e = command(ctx, "-j", "-4", "route", "get", cfg.endpointIP())
	if e != nil {
		return e
	}
	var routes []struct {
		Dev string `json:"dev"`
	}
	if json.Unmarshal(raw, &routes) != nil || len(routes) != 1 || routes[0].Dev == "" {
		return errors.New("no physical endpoint route")
	}
	if cfg.Interface == "" {
		cfg.Interface = routes[0].Dev
	} else if cfg.Interface != routes[0].Dev {
		return errors.New("configured interface differs from endpoint route")
	}
	if e = cfg.Validate(); e != nil {
		return e
	}
	var fullPlan defaultRoutePlan
	if cfg.FullIPv4() {
		fullPlan, e = planDefaultRoute(cfg.endpointIP(), mainRoutes, raw)
		if e != nil {
			return e
		}
	}
	nonce := make([]byte, 5)
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	c.Name = "ql" + hex.EncodeToString(nonce)
	f, e := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if e != nil {
		return errors.New("cannot open TUN")
	}
	c.dev = f
	defer func() {
		if err != nil {
			c.stopLocked()
		}
	}()
	req, e := unix.NewIfreq(c.Name)
	if e != nil {
		return e
	}
	req.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI | unix.IFF_TUN_EXCL)
	if e = unix.IoctlIfreq(int(f.Fd()), unix.TUNSETIFF, req); e != nil {
		return errors.New("cannot create exclusive TUN")
	}
	if _, e = command(ctx, "link", "set", "dev", c.Name, "mtu", "1280", "up"); e != nil {
		return e
	}
	payload, e := cfg.GatewayJSON()
	if e != nil {
		return e
	}
	c.gateway = mobile.NewGateway(c.Sink)
	if e = c.gateway.StartLinuxContext(ctx, payload, Binder(cfg.Interface)); e != nil {
		return errors.New("VPN transport startup failed")
	}
	if policy == nil {
		policy, _ = approuting.CompileLinuxTUN(approuting.TUNPolicy{Version: 2, Mode: approuting.OnlySelected})
	}
	c.router, e = mobile.NewDesktopAppRouter(policy, approuting.NewLinuxOwnerResolver(), Binder(cfg.Interface), c.gateway, c.Sink)
	if e == nil {
		e = c.router.AttachLinuxFD(int(f.Fd()))
	}
	if e != nil {
		return errors.New("cannot attach packet engine")
	}
	if cfg.FullIPv4() {
		if len(fullPlan.pin) != 0 {
			if _, e = command(ctx, append([]string{"-4", "route", "add"}, fullPlan.pin...)...); e != nil {
				return e
			}
			c.endpointPin = fullPlan.pin
		}
		for _, args := range fullPlan.captureArgs(c.Name) {
			if _, e = command(ctx, args...); e != nil {
				return e
			}
		}
	} else {
		for _, target := range cfg.Targets {
			if _, e = command(ctx, "-4", "route", "add", target, "dev", c.Name); e != nil {
				return e
			}
		}
	}
	c.effective = cloneConfig(cfg)
	c.startHealthLocked(ctx, c.gateway, cfg.HealthDNS, false)
	return nil
}
func (c *Client) stopLocked() {
	if c.health != nil {
		c.health.stop()
		c.health = nil
	}
	if c.vpnCancel != nil {
		c.vpnCancel()
		c.vpnCancel = nil
	}
	if c.router != nil {
		c.router.Close()
		s := c.router.Snapshot()
		c.appStats = &s
		c.router = nil
	}
	if c.gateway != nil {
		c.gateway.Stop()
		c.gateway = nil
	}
	if c.dev != nil {
		c.dev.Close()
		c.dev = nil
	}
	if len(c.endpointPin) != 0 {
		// The TUN/default is gone first. Match every attribute so an externally
		// replaced endpoint route is never removed by our cleanup.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		command(ctx, append([]string{"-4", "route", "del"}, c.endpointPin...)...)
		cancel()
		c.endpointPin = nil
	}
}
func (c *Client) Stop() {
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
}

func (c *Client) AppRoutingSnapshot() *approuting.Statistics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.router != nil {
		s := c.router.Snapshot()
		return &s
	}
	if c.appStats != nil {
		s := *c.appStats
		return &s
	}
	return nil
}

// PrepareApplications performs only local policy/path checks. Call before any
// TUN/route allocation; disabled stale selections remain usable configuration.
func (c Config) PrepareApplications() (*approuting.TUNCompiled, error) {
	p, e := approuting.CanonicalLinuxTUNPolicy(c.AppRouting)
	if e != nil {
		return nil, errors.New("application routing: executable unavailable or invalid policy")
	}
	if p == nil || !p.Enabled {
		return nil, nil
	}
	return approuting.CompileLinuxTUN(*p)
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gateway != nil && c.gateway.IsConnected() && (c.health == nil || c.health.snapshot().State != "unavailable")
}

func (c *Client) startHealthLocked(ctx context.Context, g *mobile.Gateway, resolver string, verified bool) {
	if resolver != "" {
		c.health = newHealthMonitor(ctx, func(ctx context.Context) error {
			return g.ProbeDesktopDNS(ctx, resolver)
		}, healthInterval, healthTimeout, verified)
	}
}

func (c *Client) Readiness() HealthStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dev == nil {
		return HealthStatus{State: "stopped"}
	}
	if c.health == nil {
		return HealthStatus{State: "engine_ready"}
	}
	return c.health.snapshot()
}
