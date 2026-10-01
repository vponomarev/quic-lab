package mobile

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"net"
	"net/http"
	"quiclab/internal/bond"
	"quiclab/internal/bondhttps"
	"quiclab/internal/gateway"
	"syscall"
	"time"
)

// Uses the already validated identity and explicit protected physical endpoint.
func (g *Gateway) addBondHTTPS(ctx context.Context, endpoint string, info bond.PathInfo, binder SocketBinder, create bool) error {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if binder != nil {
		dialer.Control = func(_, _ string, raw syscall.RawConn) error {
			var e error
			if err := raw.Control(func(fd uintptr) { e = binder.Bind(int64(fd)) }); err != nil {
				return err
			}
			return e
		}
	}
	tc := g.tls.Clone()
	tc.NextProtos = []string{"http/1.1"}
	tr := &http.Transport{TLSClientConfig: tc, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		c, e := dialer.DialContext(ctx, "tcp4", endpoint)
		if e != nil {
			return nil, e
		}
		return meterBoundConn(c, binder), nil
	}}
	_, port, e := net.SplitHostPort(endpoint)
	if e != nil {
		return e
	}
	handshake, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, resp, e := websocket.Dial(handshake, "wss://"+net.JoinHostPort(g.cfg.Hostname, port)+"/tunnel/bond", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Subprotocols: []string{bondhttps.Subprotocol}})
	if e != nil {
		tr.CloseIdleConnections()
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return e
	}
	fail := func(e error) error { c.CloseNow(); tr.CloseIdleConnections(); return e }
	if c.Subprotocol() != bondhttps.Subprotocol {
		return fail(errors.New("bond subprotocol required"))
	}
	c.SetReadLimit(4096)
	hello := gateway.BondHello{Token: g.bondToken, Create: create, PathID: info.ID, ProfileID: info.ProfileID, Network: info.Network, Generation: info.Generation, CopyBudget: g.cfg.BondCopyBudget, CellBudget: g.remainingBondBudget(), CellDisabled: g.bondCellBlocked}
	raw, e := json.Marshal(hello)
	if e != nil {
		return fail(e)
	}
	if e = c.Write(handshake, websocket.MessageText, raw); e != nil {
		return fail(e)
	}
	kind, raw, e := c.Read(handshake)
	if e != nil {
		return fail(e)
	}
	if kind != websocket.MessageText {
		return fail(errors.New("invalid bond welcome"))
	}
	var reply gateway.BondWelcome
	if e = json.Unmarshal(raw, &reply); e != nil {
		return fail(e)
	}
	if reply.Error != "" {
		return fail(errors.New(reply.Error))
	}
	if create {
		token, err := hex.DecodeString(reply.Token)
		if err != nil || len(token) != 32 {
			return fail(errors.New("invalid session token"))
		}
		g.bondToken = reply.Token
	}
	path := bondhttps.NewPath(c, tr.CloseIdleConnections)
	if e = g.bond.Session.AddNamedPath(info, path); e != nil {
		path.Close()
		return e
	}
	if g.bondBinders == nil {
		g.bondBinders = map[string]SocketBinder{}
	}
	g.bondBinders[info.ID] = binder
	return nil
}
