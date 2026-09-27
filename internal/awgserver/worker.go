package awgserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Worker struct {
	lastProbe map[string]time.Time
	Device    *device.Device
	Config    Config
	Dir       string
	peers     map[string]string
	activity  map[string]PeerStatus
	identity  string
	started   time.Time
}

func (w *Worker) Apply(state State) error {
	if state.AWG == nil {
		return errors.New("AWG identity has not been provisioned")
	}
	if w.peers == nil {
		w.peers = map[string]string{}
		w.activity = map[string]PeerStatus{}
		w.started = time.Now().UTC()
	}
	if w.identity == "" {
		key, e := KeyHex(state.AWG.Private)
		if e != nil {
			return e
		}
		if e = w.Device.IpcSet(fmt.Sprintf("private_key=%s\nlisten_port=%d\n%s", key, w.Config.Port(), state.AWG.Parameters())); e != nil {
			return errors.New("AWG device configuration rejected")
		}
		w.identity = state.AWG.Private
	} else if w.identity != state.AWG.Private {
		return errors.New("AWG server identity changed; restart worker")
	}
	now := time.Now()
	desired := map[string]string{}
	prefix, _ := netip.ParsePrefix(w.Config.Address)
	used := map[string]bool{}
	for _, u := range state.Users {
		if !u.Enabled(now) {
			continue
		}
		p := u.AWG
		ip, e := netip.ParseAddr(p.Address)
		if e != nil || !prefix.Contains(ip) || ip == prefix.Addr() || ip == prefix.Masked().Addr() || !prefix.Contains(ip.Next()) || used[p.Address] {
			return errors.New("invalid or duplicate AWG peer address")
		}
		used[p.Address] = true
		pub, e := KeyHex(p.Public)
		if e != nil {
			return e
		}
		psk, e := KeyHex(p.PSK)
		if e != nil {
			return e
		}
		if _, ok := desired[pub]; ok {
			return errors.New("duplicate AWG public key")
		}
		desired[pub] = fmt.Sprintf("public_key=%s\npreshared_key=%s\nreplace_allowed_ips=true\nallowed_ip=%s/32\n", pub, psk, p.Address)
	}
	// Remove only revoked peers; replacing the entire peer set would interrupt others.
	for pub := range w.peers {
		if _, ok := desired[pub]; !ok {
			if e := w.Device.IpcSet("public_key=" + pub + "\nremove=true\n"); e != nil {
				return errors.New("cannot revoke AWG peer")
			}
			delete(w.peers, pub)
		}
	}
	for pub, ipc := range desired {
		if w.peers[pub] != ipc {
			if e := w.Device.IpcSet(ipc); e != nil {
				return errors.New("cannot apply AWG peer")
			}
			w.peers[pub] = ipc
		}
	}
	return nil
}
func (w *Worker) Snapshot(state State) (Status, error) {
	raw, e := w.Device.IpcGet()
	if e != nil {
		return Status{}, errors.New("cannot read AWG runtime")
	}
	ids := map[string]string{}
	for _, u := range state.Users {
		if u.Enabled(time.Now()) {
			pub, _ := KeyHex(u.AWG.Public)
			ids[pub] = u.ID
		}
	}
	now := time.Now().UTC()
	out := Status{Started: w.started, Updated: now}
	var peer *PeerStatus
	flush := func() {
		if peer == nil || peer.ID == "" {
			return
		}
		previous := w.activity[peer.ID]
		peer.Activity = confirmedActivity(previous, *peer, now)
		w.activity[peer.ID] = *peer
		out.Peers = append(out.Peers, *peer)
	}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if k == "public_key" {
			flush()
			peer = &PeerStatus{ID: ids[v]}
			continue
		}
		if peer == nil {
			continue
		}
		switch k {
		case "endpoint":
			peer.Source = v
		case "rx_bytes":
			peer.TX, _ = strconv.ParseUint(v, 10, 64)
		case "tx_bytes":
			peer.RX, _ = strconv.ParseUint(v, 10, 64)
		case "last_handshake_time_sec":
			n, _ := strconv.ParseInt(v, 10, 64)
			if n > 0 {
				peer.Handshake = time.Unix(n, 0).UTC()
			}
		}
	}
	flush()
	for id := range w.activity {
		found := false
		for _, p := range out.Peers {
			found = found || p.ID == id
		}
		if !found {
			delete(w.activity, id)
		}
	}
	return out, nil
}
func (w *Worker) Tick() error {
	raw, e := os.ReadFile(filepath.Join(w.Dir, "identities.json"))
	if e != nil {
		return errors.New("cannot read AWG identity store")
	}
	var state State
	if json.Unmarshal(raw, &state) != nil {
		return errors.New("invalid AWG identity store")
	}
	if e = w.Apply(state); e != nil {
		return e
	}
	status, e := w.Snapshot(state)
	if e != nil {
		return e
	}
	w.probeIdle(state, status)
	return WriteStatus(w.Dir, status)
}
func (w *Worker) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if e := w.Tick(); e != nil {
				return e
			}
		}
	}
}

// TX is client-to-server (the device rx_bytes counter). Server sends are not
// evidence of a reachable peer. Initial snapshots must not revive old traffic.
func confirmedActivity(previous, current PeerStatus, now time.Time) time.Time {
	seen := previous.Activity
	if current.Handshake.After(seen) {
		seen = current.Handshake
	}
	if previous.ID != "" && current.TX > previous.TX {
		seen = now
	}
	return seen
}
func (w *Worker) probeIdle(state State, status Status) {
	if w.lastProbe == nil {
		w.lastProbe = map[string]time.Time{}
	}
	present := map[string]bool{}
	for _, p := range status.Peers {
		present[p.ID] = true
		if p.Source == "" || status.Updated.Sub(p.Activity) < w.Config.HealthInterval() || status.Updated.Sub(w.lastProbe[p.ID]) < w.Config.HealthInterval() {
			continue
		}
		u, ok := state.Users[p.ID]
		if !ok || u.AWG == nil || !u.Enabled(status.Updated) {
			continue
		}
		raw, e := base64.StdEncoding.DecodeString(u.AWG.Public)
		if e != nil || len(raw) != 32 {
			continue
		}
		var key device.NoisePublicKey
		copy(key[:], raw)
		if peer := w.Device.LookupPeer(key); peer != nil {
			w.lastProbe[p.ID] = status.Updated
			// Standard authenticated handshake, not an ICMP dependency. A failed send
			// never refreshes Activity or disconnects another live transport.
			_ = peer.SendHandshakeInitiation(false)
		}
	}
	for id := range w.lastProbe {
		if !present[id] {
			delete(w.lastProbe, id)
		}
	}
}
