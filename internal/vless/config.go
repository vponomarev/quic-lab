package vless

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/proxyman"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	account "github.com/xtls/xray-core/proxy/vless"
	outbound "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/reality"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
	"github.com/xtls/xray-core/transport/internet/websocket"
)

// Config is the restricted TCP/WebSocket client configuration, not an arbitrary Xray JSON
// configuration. Endpoint resolution belongs to the bound SocketFactory.
type Config struct {
	Endpoint         string
	Transport        string
	WebSocketPath    string
	WebSocketHost    string
	UUID             string
	Security         string
	ServerName       string
	Fingerprint      string
	Flow             string
	RealityPublicKey string
	ShortID          string
	SpiderX          string `json:",omitempty"`
	// RootPEM optionally supplies a private fixture trust root for TLS.
	RootPEM []byte
}

func buildConfig(c Config) (*core.Config, error) {
	host, portText, err := net.SplitHostPort(c.Endpoint)
	if err != nil || !validHost(host) {
		return nil, errors.New("invalid VLESS endpoint")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return nil, errors.New("invalid VLESS endpoint port")
	}
	if !validUUID(c.UUID) {
		return nil, errors.New("invalid VLESS UUID")
	}
	if !validHost(c.ServerName) || strings.EqualFold(c.ServerName, "frommitm") {
		return nil, errors.New("invalid VLESS server name")
	}
	if c.Flow != "" && c.Flow != "xtls-rprx-vision" {
		return nil, errors.New("unsupported VLESS flow")
	}
	transport := c.Transport
	if transport == "" {
		transport = "tcp"
	}
	if transport != "tcp" && transport != "ws" {
		return nil, errors.New("unsupported VLESS transport")
	}
	if transport == "ws" {
		if c.Security != "tls" || c.Flow != "" || c.RealityPublicKey != "" || c.ShortID != "" || c.SpiderX != "" {
			return nil, errors.New("invalid WebSocket VLESS options")
		}
		if c.WebSocketHost != "" && !validHost(c.WebSocketHost) {
			return nil, errors.New("invalid WebSocket host")
		}
		if _, err := normalizeWebSocketPath(c.WebSocketPath); err != nil {
			return nil, err
		}
	} else if c.WebSocketPath != "" || c.WebSocketHost != "" {
		return nil, errors.New("WebSocket settings require WebSocket transport")
	}
	switch c.Fingerprint {
	case "", "chrome", "firefox", "safari", "edge":
	default:
		return nil, errors.New("unsupported VLESS fingerprint")
	}
	var security *serial.TypedMessage
	switch c.Security {
	case "tls":
		if c.RealityPublicKey != "" || c.ShortID != "" || c.SpiderX != "" {
			return nil, errors.New("REALITY fields require REALITY security")
		}
		tls := &xtls.Config{ServerName: c.ServerName, Fingerprint: c.Fingerprint}
		if c.Flow != "" {
			tls.MinVersion = "1.3"
		}
		if len(c.RootPEM) > 0 {
			if !validRoots(c.RootPEM) {
				return nil, errors.New("invalid VLESS TLS root certificate")
			}
			tls.DisableSystemRoot = true
			tls.Certificate = []*xtls.Certificate{{Certificate: bytes.Clone(c.RootPEM), Usage: xtls.Certificate_AUTHORITY_VERIFY, OneTimeLoading: true}}
		}
		security = serial.ToTypedMessage(tls)
	case "reality":
		if len(c.RootPEM) > 0 || c.Fingerprint == "" {
			return nil, errors.New("invalid VLESS REALITY options")
		}
		key, err := base64.RawURLEncoding.Strict().DecodeString(c.RealityPublicKey)
		if err != nil || len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
			return nil, errors.New("invalid VLESS REALITY public key")
		}
		sid, err := hex.DecodeString(c.ShortID)
		if err != nil || len(sid) > 8 {
			return nil, errors.New("invalid VLESS REALITY short ID")
		}
		spider, err := NormalizeSpiderX(c.SpiderX)
		if err != nil {
			return nil, err
		}
		shortID := make([]byte, 8)
		copy(shortID, sid)
		security = serial.ToTypedMessage(&reality.Config{ServerName: c.ServerName, Fingerprint: c.Fingerprint, PublicKey: key, ShortId: shortID, SpiderX: spider, SpiderY: make([]int64, 10)})
	default:
		return nil, errors.New("unsupported VLESS security")
	}
	stream := &internet.StreamConfig{ProtocolName: transport, SecurityType: security.Type, SecuritySettings: []*serial.TypedMessage{security}}
	if transport == "ws" {
		path, _ := normalizeWebSocketPath(c.WebSocketPath)
		stream.ProtocolName = "websocket"
		stream.TransportSettings = []*internet.TransportConfig{{ProtocolName: "websocket", Settings: serial.ToTypedMessage(&websocket.Config{Path: path, Host: c.WebSocketHost})}}
	}
	return &core.Config{
		App: []*serial.TypedMessage{serial.ToTypedMessage(&dispatcher.Config{}), serial.ToTypedMessage(&proxyman.OutboundConfig{})},
		Outbound: []*core.OutboundHandlerConfig{{
			SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{StreamSettings: stream}),
			ProxySettings:  serial.ToTypedMessage(&outbound.Config{Vnext: &protocol.ServerEndpoint{Address: xnet.NewIPOrDomain(xnet.ParseAddress(host)), Port: uint32(port), User: &protocol.User{Account: serial.ToTypedMessage(&account.Account{Id: c.UUID, Flow: c.Flow, Encryption: "none"})}}}),
		}},
	}, nil
}

func validUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil
}

func validHost(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	if ip, err := netip.ParseAddr(s); err == nil {
		return !ip.IsUnspecified() && !ip.IsMulticast() && !ip.Is4In6() && ip.Zone() == ""
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, b := range []byte(label) {
			if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-') {
				return false
			}
		}
	}
	return true
}

func normalizeWebSocketPath(value string) (string, error) {
	if value == "" {
		return "/", nil
	}
	if len(value) > 2048 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\#\r\n\t") {
		return "", errors.New("invalid WebSocket path")
	}
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return "", errors.New("invalid WebSocket path")
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return "", errors.New("invalid WebSocket path")
		}
	}
	return value, nil
}

func validRoots(data []byte) bool {
	if len(data) > 64*1024 {
		return false
	}
	count := 0
	for len(bytes.TrimSpace(data)) > 0 {
		data = bytes.TrimSpace(data)
		if !bytes.HasPrefix(data, []byte("-----BEGIN CERTIFICATE-----")) {
			return false
		}
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) > 0 {
			return false
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return false
		}
		count++
		data = rest
	}
	return count > 0
}

// NormalizeSpiderX preserves escaped paths and query parameters without accepting an external URL.
func NormalizeSpiderX(value string) (string, error) {
	if value == "" {
		return "/", nil
	}
	bad := errors.New("invalid REALITY SpiderX path")
	if len(value) > 2048 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\#\r\n\t") {
		return "", bad
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return "", bad
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.Host != "" || u.Scheme != "" || u.Fragment != "" {
		return "", bad
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", bad
	}
	for _, r := range decoded {
		if r < 32 || r == 127 {
			return "", bad
		}
	}
	return value, nil
}

// ValidateConfig checks profile and trust syntax without starting a core or opening sockets.
func ValidateConfig(c Config) error { _, err := buildConfig(c); return err }
