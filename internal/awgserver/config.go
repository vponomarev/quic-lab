// Package awgserver defines the private provisioning contract for the AWG worker.
package awgserver

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Endpoint   string   `json:"endpoint"`
	Address    string   `json:"address"`
	DNS        string   `json:"dns"`
	AllowedIPs []string `json:"allowed_ips"`
	Interface  string   `json:"interface"`
	MTU        int      `json:"mtu"`
}

func (c Config) Validate() error {
	h, p, e := net.SplitHostPort(c.Endpoint)
	port, x := strconv.Atoi(p)
	if e != nil || x != nil || h == "" || strings.ContainsAny(h, "\r\n\t ;#") || port < 1 || port > 65535 {
		return errors.New("AWG endpoint must be host:port")
	}
	a, e := netip.ParsePrefix(c.Address)
	if e != nil || !a.Addr().Is4() || a.Bits() < 16 || a.Bits() > 29 || a.Addr() == a.Masked().Addr() || !a.Contains(a.Addr().Next()) {
		return errors.New("AWG address must be a host IPv4 /16../29")
	}
	if d, e := netip.ParseAddr(c.DNS); e != nil || !d.Is4() {
		return errors.New("AWG DNS must be IPv4")
	}
	if c.Interface != "ql-awg0" {
		return errors.New("AWG interface must be ql-awg0")
	}
	if c.MTU < 1280 || c.MTU > 1420 {
		return errors.New("AWG MTU must be 1280..1420")
	}
	if len(c.AllowedIPs) == 0 {
		return errors.New("AWG AllowedIPs required")
	}
	for _, v := range c.AllowedIPs {
		a, e := netip.ParsePrefix(v)
		if e != nil || !a.Addr().Is4() {
			return errors.New("AWG supports IPv4 AllowedIPs")
		}
	}
	return nil
}
func (c Config) Port() int {
	_, p, _ := net.SplitHostPort(c.Endpoint)
	n, _ := strconv.Atoi(p)
	return n
}

type Identity struct {
	Private string    `json:"private"`
	Public  string    `json:"public"`
	Headers [4]uint32 `json:"headers"`
}
type Peer struct {
	Private string `json:"private"`
	Public  string `json:"public"`
	PSK     string `json:"psk"`
	Address string `json:"address"`
}

func KeyPair() (string, string, error) {
	k, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		return "", "", e
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}
func NewIdentity() (*Identity, error) {
	a, b, e := KeyPair()
	if e != nil {
		return nil, e
	}
	v := &Identity{Private: a, Public: b}
	seen := map[uint32]bool{}
	for i := range v.Headers {
		for {
			var b [4]byte
			if _, e = rand.Read(b[:]); e != nil {
				return nil, e
			}
			n := binary.LittleEndian.Uint32(b[:])
			if n > 4 && !seen[n] {
				v.Headers[i] = n
				seen[n] = true
				break
			}
		}
	}
	return v, nil
}
func NewPeer(address string) (*Peer, error) {
	a, b, e := KeyPair()
	if e != nil {
		return nil, e
	}
	var key [32]byte
	if _, e = rand.Read(key[:]); e != nil {
		return nil, e
	}
	return &Peer{a, b, base64.StdEncoding.EncodeToString(key[:]), address}, nil
}
func KeyHex(key string) (string, error) {
	b, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(b) != 32 {
		return "", errors.New("invalid AWG key")
	}
	return hex.EncodeToString(b), nil
}
func (i Identity) Parameters() string {
	return fmt.Sprintf("jc=5\njmin=50\njmax=1000\ns1=134\ns2=90\nh1=%d\nh2=%d\nh3=%d\nh4=%d\n", i.Headers[0], i.Headers[1], i.Headers[2], i.Headers[3])
}
func (c Config) Client(i Identity, p Peer) string {
	return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\nDNS = %s\nMTU = %d\nJc = 5\nJmin = 50\nJmax = 1000\nS1 = 134\nS2 = 90\nH1 = %d\nH2 = %d\nH3 = %d\nH4 = %d\n\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s\nEndpoint = %s\nPersistentKeepalive = 25\n", p.Private, p.Address, c.DNS, c.MTU, i.Headers[0], i.Headers[1], i.Headers[2], i.Headers[3], i.Public, p.PSK, strings.Join(c.AllowedIPs, ", "), c.Endpoint)
}

// The worker reads the same atomic identity store as the admin, but only these fields.
type User struct {
	ID        string    `json:"id"`
	Disabled  bool      `json:"disabled"`
	Protocols []string  `json:"protocols"`
	Expires   time.Time `json:"expires"`
	AWG       *Peer     `json:"awg"`
}
type State struct {
	AWG   *Identity       `json:"awg"`
	Users map[string]User `json:"users"`
}

func Enabled(protocols []string, protocol string) bool {
	if protocols == nil {
		return protocol == "quic" || protocol == "https"
	}
	for _, p := range protocols {
		if p == protocol {
			return true
		}
	}
	return false
}
func (u User) Enabled(now time.Time) bool {
	return !u.Disabled && u.Expires.After(now) && Enabled(u.Protocols, "awg") && u.AWG != nil
}

type PeerStatus struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Handshake time.Time `json:"handshake"`
	Activity  time.Time `json:"activity"`
	TX        uint64    `json:"tx"`
	RX        uint64    `json:"rx"`
}
type Status struct {
	Started time.Time    `json:"started"`
	Updated time.Time    `json:"updated"`
	Peers   []PeerStatus `json:"peers"`
}

func WriteStatus(dir string, v Status) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".awg-status-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, filepath.Join(dir, "awg-status.json"))
}
