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
)

type Binder string

func (b Binder) Bind(fd int64) error {
	return unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, string(b))
}

// A single foreground owner; callers serialize Start/Stop. TUN is nonpersistent.
type Client struct {
	mu       sync.Mutex
	dev      *os.File
	gateway  *mobile.Gateway
	router   *mobile.DesktopAppRouter
	appStats *approuting.Statistics
	Name     string
	Sink     mobile.EventSink
}

func command(ctx context.Context, args ...string) ([]byte, error) {
	out, e := exec.CommandContext(ctx, "ip", args...).Output()
	if e != nil {
		return nil, errors.New("iproute2 operation failed")
	}
	return out, nil
}
func (c *Client) Start(ctx context.Context, cfg Config) (err error) {
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
	if policy != nil {
		c.router, e = mobile.NewDesktopAppRouter(policy, approuting.NewLinuxOwnerResolver(), Binder(cfg.Interface), c.gateway, c.Sink)
		if e == nil {
			e = c.router.AttachLinuxFD(int(f.Fd()))
		}
	} else {
		e = c.gateway.Attach(int(f.Fd()))
	}
	if e != nil {
		return errors.New("cannot attach packet engine")
	}
	for _, target := range cfg.Targets {
		if _, e = command(ctx, "-4", "route", "add", target, "dev", c.Name); e != nil {
			return e
		}
	}
	return nil
}
func (c *Client) stopLocked() {
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
}
func (c *Client) Stop() { c.mu.Lock(); defer c.mu.Unlock(); c.stopLocked() }

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
	return c.gateway != nil && c.gateway.IsConnected()
}
