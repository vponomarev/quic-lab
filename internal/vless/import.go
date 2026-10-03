package vless

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ImportedProfile contains credentials. Serialize only into encrypted storage;
// it is not a display or logging DTO.
type ImportedProfile struct {
	Name   string `json:"name"`
	Config Config `json:"config"`
}

// ParseImport accepts at most 16 KiB of a vless:// URI or one standalone Xray
// VLESS outbound object. JSON permits only tag/protocol/settings(vnext with one
// address, port and user id/encryption/flow)/streamSettings(network, security,
// tlsSettings or realitySettings). Security is TLS or REALITY over TCP (including its RAW alias) only.
// Missing URI type and encryption mean tcp and none. SNI is always explicit.
// Full configs, subscriptions, routing, chaining, custom roots and extensions
// are rejected. Parsing does not resolve names, fetch URLs or open sockets.
func ParseImport(raw string) (ImportedProfile, error) {
	fail := func() (ImportedProfile, error) {
		return ImportedProfile{}, errors.New("invalid or unsupported VLESS import")
	}
	if len(raw) == 0 || len(raw) > 16*1024 || !utf8.ValidString(raw) {
		return fail()
	}
	raw = strings.TrimSpace(raw)
	var p ImportedProfile
	var err error
	if strings.HasPrefix(raw, "vless://") {
		p, err = parseImportURI(raw)
	} else if strings.HasPrefix(raw, "{") {
		p, err = parseImportJSON(raw)
	} else {
		return fail()
	}
	if err != nil || len(p.Name) > 256 || !utf8.ValidString(p.Name) || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
		return fail()
	}
	if _, err = buildConfig(p.Config); err != nil {
		return fail()
	}
	return p, nil
}

func parseImportURI(raw string) (ImportedProfile, error) {
	var p ImportedProfile
	invalid := errors.New("invalid VLESS URI")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "vless" || u.Opaque != "" || u.User == nil || u.Path != "" || u.ForceQuery {
		return p, invalid
	}
	if _, ok := u.User.Password(); ok {
		return p, invalid
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return p, invalid
	}
	for key, values := range q {
		if len(values) != 1 {
			return p, invalid
		}
		switch key {
		case "encryption", "security", "type", "sni", "fp", "flow", "pbk", "sid", "spx":
		default:
			return p, invalid
		}
	}
	if (q.Has("type") && q.Get("type") != "tcp" && q.Get("type") != "raw") || (q.Has("encryption") && q.Get("encryption") != "none") {
		return p, invalid
	}
	// The adapter already uses SpiderX="/". Accept this explicit default only; do not silently discard a different crawl path.
	if q.Has("spx") && (q.Get("security") != "reality" || q.Get("spx") != "/") {
		return p, invalid
	}
	// Explicit REALITY query fields under TLS are rejected even when empty.
	if q.Get("security") != "reality" && (q.Has("pbk") || q.Has("sid")) {
		return p, invalid
	}
	p.Name = u.Fragment
	p.Config = Config{Endpoint: u.Host, UUID: u.User.Username(), Security: q.Get("security"), ServerName: q.Get("sni"), Fingerprint: q.Get("fp"), Flow: q.Get("flow"), RealityPublicKey: q.Get("pbk"), ShortID: q.Get("sid")}
	return p, nil
}

type importUser struct {
	ID         string `json:"id"`
	Encryption string `json:"encryption"`
	Flow       string `json:"flow"`
}
type importServer struct {
	Address string       `json:"address"`
	Port    uint16       `json:"port"`
	Users   []importUser `json:"users"`
}
type importTLS struct {
	ServerName  string `json:"serverName"`
	Fingerprint string `json:"fingerprint"`
}
type importReality struct {
	ServerName  string `json:"serverName"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	ShortID     string `json:"shortId"`
}
type importObject struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
	Settings struct {
		Vnext []importServer `json:"vnext"`
	} `json:"settings"`
	Stream struct {
		Network  string         `json:"network"`
		Security string         `json:"security"`
		TLS      *importTLS     `json:"tlsSettings"`
		Reality  *importReality `json:"realitySettings"`
	} `json:"streamSettings"`
}

func parseImportJSON(raw string) (ImportedProfile, error) {
	var p ImportedProfile
	invalid := errors.New("invalid VLESS outbound")
	// Token walk rejects duplicate keys, excessive nesting, case aliases and null
	// before Go's decoder can silently replace earlier values or ignore null.
	d := json.NewDecoder(strings.NewReader(raw))
	if err := scanImportJSON(d, 0); err != nil {
		return p, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return p, invalid
	}
	var o importObject
	d = json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&o); err != nil {
		return p, invalid
	}
	if o.Protocol != "vless" || (o.Stream.Network != "tcp" && o.Stream.Network != "raw") || len(o.Settings.Vnext) != 1 {
		return p, invalid
	}
	server := o.Settings.Vnext[0]
	if len(server.Users) != 1 || server.Users[0].Encryption != "none" {
		return p, invalid
	}
	p.Name = o.Tag
	p.Config = Config{Endpoint: net.JoinHostPort(server.Address, strconv.Itoa(int(server.Port))), UUID: server.Users[0].ID, Flow: server.Users[0].Flow, Security: o.Stream.Security}
	switch o.Stream.Security {
	case "tls":
		if o.Stream.TLS == nil || o.Stream.Reality != nil {
			return p, invalid
		}
		p.Config.ServerName = o.Stream.TLS.ServerName
		p.Config.Fingerprint = o.Stream.TLS.Fingerprint
	case "reality":
		if o.Stream.Reality == nil || o.Stream.TLS != nil {
			return p, invalid
		}
		p.Config.ServerName = o.Stream.Reality.ServerName
		p.Config.Fingerprint = o.Stream.Reality.Fingerprint
		p.Config.RealityPublicKey = o.Stream.Reality.PublicKey
		p.Config.ShortID = o.Stream.Reality.ShortID
	default:
		return p, invalid
	}
	return p, nil
}

func scanImportJSON(d *json.Decoder, depth int) error {
	invalid := errors.New("invalid JSON import")
	if depth > 12 {
		return invalid
	}
	token, err := d.Token()
	if err != nil {
		return invalid
	}
	if token == nil {
		return invalid
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return invalid
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return invalid
			}
			seen[key] = true
			switch key {
			case "tag", "protocol", "settings", "vnext", "address", "port", "users", "id", "encryption", "flow", "streamSettings", "network", "security", "tlsSettings", "realitySettings", "serverName", "fingerprint", "publicKey", "shortId":
			default:
				return invalid
			}
			if err := scanImportJSON(d, depth+1); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim('}') {
			return invalid
		}
	case '[':
		for d.More() {
			if err := scanImportJSON(d, depth+1); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim(']') {
			return invalid
		}
	default:
		return invalid
	}
	return nil
}
