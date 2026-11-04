// Package exitcontrol is the public, secret-free selection contract and the
// authenticated HTTPS client. It does not import server runtime providers.
package exitcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const MaxChoices = 32

var ErrConfig = errors.New("invalid server control credentials")
var ErrUnavailable = errors.New("server exit control unavailable; refresh before retrying")
var ErrConflict = errors.New("server exit revision changed; refresh choices")
var ErrDenied = errors.New("server exit not permitted")
var ErrAuth = errors.New("server control authentication expired")
var ErrUnsupported = errors.New("server does not support exit selection")
var exitID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var deviceID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var hexToken = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

type Config struct {
	ConfigURL string `json:"config_url"`
	DeviceID  string `json:"device_id"`
	Token     string `json:"update_token,omitempty"`
}

func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	v := *c
	return &v
}
func (c Config) Validate() error {
	u, err := url.Parse(c.ConfigURL)
	if err != nil || len(c.ConfigURL) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !deviceID.MatchString(c.DeviceID) || !hexToken.MatchString(c.Token) || u.Path != "/api/v1/devices/"+c.DeviceID+"/config" {
		return ErrConfig
	}
	return nil
}
func (c Config) Endpoint() (string, error) {
	if c.Validate() != nil {
		return "", ErrConfig
	}
	u, _ := url.Parse(c.ConfigURL)
	u.Path = strings.TrimSuffix(u.Path, "/config") + "/server-exit"
	return u.String(), nil
}

type Metadata struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Disabled bool   `json:"disabled,omitempty"`
}
type View struct {
	Selected  string     `json:"selected"`
	Effective string     `json:"effective"`
	Legacy    bool       `json:"legacy"`
	Blocked   bool       `json:"blocked"`
	Pending   bool       `json:"pending"`
	Revision  string     `json:"revision"`
	Choices   []Metadata `json:"choices"`
}
type Change struct {
	ID       string `json:"server_exit_id"`
	Revision string `json:"revision"`
}

func (c Change) Validate() error {
	if (c.ID != "" && !exitID.MatchString(c.ID)) || !hexToken.MatchString(c.Revision) {
		return ErrConfig
	}
	return nil
}
func (v View) Validate() error {
	if len(v.Choices) > MaxChoices || !hexToken.MatchString(v.Revision) || (v.Selected != "" && !exitID.MatchString(v.Selected)) {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, m := range v.Choices {
		if !exitID.MatchString(m.ID) || seen[m.ID] || m.Protocol != "awg" || len(m.Name) == 0 || len(m.Name) > 128 || strings.IndexFunc(m.Name, unicode.IsControl) >= 0 {
			return ErrUnavailable
		}
		seen[m.ID] = true
	}
	if v.Effective != "" && !seen[v.Effective] {
		return ErrUnavailable
	}
	if !v.Legacy && !v.Blocked && v.Effective == "" {
		return ErrUnavailable
	}
	return nil
}

type Client struct{ HTTP *http.Client }

func (c Client) Fetch(ctx context.Context, cfg Config) (View, error) { return c.request(ctx, cfg, nil) }
func (c Client) Select(ctx context.Context, cfg Config, change Change) (View, error) {
	if change.Validate() != nil {
		return View{}, ErrConfig
	}
	return c.request(ctx, cfg, &change)
}
func (c Client) request(ctx context.Context, cfg Config, change *Change) (View, error) {
	endpoint, err := cfg.Endpoint()
	if err != nil {
		return View{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	method := http.MethodGet
	var body io.Reader
	if change != nil {
		method = http.MethodPost
		raw, _ := json.Marshal(change)
		body = bytes.NewReader(raw)
	}
	r, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return View{}, ErrConfig
	}
	r.Header.Set("Authorization", "Bearer "+cfg.Token)
	r.Header.Set("Accept", "application/json")
	if change != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{Timeout: 15 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(r)
	if err != nil {
		return View{}, ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 401:
		return View{}, ErrAuth
	case 403:
		return View{}, ErrDenied
	case 404:
		return View{}, ErrUnsupported
	case 409:
		return View{}, ErrConflict
	default:
		return View{}, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return View{}, ErrUnavailable
	}
	var v View
	if json.Unmarshal(raw, &v) != nil || v.Validate() != nil {
		return View{}, ErrUnavailable
	}
	return v, nil
}
