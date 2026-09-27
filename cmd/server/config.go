package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

type serverConfig struct {
	Listen        string `json:"listen"`
	Cert          string `json:"cert"`
	Key           string `json:"key"`
	EphemeralCert bool   `json:"ephemeral_cert"`
	TLSFallback   string `json:"tls_fallback"`
	TLSHost       string `json:"tls_host"`
	HTTPSListen   string `json:"https_listen"`
	WebListen     string `json:"web_listen"`
	GatewayQUIC   string `json:"gateway_quic"`
	GatewayHTTPS  string `json:"gateway_https"`
	ClientCA      string `json:"client_ca"`
	GatewayAllow  string `json:"gateway_allow"`
	DemoListen    string `json:"demo_listen"`
	AdminConfig   string `json:"admin_config"`
}

func parseServerConfig(args []string) (serverConfig, error) {
	c := serverConfig{Listen: "127.0.0.1:4433"}
	fs := flag.NewFlagSet("quic-lab-server", flag.ContinueOnError)
	path := fs.String("config", "", "server JSON configuration; explicit CLI flags override file values")
	check := fs.Bool("check-config", false, "validate configuration without opening listeners")
	fs.StringVar(&c.Listen, "listen", c.Listen, "Echo QUIC UDP listen address")
	fs.StringVar(&c.Cert, "cert", "", "PEM certificate file")
	fs.StringVar(&c.Key, "key", "", "PEM private key file")
	fs.BoolVar(&c.EphemeralCert, "ephemeral-cert", false, "in-memory lab certificate")
	fs.StringVar(&c.TLSFallback, "tls-fallback", "", "TLS passthrough backend host:port for other SNI names")
	fs.StringVar(&c.TLSHost, "tls-host", "", "SNI hostname terminated locally")
	fs.StringVar(&c.HTTPSListen, "https-listen", "", "public HTTPS listen address")
	fs.StringVar(&c.WebListen, "web-listen", "", "HTTP echo backend listen address")
	fs.StringVar(&c.GatewayQUIC, "gateway-quic", "", "VPN QUIC listen address")
	fs.StringVar(&c.GatewayHTTPS, "gateway-https", "", "VPN HTTPS/mTLS listen address")
	fs.StringVar(&c.ClientCA, "client-ca", "", "trusted client CA PEM")
	fs.StringVar(&c.GatewayAllow, "gateway-allow", "", "comma-separated IPv4 destination CIDRs")
	fs.StringVar(&c.DemoListen, "demo-listen", "", "HTTP demo backend listen address")
	fs.StringVar(&c.AdminConfig, "admin-config", "", "admin JSON configuration")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		return c, err
	}
	if fs.NArg() != 0 {
		return c, errors.New("unexpected positional arguments")
	}
	overrides := map[string]string{}
	fs.Visit(func(f *flag.Flag) { overrides[f.Name] = f.Value.String() })
	if *path != "" {
		data, err := os.ReadFile(*path)
		if err != nil {
			return c, err
		}
		c = serverConfig{Listen: "127.0.0.1:4433"}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&c); err != nil {
			return c, fmt.Errorf("server config: %w", err)
		}
		if dec.Decode(new(any)) != io.EOF {
			return c, errors.New("server config: trailing JSON data")
		}
		for name, value := range overrides {
			if err = fs.Set(name, value); err != nil {
				return c, err
			}
		}
	}
	// Only expand the systemd credential directory, never arbitrary environment values.
	for _, p := range []*string{&c.Cert, &c.Key, &c.ClientCA, &c.AdminConfig} {
		if strings.Contains(*p, "${CREDENTIALS_DIRECTORY}") {
			dir := os.Getenv("CREDENTIALS_DIRECTORY")
			if dir == "" {
				return c, errors.New("CREDENTIALS_DIRECTORY is not set")
			}
			*p = strings.ReplaceAll(*p, "${CREDENTIALS_DIRECTORY}", dir)
		}
	}
	if err := c.validate(); err != nil {
		return c, err
	}
	if *check {
		fmt.Println("Server configuration is valid (syntax and settings; files and listeners are checked at startup).")
		os.Exit(0)
	}
	return c, nil
}

func validateAddress(value string, backend bool) error {
	h, p, err := net.SplitHostPort(value)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("port must be 1..65535")
	}
	if backend && (h == "" || strings.ContainsAny(h, " /\\") || (net.ParseIP(h) != nil && net.ParseIP(h).IsUnspecified())) {
		return errors.New("backend requires a concrete IP or DNS hostname")
	}
	return nil
}

func (c serverConfig) validate() error {
	for name, addr := range map[string]string{"listen": c.Listen, "https_listen": c.HTTPSListen, "web_listen": c.WebListen, "gateway_quic": c.GatewayQUIC, "gateway_https": c.GatewayHTTPS, "demo_listen": c.DemoListen} {
		if addr != "" {
			if err := validateAddress(addr, false); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	if c.Listen == "" {
		return errors.New("listen cannot be empty")
	}
	if c.EphemeralCert {
		if c.Cert != "" || c.Key != "" {
			return errors.New("ephemeral_cert conflicts with cert/key")
		}
	} else if c.Cert == "" || c.Key == "" {
		return errors.New("cert and key are required")
	}
	if c.TLSFallback != "" {
		if c.HTTPSListen == "" || c.TLSHost == "" || strings.ContainsAny(c.TLSHost, "/ :") {
			return errors.New("tls_fallback requires https_listen and a valid tls_host")
		}
		if err := validateAddress(c.TLSFallback, true); err != nil {
			return fmt.Errorf("tls_fallback: %w", err)
		}
		if c.TLSFallback == c.HTTPSListen {
			return errors.New("tls_fallback points to the public listener")
		}
	}
	if c.GatewayQUIC != "" || c.GatewayHTTPS != "" {
		if c.GatewayAllow == "" {
			return errors.New("gateway_allow is required")
		}
		for _, cidr := range strings.Split(c.GatewayAllow, ",") {
			ip, _, err := net.ParseCIDR(strings.TrimSpace(cidr))
			if err != nil || ip.To4() == nil {
				return errors.New("gateway_allow requires IPv4 CIDRs")
			}
		}
		if c.ClientCA == "" && c.AdminConfig == "" {
			return errors.New("gateway requires client_ca or admin_config")
		}
	}
	return nil
}
