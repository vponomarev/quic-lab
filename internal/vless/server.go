package vless

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"github.com/xtls/xray-core/app/proxyman"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/freedom"
	account "github.com/xtls/xray-core/proxy/vless"
	inbound "github.com/xtls/xray-core/proxy/vless/inbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/reality"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
	"google.golang.org/protobuf/proto"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxServerClients bounds configured credentials, independently of shared active admission.
const MaxServerClients = 512

type ServerClient struct{ UUID, Flow string }
type ServerOptions struct {
	AcceptProxyProtocol                   bool
	Listen, Security, Mode, DemuxEndpoint string
	Certificate, Key                      []byte
	RealityTarget                         string
	RealityServerNames                    []string
	RealityPrivateKey                     []byte
	RealityShortIDs                       []string
	Clients                               []ServerClient
	Admission                             func(context.Context, string) (time.Duration, error)
}
type serverContextKey struct{}
type admittedContextKey struct{}
type serverContext struct {
	gate         *deviceDispatcher
	mode, target string
	users        proxy.UserManager
}
type Server struct {
	engine  *engine
	gate    *deviceDispatcher
	once    sync.Once
	mu      sync.Mutex
	users   proxy.UserManager
	clients map[string]string
	err     error
}
type serverSockets struct{ net.Dialer }

func StartServer(ctx context.Context, o ServerOptions) (*Server, error) {
	fail := errors.New("invalid VLESS server options")
	addr, e := netip.ParseAddrPort(o.Listen)
	if e != nil || !addr.Addr().Is4() || addr.Addr().IsMulticast() || addr.Port() == 0 {
		return nil, fail
	}
	if o.AcceptProxyProtocol && !addr.Addr().IsLoopback() {
		return nil, fail
	}
	if o.Mode != "standalone" && o.Mode != "demux-only" {
		return nil, fail
	}
	if o.Mode == "demux-only" {
		d, e := netip.ParseAddrPort(o.DemuxEndpoint)
		if e != nil || !d.Addr().Is4() || d.Addr().IsUnspecified() || d.Addr().IsMulticast() || d.Port() == 0 {
			return nil, fail
		}
	} else if o.DemuxEndpoint != "" {
		return nil, fail
	}
	if len(o.Clients) > MaxServerClients {
		return nil, fail
	}
	clients := make([]*protocol.User, 0, len(o.Clients))
	seen := map[string]bool{}
	for _, c := range o.Clients {
		c.UUID = strings.ToLower(c.UUID)
		if !validUUID(c.UUID) || seen[c.UUID] || (c.Flow != "" && c.Flow != "xtls-rprx-vision") {
			return nil, fail
		}
		seen[c.UUID] = true
		clients = append(clients, &protocol.User{Email: c.UUID, Account: serial.ToTypedMessage(&account.Account{Id: c.UUID, Encryption: "none", Flow: c.Flow})})
	}
	var sec *serial.TypedMessage
	switch o.Security {
	case "tls":
		if len(o.RealityPrivateKey) > 0 || o.RealityTarget != "" || len(o.RealityServerNames) > 0 || len(o.RealityShortIDs) > 0 {
			return nil, fail
		}
		if _, e = tls.X509KeyPair(o.Certificate, o.Key); e != nil {
			return nil, fail
		}
		sec = serial.ToTypedMessage(&xtls.Config{MinVersion: "1.3", Certificate: []*xtls.Certificate{{Certificate: o.Certificate, Key: o.Key, OneTimeLoading: true}}})
	case "reality":
		if len(o.Certificate) > 0 || len(o.Key) > 0 || len(o.RealityPrivateKey) != 32 || len(o.RealityServerNames) == 0 || len(o.RealityShortIDs) == 0 {
			return nil, fail
		}
		host, port, e := net.SplitHostPort(o.RealityTarget)
		n, pe := strconv.ParseUint(port, 10, 16)
		if e != nil || pe != nil || n == 0 || !validHost(host) {
			return nil, fail
		}
		for _, name := range o.RealityServerNames {
			if !validHost(name) {
				return nil, fail
			}
		}
		var ids [][]byte
		for _, s := range o.RealityShortIDs {
			b, e := hex.DecodeString(s)
			if e != nil || len(b) > 8 {
				return nil, fail
			}
			p := make([]byte, 8)
			copy(p, b)
			ids = append(ids, p)
		}
		sec = serial.ToTypedMessage(&reality.Config{Dest: o.RealityTarget, Type: "tcp", ServerNames: o.RealityServerNames, PrivateKey: o.RealityPrivateKey, ShortIds: ids})
	default:
		return nil, fail
	}
	inner, e := proto.Marshal(&inbound.Config{Decryption: "none", Clients: clients})
	if e != nil {
		return nil, fail
	}
	gate := newDeviceDispatcher(nil)
	gate.admission = o.Admission
	sc := &serverContext{gate: gate, mode: o.Mode, target: o.DemuxEndpoint}
	ctx = context.WithValue(ctx, serverContextKey{}, sc)
	cfg := &core.Config{App: []*serial.TypedMessage{serial.ToTypedMessage(&ServerDispatcherConfig{}), serial.ToTypedMessage(&proxyman.OutboundConfig{}), serial.ToTypedMessage(&proxyman.InboundConfig{})},
		Inbound:  []*core.InboundHandlerConfig{{Tag: "vless-managed", ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{Listen: xnet.NewIPOrDomain(xnet.IPAddress(addr.Addr().AsSlice())), PortList: &xnet.PortList{Range: []*xnet.PortRange{{From: uint32(addr.Port()), To: uint32(addr.Port())}}}, StreamSettings: &internet.StreamConfig{ProtocolName: "tcp", SocketSettings: &internet.SocketConfig{AcceptProxyProtocol: o.AcceptProxyProtocol}, SecurityType: sec.Type, SecuritySettings: []*serial.TypedMessage{sec}}}), ProxySettings: serial.ToTypedMessage(&ServerInboundConfig{VlessConfig: inner})}},
		Outbound: []*core.OutboundHandlerConfig{{ProxySettings: serial.ToTypedMessage(&freedom.Config{})}}}
	eng, e := newEngine(ctx, cfg, &serverSockets{net.Dialer{Timeout: 5 * time.Second}})
	if e != nil {
		gate.Close()
		return nil, e
	}
	known := map[string]string{}
	for _, c := range o.Clients {
		known[strings.ToLower(c.UUID)] = c.Flow
	}
	return &Server{engine: eng, gate: gate, users: sc.users, clients: known}, nil
}
func (s *Server) Revoke(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokeLocked(strings.ToLower(id))
}
func (s *Server) Close() error {
	s.once.Do(func() { s.mu.Lock(); defer s.mu.Unlock(); s.gate.Close(); s.err = s.engine.Close() })
	return s.err
}
