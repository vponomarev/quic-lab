package admin

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Profile struct {
	Version     int    `json:"version"`
	Kind        string `json:"kind"`
	Name        string `json:"name,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	QUIC        string `json:"quic,omitempty"`
	HTTPS       string `json:"https,omitempty"`
	Hostname    string `json:"hostname"`
	Pin         string `json:"pin,omitempty"`
	CA          string `json:"ca,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	Key         string `json:"key,omitempty"`
	DNS         string `json:"dns,omitempty"`
	Mode        int    `json:"mode,omitempty"`
	Routes      string `json:"routes,omitempty"`
}
type Config struct {
	Listen    string  `json:"listen"`
	PublicURL string  `json:"public_url"`
	Username  string  `json:"username"`
	Password  string  `json:"password"`
	DataDir   string  `json:"data_dir"`
	Echo      Profile `json:"echo"`
	VPN       Profile `json:"vpn"`
}

func ReadConfig(file string) (Config, error) {
	var c Config
	b, e := os.ReadFile(file)
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	u, e := url.Parse(c.PublicURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/") {
		return errors.New("public_url must be an HTTPS URL ending in /")
	}
	if c.Username == "" || len(c.Password) < 12 || c.Password == "CHANGE-ME-BEFORE-START" {
		return errors.New("set username and a unique password of at least 12 characters")
	}
	h, _, e := net.SplitHostPort(c.Listen)
	if e != nil || net.ParseIP(h) == nil || !net.ParseIP(h).IsLoopback() {
		return errors.New("admin listen must be loopback behind HTTPS reverse proxy")
	}
	if c.DataDir == "" {
		return errors.New("data_dir required")
	}
	for _, v := range []string{c.Echo.Endpoint, c.VPN.QUIC, c.VPN.HTTPS} {
		h, p, e := net.SplitHostPort(v)
		n, _ := strconv.Atoi(p)
		if e != nil || h == "" || n < 1 || n > 65535 {
			return errors.New("echo and VPN endpoints must be host:port")
		}
	}
	if c.Echo.Hostname == "" || c.VPN.Hostname == "" {
		return errors.New("TLS hostnames required")
	}
	if c.VPN.Mode != 0 && c.VPN.Mode != 3 {
		return errors.New("provisioning mode must be all apps (0) or routes (3)")
	}
	if c.Echo.Key != "" || c.Echo.Certificate != "" || c.VPN.Key != "" || c.VPN.Certificate != "" {
		return errors.New("identity material is generated per user, not stored in profiles")
	}
	return nil
}
