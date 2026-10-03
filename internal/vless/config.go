package vless

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net"
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
)

// Config is the restricted TCP client configuration, not an arbitrary Xray JSON
// configuration. Endpoint resolution belongs to the bound SocketFactory.
type Config struct {
	Endpoint         string
	UUID             string
	Security         string
	ServerName       string
	Fingerprint      string
	Flow             string
	RealityPublicKey string
	ShortID          string
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
	switch c.Fingerprint {
	case "", "chrome", "firefox", "safari":
	default:
		return nil, errors.New("unsupported VLESS fingerprint")
	}
	var security *serial.TypedMessage
	switch c.Security {
	case "tls":
		if c.RealityPublicKey != "" || c.ShortID != "" {
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
		shortID := make([]byte, 8)
		copy(shortID, sid)
		security = serial.ToTypedMessage(&reality.Config{ServerName: c.ServerName, Fingerprint: c.Fingerprint, PublicKey: key, ShortId: shortID, SpiderX: "/", SpiderY: make([]int64, 10)})
	default:
		return nil, errors.New("unsupported VLESS security")
	}
	return &core.Config{
		App: []*serial.TypedMessage{serial.ToTypedMessage(&dispatcher.Config{}), serial.ToTypedMessage(&proxyman.OutboundConfig{})},
		Outbound: []*core.OutboundHandlerConfig{{
			SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{StreamSettings: &internet.StreamConfig{ProtocolName: "tcp", SecurityType: security.Type, SecuritySettings: []*serial.TypedMessage{security}}}),
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
	if ip := net.ParseIP(s); ip != nil {
		return ip.To4() != nil
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
