package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"quiclab/internal/exitcontrol"
	"quiclab/internal/serverexit"
	"sort"
	"sync"
	"time"
)

var ErrServerExitRevision = errors.New("server exit selection changed; refresh choices")
var ErrServerExitApply = errors.New("server exit change saved but runtime has not acknowledged it")

type ServerExitPolicyView struct {
	Policy   *serverexit.Policy    `json:"policy"`
	Revision string                `json:"revision"`
	Choices  []serverexit.Metadata `json:"choices"`
}
type DeviceServerExitView = exitcontrol.View

func exitRevision(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ConfigureServerExits installs an immutable startup catalogue. Runtime changes
// require restarting the owner so metadata and providers cannot diverge.
func (s *Store) ConfigureServerExits(definitions []serverexit.Definition) error {
	c, err := serverexit.NewCatalog(definitions)
	if err != nil {
		return err
	}
	s.serverExitMu.Lock()
	defer s.serverExitMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serverExits != nil {
		return errors.New("server exits already configured")
	}
	s.serverExits = c
	return nil
}

// The callback must close/replace native ingress flows and acknowledge the new
// policy before returning. A missing owner never acknowledges a change.
func (s *Store) SetServerExitApply(apply func([]string) error) {
	s.serverExitMu.Lock()
	defer s.serverExitMu.Unlock()
	s.mu.Lock()
	s.serverExitApply = apply
	s.mu.Unlock()
}

func (s *Store) ServerExitWorkers(ids []string) (awg, vless bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		d := s.state.Devices[id]
		awg = awg || d.AWG != nil
		vless = vless || (d.VLESSUUID != "" && !d.VLESSRevoked)
	}
	return
}

func (s *Store) policyViewLocked(u User) ServerExitPolicyView {
	v := ServerExitPolicyView{Policy: u.ServerExitPolicy.Clone(), Choices: s.serverExits.List()}
	v.Revision = exitRevision(struct {
		ID   string
		View ServerExitPolicyView
	}{u.ID, v})
	return v
}
func (s *Store) UserServerExitPolicy(userID string) (ServerExitPolicyView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.state.Users[userID]
	if !ok {
		return ServerExitPolicyView{}, ErrUpdateForbidden
	}
	return s.policyViewLocked(u), nil
}

func (s *Store) exitViewLocked(d Device) DeviceServerExitView {
	p := s.state.Users[d.UserID].ServerExitPolicy
	v := DeviceServerExitView{Selected: d.ServerExitID, Legacy: p == nil, Choices: []serverexit.Metadata{}}
	if p != nil {
		for _, id := range p.Allowed {
			if m, ok := s.serverExits.Metadata(id); ok {
				v.Choices = append(v.Choices, m)
			}
		}
	}
	id, err := s.serverExits.Resolve(p, d.ServerExitID)
	v.Effective, v.Blocked = id, err != nil
	v.Pending = s.serverExitPending[d.ID]
	v.Revision = exitRevision(struct {
		ID         string
		Generation uint64
		Policy     *serverexit.Policy
		View       DeviceServerExitView
	}{d.ID, d.ServerExitRevision, p, v})
	return v
}
func (s *Store) DeviceServerExits(deviceID, token string) (DeviceServerExitView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.authenticateUpdateLocked(deviceID, token)
	if err != nil {
		return DeviceServerExitView{}, err
	}
	return s.exitViewLocked(d), nil
}

// ServerExitForDevice returns desired policy to trusted runtime owners. It is
// not an admission acknowledgement: owners must fail closed while applying it.
func (s *Store) ServerExitForDevice(deviceID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverExitForDeviceLocked(deviceID)
}
func (s *Store) serverExitForDeviceLocked(deviceID string) (string, error) {
	d, ok := s.state.Devices[deviceID]
	u, present := s.state.Users[d.UserID]
	now := time.Now()
	if !ok || !present || d.Disabled || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) {
		return "", ErrUpdateForbidden
	}
	return s.serverExits.Resolve(u.ServerExitPolicy, d.ServerExitID)
}

func (s *Store) ActiveServerExitForDevice(deviceID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverExitAdmissionLocked(deviceID, true)
}
func (s *Store) serverExitAdmissionLocked(deviceID string, capable bool) (string, error) {
	d := s.state.Devices[deviceID]
	if s.serverExitPending[deviceID] || (!capable && (d.ServerExitID != "" || s.state.Users[d.UserID].ServerExitPolicy != nil)) {
		return "", ErrServerExitApply
	}
	return s.serverExitForDeviceLocked(deviceID)
}
func (s *Store) pendingServerExitLocked(id string) {
	if s.serverExitPending == nil {
		s.serverExitPending = map[string]bool{}
	}
	s.serverExitPending[id] = true
}

