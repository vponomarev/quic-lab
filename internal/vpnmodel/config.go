// Package vpnmodel defines platform-independent VPN exits and connection profiles.
package vpnmodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Version  int       `json:"version"`
	Exits    []Exit    `json:"exits"`
	Profiles []Profile `json:"profiles"`
}
type Exit struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	DemuxID string `json:"demux_id,omitempty"`
}
type Profile struct {
	ID           string `json:"id"`
	ExitID       string `json:"exit_id"`
	Transport    string `json:"transport"`
	Mode         string `json:"mode"`
	Endpoint     string `json:"endpoint"`
	Priority     int    `json:"priority"`
	PoolSize     int    `json:"pool_size"`
	CheckReserve bool   `json:"check_reserve"`
}

// Parse accepts exactly one versioned configuration and fails closed on unknown fields.
func Parse(raw []byte) (Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("configuration JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Config{}, errors.New("expected one configuration document")
	}
	if err := Validate(c); err != nil {
		return Config{}, err
	}
	return c, nil
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func Validate(c Config) error {
	if c.Version != 1 {
		return errors.New("unsupported configuration version")
	}
	if len(c.Exits) == 0 || len(c.Profiles) == 0 {
		return errors.New("configuration must contain exits and profiles")
	}
	exits := make(map[string]Exit, len(c.Exits))
	for _, e := range c.Exits {
		if !validID.MatchString(e.ID) {
			return errors.New("invalid exit ID")
		}
		if _, ok := exits[e.ID]; ok {
			return errors.New("duplicate exit ID")
		}
		if strings.TrimSpace(e.Name) == "" {
			return errors.New("exit name is required")
		}
		if e.Kind != "demux" && e.Kind != "standalone" {
			return errors.New("unknown exit kind")
		}
		if e.Kind == "demux" && strings.TrimSpace(e.DemuxID) == "" {
			return errors.New("demux identity is required")
		}
		exits[e.ID] = e
	}
	seen := make(map[string]bool, len(c.Profiles))
	owners := make(map[string]int)
	for _, p := range c.Profiles {
		if !validID.MatchString(p.ID) || seen[p.ID] {
			return errors.New("invalid or duplicate profile ID")
		}
		seen[p.ID] = true
		e, ok := exits[p.ExitID]
		if !ok {
			return errors.New("profile references unknown exit")
		}
		owners[p.ExitID]++
		if p.Transport != "quic" && p.Transport != "https" && p.Transport != "awg" {
			return errors.New("unsupported transport")
		}
		if e.Kind == "demux" && p.Transport == "awg" {
			return errors.New("AWG underlay is not supported")
		}
		if p.Mode != "auto" && p.Mode != "reserve" && p.Mode != "disabled" {
			return errors.New("unknown profile mode")
		}
		if p.Priority < 0 {
			return errors.New("negative profile priority")
		}
		if p.PoolSize < 1 || p.PoolSize > 5 {
			return errors.New("pool size must be one, or two to five")
		}
		if e.Kind == "standalone" && p.PoolSize != 1 {
			return errors.New("standalone profiles cannot use carousel")
		}
		if p.Endpoint != "" || p.Mode != "disabled" {
			host, port, err := net.SplitHostPort(p.Endpoint)
			if err != nil || host == "" || strings.ContainsAny(host, " /\\\t\r\n") {
				return errors.New("invalid profile endpoint")
			}
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return errors.New("invalid endpoint port")
			}
			if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
				return errors.New("IPv6 endpoints are not supported")
			}
		}
	}
	for id, e := range exits {
		if owners[id] == 0 {
			return errors.New("exit has no profiles")
		}
		if e.Kind == "standalone" && owners[id] != 1 {
			return errors.New("standalone exit needs exactly one profile")
		}
	}
	return nil
}
