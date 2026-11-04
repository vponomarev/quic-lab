package windowsclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
	"regexp"
)

const MaxSavedProfiles = 32

var profileID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type SavedProfile struct {
	ID       string                 `json:"id"`
	Settings windowsengine.Settings `json:"settings"`
}
type ProfileSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
}
type ProfileList struct {
	Revision uint64           `json:"revision"`
	Profiles []ProfileSummary `json:"profiles"`
}
type ProfileChange struct {
	ReplaceServerControl bool                    `json:"replace_server_control,omitempty"`
	ID                   string                  `json:"id,omitempty"`
	ExpectedRevision     uint64                  `json:"expected_revision"`
	Settings             *windowsengine.Settings `json:"settings,omitempty"`
	ReplaceKey           bool                    `json:"replace_key,omitempty"`
	Reconnect            bool                    `json:"reconnect,omitempty"`
}

func cloneDocument(d SettingsDocument) SettingsDocument {
	d.Settings = cloneSettings(d.Settings)
	d.Profiles = append([]SavedProfile(nil), d.Profiles...)
	for i := range d.Profiles {
		d.Profiles[i].Settings = cloneSettings(d.Profiles[i].Settings)
	}
	return d
}
func validDocument(d SettingsDocument) bool {
	if d.Revision == 0 || d.Settings.Validate() != nil || len(d.Profiles) > MaxSavedProfiles {
		return false
	}
	ids := make(map[string]bool, len(d.Profiles))
	for _, p := range d.Profiles {
		if !profileID.MatchString(p.ID) || ids[p.ID] || p.Settings.Validate() != nil {
			return false
		}
		ids[p.ID] = true
	}
	raw, err := json.Marshal(d)
	defer clear(raw)
	return err == nil && len(raw) <= MaxFrame
}
func (m *SettingsManager) documentWith(s windowsengine.Settings, revision uint64) SettingsDocument {
	m.mu.RLock()
	d := cloneDocument(m.doc)
	m.mu.RUnlock()
	d.Settings = cloneSettings(s)
	d.Revision = revision
	return d
}
func (m *SettingsManager) Profiles() ProfileList {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v := ProfileList{Revision: m.doc.Revision, Profiles: make([]ProfileSummary, 0, len(m.doc.Profiles))}
	for _, p := range m.doc.Profiles {
		v.Profiles = append(v.Profiles, ProfileSummary{p.ID, p.Settings.Name, p.Settings.Transport, p.Settings.Endpoint})
	}
	return v
}
func findProfile(d SettingsDocument, id string) int {
	for i, p := range d.Profiles {
		if p.ID == id {
			return i
		}
	}
	return -1
}
func (m *SettingsManager) editLibrary(ctx context.Context, q ProfileChange, save bool) error {
	if err := m.lock(ctx); err != nil {
		return err
	}
	defer func() { <-m.gate }()
	run, done, err := m.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	m.mu.RLock()
	d := cloneDocument(m.doc)
	m.mu.RUnlock()
	if d.Revision == 0 {
		return errors.New("config_required")
	}
	if q.ExpectedRevision != d.Revision {
		return errors.New("revision_conflict")
	}
	index := findProfile(d, q.ID)
	if q.ID != "" && (!profileID.MatchString(q.ID) || index < 0) {
		return errors.New("profile_not_found")
	}
	if save {
		if q.Settings == nil {
			return errors.New("invalid_config")
		}
		s, err := m.resolve(SettingsChange{ExpectedRevision: q.ExpectedRevision, Settings: *q.Settings, ReplaceKey: q.ReplaceKey, ReplaceServerControl: q.ReplaceServerControl})
		if err != nil {
			return err
		}
		if _, _, err = m.factory(s); err != nil {
			return errors.New("invalid_config")
		}
		if index < 0 {
			if len(d.Profiles) >= MaxSavedProfiles {
				return errors.New("profile_limit")
			}
			var raw [16]byte
			if _, err = rand.Read(raw[:]); err != nil {
				return errors.New("operation_failed")
			}
			d.Profiles = append(d.Profiles, SavedProfile{hex.EncodeToString(raw[:]), cloneSettings(s)})
		} else {
			d.Profiles[index].Settings = cloneSettings(s)
		}
	} else {
		if index < 0 {
			return errors.New("profile_not_found")
		}
		d.Profiles = append(d.Profiles[:index], d.Profiles[index+1:]...)
	}
	d.Revision++
	if !validDocument(d) {
		return errors.New("profile_limit")
	}
	if err := m.store.Save(run, cloneDocument(d)); err != nil {
		return errors.New("config_store_failed")
	}
	m.mu.Lock()
	m.doc = d
	m.mu.Unlock()
	return nil
}
func (m *SettingsManager) SaveProfile(ctx context.Context, q ProfileChange) error {
	return m.editLibrary(ctx, q, true)
}
func (m *SettingsManager) DeleteProfile(ctx context.Context, q ProfileChange) error {
	return m.editLibrary(ctx, q, false)
}
func (m *SettingsManager) ActivateProfile(ctx context.Context, q ProfileChange) error {
	if err := m.lock(ctx); err != nil {
		return err
	}
	defer func() { <-m.gate }()
	m.mu.RLock()
	d := cloneDocument(m.doc)
	m.mu.RUnlock()
	if q.ExpectedRevision != d.Revision {
		return errors.New("revision_conflict")
	}
	i := findProfile(d, q.ID)
	if !profileID.MatchString(q.ID) || i < 0 {
		return errors.New("profile_not_found")
	}
	if m.Snapshot().State != vpnclient.Stopped && !q.Reconnect {
		return errors.New("stop_before_apply")
	}
	s := d.Profiles[i].Settings
	// Local capture/binding choices belong to this device, not a remote profile.
	s.Targets = append([]string(nil), d.Settings.Targets...)
	s.RoutingMode = d.Settings.RoutingMode
	s.UplinkMode = d.Settings.UplinkMode
	s.InterfaceIndex = d.Settings.InterfaceIndex
	s.DNS = d.Settings.DNS
	s.AppRouting = d.Settings.AppRouting.Clone()
	return m.applyLocked(ctx, SettingsChange{ExpectedRevision: q.ExpectedRevision, Settings: s, ReplaceKey: true, ReplaceServerControl: true, Reconnect: q.Reconnect})
}