// AcquireServerExit is called only by the private broker after the worker has
// authenticated its peer. Identity is the VLESS UUID or the AWG device ID; the
// broker never accepts a caller-supplied exit ID. Selection and registration
// share the same lock as policy publication, preventing revocation races.
func (s *Store) AcquireServerExit(protocol, identity string, close func()) (string, string, netip.Addr, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var d Device
	switch protocol {
	case "awg":
		d = s.state.Devices[identity]
		if d.AWG == nil {
			return "", "", netip.Addr{}, nil, ErrUpdateForbidden
		}
	case "vless":
		for _, candidate := range s.state.Devices {
			if candidate.VLESSUUID == identity && identity != "" && !candidate.VLESSRevoked {
				d = candidate
				break
			}
		}
	default:
		return "", "", netip.Addr{}, nil, ErrUpdateForbidden
	}
	u, ok := s.state.Users[d.UserID]
	now := time.Now()
	if d.ID == "" || !ok || d.Disabled || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) || !u.Allows(protocol) {
		return "", "", netip.Addr{}, nil, ErrUpdateForbidden
	}
	exit, err := s.serverExitAdmissionLocked(d.ID, true)
	if err != nil {
		return "", "", netip.Addr{}, nil, err
	}
	source := netip.Addr{}
	if protocol == "awg" {
		source, err = netip.ParseAddr(d.AWG.Address)
		if err != nil || !source.Is4() {
			return "", "", netip.Addr{}, nil, ErrUpdateForbidden
		}
	}
	releaseAdmission, err := s.admission.Acquire(d.ID)
	if err != nil {
		return "", "", netip.Addr{}, nil, err
	}
	token := randomID()
	if s.active[d.ID] == nil {
		s.active[d.ID] = map[string]func(){}
	}
	if s.sessionProtocols == nil {
		s.sessionProtocols = map[string]string{}
	}
	s.active[d.ID][token] = func() { releaseAdmission(); close() }
	s.sessionProtocols[token] = protocol
	expires := d.Expires
	if u.Expires.Before(expires) {
		expires = u.Expires
	}
	timer := time.AfterFunc(time.Until(expires), close)
	var once sync.Once
	release := func() {
		once.Do(func() {
			timer.Stop()
			releaseAdmission()
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.active[d.ID], token)
			delete(s.sessionProtocols, token)
			if len(s.active[d.ID]) == 0 {
				delete(s.active, d.ID)
			}
		})
	}
	return d.ID, exit, source, release, nil
}

func (s *Store) applyServerExits(ids []string, closers []func(), apply func([]string) error, saveErr error) error {
	closeAll(closers)
	var err error
	if apply == nil {
		err = ErrServerExitApply
	} else if apply(ids) != nil {
		err = ErrServerExitApply
	}
	if err == nil {
		s.mu.Lock()
		for _, id := range ids {
			delete(s.serverExitPending, id)
		}
		s.mu.Unlock()
	}
	return errors.Join(saveErr, err)
}

func (s *Store) SetServerExitPolicy(userID string, policy *serverexit.Policy, expected string) error {
	s.serverExitMu.Lock()
	defer s.serverExitMu.Unlock()
	s.mu.Lock()
	u, ok := s.state.Users[userID]
	if !ok {
		s.mu.Unlock()
		return ErrUpdateForbidden
	}
	if expected == "" || expected != s.policyViewLocked(u).Revision {
		s.mu.Unlock()
		return ErrServerExitRevision
	}
	policy = policy.Clone()
	if err := s.serverExits.ValidatePolicy(policy); err != nil {
		s.mu.Unlock()
		return err
	}
	old := u
	u.ServerExitPolicy = policy
	s.state.Users[userID] = u
	saveErr := s.save()
	if saveErr != nil && !statePublished(saveErr) {
		s.state.Users[userID] = old
		s.mu.Unlock()
		return saveErr
	}
	ids, closers := []string{}, []func(){}
	for id, d := range s.state.Devices {
		if d.UserID == userID {
			s.pendingServerExitLocked(id)
			ids = append(ids, id)
			closers = append(closers, s.takeClosers(id)...)
		}
	}
	sort.Strings(ids)
	apply := s.serverExitApply
	s.mu.Unlock()
	return s.applyServerExits(ids, closers, apply, saveErr)
}

func (s *Store) SelectServerExit(deviceID, token, selected, expected string) (DeviceServerExitView, error) {
	s.serverExitMu.Lock()
	defer s.serverExitMu.Unlock()
	s.mu.Lock()
	d, err := s.authenticateUpdateLocked(deviceID, token)
	if err != nil {
		s.mu.Unlock()
		return DeviceServerExitView{}, err
	}
	if expected == "" || expected != s.exitViewLocked(d).Revision {
		s.mu.Unlock()
		return DeviceServerExitView{}, ErrServerExitRevision
	}
	if _, err = s.serverExits.Resolve(s.state.Users[d.UserID].ServerExitPolicy, selected); err != nil {
		s.mu.Unlock()
		return DeviceServerExitView{}, err
	}
	old := d
	d.ServerExitID = selected
	d.ServerExitRevision++
	s.state.Devices[deviceID] = d
	saveErr := s.save()
	if saveErr != nil && !statePublished(saveErr) {
		s.state.Devices[deviceID] = old
		s.mu.Unlock()
		return DeviceServerExitView{}, saveErr
	}
	s.pendingServerExitLocked(deviceID)
	closers, apply := s.takeClosers(deviceID), s.serverExitApply
	s.mu.Unlock()
	err = s.applyServerExits([]string{deviceID}, closers, apply, saveErr)
	s.mu.Lock()
	v := s.exitViewLocked(s.state.Devices[deviceID])
	s.mu.Unlock()
	return v, err
}
