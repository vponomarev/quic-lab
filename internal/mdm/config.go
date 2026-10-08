package mdm

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"quiclab/internal/awg"
	"quiclab/internal/vless"
)

// ConfigurationDocument is a complete desired application configuration, not a
// map of SharedPreferences or Android permissions. Credentials are never logged.
type ConfigurationDocument struct {
	Schema            int                      `json:"schema"`
	Profiles          []ConfigurationProfile   `json:"profiles"`
	CurrentProfileID  string                   `json:"currentProfileId"`
	EnabledProfileIDs []string                 `json:"enabledProfileIds"`
	Multiple          bool                     `json:"multiple"`
	GlobalApps        []string                 `json:"globalApps"`
	DNS               ConfigurationDNS         `json:"dns"`
	Budget            ConfigurationBudget      `json:"budget"`
	Diagnostics       ConfigurationDiagnostics `json:"diagnostics"`
}
type ConfigurationProfile struct {
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	Settings map[string]json.RawMessage `json:"settings"`
	// Omission preserves credentials for this ID. Explicit removal is separate.
	Identity       *ConfigurationIdentity `json:"identity,omitempty"`
	RemoveIdentity bool                   `json:"removeIdentity,omitempty"`
}
type ConfigurationIdentity struct {
	Certificate string `json:"certificate,omitempty"`
	Key         string `json:"key,omitempty"`
	AWG         string `json:"awg_config,omitempty"`
	VLESS       string `json:"vless_uri,omitempty"`
}
type ConfigurationDNS struct {
	Mode      string `json:"mode"`
	ProfileID string `json:"profileId"`
}
type ConfigurationBudget struct {
	LimitBytes int64 `json:"limitBytes"`
}
type ConfigurationDiagnostics struct {
	Enabled  bool `json:"enabled"`
	Detailed bool `json:"detailed"`
}

var profileID = regexp.MustCompile(`^(default|[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})$`)
var hostName = regexp.MustCompile(`^[a-zA-Z0-9.-]{1,253}$`)
var appName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)

// ValidateDocument is also used by the native Android adapter. Errors deliberately
// carry no field values (a rejected document can contain keys).
func ValidateDocument(raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) {
		return ErrInvalid
	}
	// encoding/json otherwise accepts duplicate keys and null for scalar fields.
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueValue(tokens, 0); err != nil {
		return ErrInvalid
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d ConfigurationDocument
	if dec.Decode(&d) != nil || d.Schema != 1 || len(d.Profiles) == 0 || len(d.Profiles) > 32 {
		return ErrInvalid
	}
	// Require a full document, so a missing false/zero does not silently reset a
	// setting. Individual profile settings may be omitted for application defaults.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ErrInvalid
	}
	for _, k := range []string{"schema", "profiles", "currentProfileId", "enabledProfileIds", "multiple", "globalApps", "dns", "budget", "diagnostics"} {
		if _, ok := fields[k]; !ok {
			return ErrInvalid
		}
	}
	for key, required := range map[string][]string{"dns": {"mode", "profileId"}, "budget": {"limitBytes"}, "diagnostics": {"enabled", "detailed"}} {
		var m map[string]json.RawMessage
		if json.Unmarshal(fields[key], &m) != nil {
			return ErrInvalid
		}
		for _, k := range required {
			if _, ok := m[k]; !ok {
				return ErrInvalid
			}
		}
	}
	ids := map[string]bool{}
	for _, p := range d.Profiles {
		if !profileID.MatchString(p.ID) || ids[p.ID] || !displayName(p.Name, 100) || p.Settings == nil {
			return ErrInvalid
		}
		ids[p.ID] = true
		if validateSettings(p.Settings) != nil {
			return ErrInvalid
		}
		if p.RemoveIdentity && p.Identity != nil {
			return ErrInvalid
		}
		if p.Identity != nil {
			i := p.Identity
			if i.Certificate == "" && i.Key == "" && i.AWG == "" && i.VLESS == "" {
				return ErrInvalid
			}
			if i.Certificate != "" || i.Key != "" {
				if _, e := tls.X509KeyPair([]byte(i.Certificate), []byte(i.Key)); e != nil {
					return ErrInvalid
				}
			}
			if i.AWG != "" {
				if _, e := awg.Parse(i.AWG); e != nil {
					return ErrInvalid
				}
			}
			if i.VLESS != "" {
				if !strings.HasPrefix(i.VLESS, "vless://") {
					return ErrInvalid
				}
				if _, e := vless.ParseImport(i.VLESS); e != nil {
					return ErrInvalid
				}
			}
		}
	}
	if !ids[d.CurrentProfileID] || !ids[d.DNS.ProfileID] || !oneOf(d.DNS.Mode, "system", "tunnel") || d.Budget.LimitBytes < 0 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range d.EnabledProfileIDs {
		if !ids[id] || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	if d.Multiple && len(seen) == 0 {
		return ErrInvalid
	}
	if !apps(d.GlobalApps) {
		return ErrInvalid
	}
	return nil
}
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func displayName(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= max && strings.IndexFunc(s, unicode.IsControl) < 0
}
func apps(a []string) bool {
	if len(a) > 2048 {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		if len(s) > 255 || !appName.MatchString(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func validateSettings(m map[string]json.RawMessage) error {
	for k, r := range m {
		switch k {
		case "max_availability", "demux_enabled", "quic_check_reserve", "https_check_reserve", "global_apps":
			var v bool
			if json.Unmarshal(r, &v) != nil {
				return ErrInvalid
			}
		case "quic_pool_size", "https_pool_size", "data_version", "bond_copy_budget", "bond_cell_budget", "mode", "bond_copy_kib", "cell_mib":
			var n int64
			if json.Unmarshal(r, &n) != nil || n < 0 {
				return ErrInvalid
			}
			if (k == "quic_pool_size" || k == "https_pool_size") && (n < 1 || n > 5) {
				return ErrInvalid
			}
			if k == "mode" && n > 3 {
				return ErrInvalid
			}
			if k == "bond_copy_kib" && n > 65536 {
				return ErrInvalid
			}
			if k == "cell_mib" && n > 8796093022207 {
				return ErrInvalid
			}
			if k == "data_version" && (n < 1 || n > 2147483647) {
				return ErrInvalid
			}
		case "apps", "available_transports":
			var a []string
			if json.Unmarshal(r, &a) != nil {
				return ErrInvalid
			}
			if k == "apps" && !apps(a) {
				return ErrInvalid
			}
			if k == "available_transports" {
				seen := map[string]bool{}
				if len(a) == 0 || len(a) > 4 {
					return ErrInvalid
				}
				for _, s := range a {
					if !oneOf(s, "quic", "https", "awg", "vless") || seen[s] {
						return ErrInvalid
					}
					seen[s] = true
				}
			}
		case "transport", "endpoint", "quic_endpoint", "https_endpoint", "awg_endpoint", "vless_endpoint",
			"hostname", "dns", "routes", "ca", "server_name", "verify_name", "control_url", "transit_endpoint", "quic_mode", "https_mode":
			var s string
			if json.Unmarshal(r, &s) != nil || len(s) > 65536 || strings.ContainsRune(s, 0) {
				return ErrInvalid
			}
			switch k {
			case "transport":
				if !oneOf(s, "quic", "https", "awg", "vless") {
					return ErrInvalid
				}
			case "quic_mode", "https_mode":
				if !oneOf(s, "auto", "reserve", "disabled") {
					return ErrInvalid
				}
			case "endpoint", "quic_endpoint", "https_endpoint", "awg_endpoint", "vless_endpoint", "transit_endpoint":
				if s != "" {
					h, p, e := net.SplitHostPort(s)
					n, e2 := strconv.Atoi(p)
					if e != nil || e2 != nil || !hostName.MatchString(h) || n < 1 || n > 65535 {
						return ErrInvalid
					}
				}
			case "hostname", "server_name", "verify_name":
				if s != "" && !hostName.MatchString(s) {
					return ErrInvalid
				}
			case "control_url":
				if s != "" {
					u, e := url.Parse(s)
					if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
						return ErrInvalid
					}
				}
			case "routes":
				for _, r := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' }) {
					a, e := netip.ParsePrefix(r)
					if e != nil || !a.Addr().Is4() {
						return ErrInvalid
					}
				}
			case "dns":
				if s != "" {
					a, e := netip.ParseAddr(s)
					if e != nil || !a.Is4() {
						return ErrInvalid
					}
				}
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

// Walk before typed decoding: reject duplicates, null values, and excessive
// nesting uniformly, including optional fields and nested settings.
func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalid
	}
	t, e := d.Token()
	if e != nil || t == nil {
		return ErrInvalid
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			s, ok := k.(string)
			if e != nil || !ok || seen[s] {
				return ErrInvalid
			}
			seen[s] = true
			if uniqueValue(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		t, e = d.Token()
		if e != nil || t != json.Delim('}') {
			return ErrInvalid
		}
	case json.Delim('['):
		for d.More() {
			if uniqueValue(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		t, e = d.Token()
		if e != nil || t != json.Delim(']') {
			return ErrInvalid
		}
	}
	return nil
}
