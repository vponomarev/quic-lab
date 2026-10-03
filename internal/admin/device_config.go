package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"quiclab/internal/protocol"
	"strings"
	"time"
)

var ErrUpdateUnauthorized = errors.New("device authentication required")
var ErrUpdateForbidden = errors.New("device unavailable")

// Enrollment secret is the only recovery key. Its hash cannot decrypt the
// bounded outbox; valid enrollment replay is the only path that returns it.
func updateOutboxCipher(token, enrollmentID, requestID, deviceID string) (cipher.AEAD, []byte, error) {
	secret, e := hex.DecodeString(token)
	if e != nil || len(secret) != 32 {
		return nil, nil, ErrUpdateUnauthorized
	}
	aad, e := json.Marshal([]string{"quiclab/device-update-outbox/v1", enrollmentID, requestID, deviceID})
	if e != nil {
		return nil, nil, e
	}
	// HKDF extract + one-block expand, with explicit domain separation.
	extract := hmac.New(sha256.New, []byte("quiclab/device-update-outbox/v1"))
	extract.Write(secret)
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write(aad)
	expand.Write([]byte{1})
	block, e := aes.NewCipher(expand.Sum(nil))
	if e != nil {
		return nil, nil, e
	}
	aead, e := cipher.NewGCM(block)
	return aead, aad, e
}
func issueUpdateToken(d *Device, en Enrollment, enrollmentToken, requestID string) (string, error) {
	aead, aad, e := updateOutboxCipher(enrollmentToken, en.ID, requestID, d.ID)
	if e != nil {
		return "", e
	}
	var raw [32]byte
	if _, e = rand.Read(raw[:]); e != nil {
		return "", e
	}
	token := hex.EncodeToString(raw[:])
	nonce := make([]byte, aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	d.UpdateTokenHash = enrollmentHash(token)
	d.UpdateTokenOutbox = base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(token), aad))
	d.UpdateOutboxExpires = en.Expires
	return token, nil
}
func replayUpdateToken(d Device, en Enrollment, enrollmentToken, requestID string) (string, error) {
	aead, aad, e := updateOutboxCipher(enrollmentToken, en.ID, requestID, d.ID)
	if e != nil {
		return "", e
	}
	b, e := base64.RawStdEncoding.DecodeString(d.UpdateTokenOutbox)
	if e != nil || len(b) != aead.NonceSize()+64+aead.Overhead() {
		return "", ErrUpdateUnauthorized
	}
	raw, e := aead.Open(nil, b[:aead.NonceSize()], b[aead.NonceSize():], aad)
	if e != nil {
		return "", ErrUpdateUnauthorized
	}
	token := string(raw)
	if subtle.ConstantTimeCompare([]byte(enrollmentHash(token)), []byte(d.UpdateTokenHash)) != 1 {
		return "", ErrUpdateUnauthorized
	}
	return token, nil
}
func (s *Store) AuthenticateUpdate(deviceID, token string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authenticateUpdateLocked(deviceID, token)
}
func (s *Store) authenticateUpdateLocked(deviceID, token string) (Device, error) {
	d, ok := s.state.Devices[deviceID]
	if len(token) != 64 {
		return Device{}, ErrUpdateUnauthorized
	}
	if _, e := hex.DecodeString(token); e != nil {
		return Device{}, ErrUpdateUnauthorized
	}
	if !ok || d.UpdateTokenHash == "" || subtle.ConstantTimeCompare([]byte(enrollmentHash(token)), []byte(d.UpdateTokenHash)) != 1 {
		return Device{}, ErrUpdateUnauthorized
	}
	u, ok := s.state.Users[d.UserID]
	now := time.Now()
	if !ok || d.Disabled || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) {
		return Device{}, ErrUpdateForbidden
	}
	return d, nil
}

type DeviceConfigEnvelope struct {
	SchemaVersion  int                   `json:"schema_version"`
	ConfigRevision uint64                `json:"config_revision"`
	DeviceID       string                `json:"device_id"`
	ExitID         string                `json:"exit_id"`
	ServerConfig   map[string]any        `json:"server_config"`
	Capabilities   protocol.Capabilities `json:"capabilities"`
}

func (w *Web) deviceConfigURL(id string) string {
	u, _ := url.Parse(w.Config.PublicURL)
	u.Path = "/api/v1/devices/" + id + "/config"
	u.RawPath = ""
	return u.String()
}
func (w *Web) deviceConfig(rw http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	token := ""
	if strings.HasPrefix(auth, "Bearer ") && len(r.Header.Values("Authorization")) == 1 {
		token = strings.TrimPrefix(auth, "Bearer ")
	}
	id := r.PathValue("id")
	d, e := w.Store.AuthenticateUpdate(id, token)
	if e != nil {
		updateHTTPError(rw, r, e)
		return
	}
	u, e := w.Store.enrollmentProfileUser(d)
	if e != nil {
		updateHTTPError(rw, r, ErrUpdateForbidden)
		return
	}
	p, e := w.profile(u)
	if e != nil {
		http.Error(rw, "Configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	raw, e := json.Marshal(p)
	if e != nil {
		http.Error(rw, "Configuration unavailable", 503)
		return
	}
	server := map[string]any{}
	if e = json.Unmarshal(raw, &server); e != nil {
		http.Error(rw, "Configuration unavailable", 503)
		return
	}
	// Routing mode/routes are local preferences, never remotely mutable.
	delete(server, "mode")
	delete(server, "routes")
	server["config_url"] = w.deviceConfigURL(id)
	caps := w.Config.Capabilities
	if caps.ControlVersion == 0 {
		caps = protocol.DefaultCapabilities()
	}
	if p.VLESSURI != "" {
		caps.MinAndroidVersionCode = max(caps.MinAndroidVersionCode, 26)
	}
	if caps.APKURL != "" {
		server["apk_url"] = caps.APKURL
	}
	envelope := DeviceConfigEnvelope{SchemaVersion: 1, DeviceID: id, ExitID: id, ServerConfig: server, Capabilities: caps}
	canonical, e := json.Marshal(envelope)
	if e != nil || len(canonical) > 1024*1024 {
		http.Error(rw, "Configuration unavailable", 503)
		return
	}
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	s := w.Store
	s.mu.Lock()
	current, e := s.authenticateUpdateLocked(id, token)
	if e == nil {
		previous := current
		if current.ConfigDigest != digest {
			current.ConfigRevision++
			current.ConfigDigest = digest
		}
		if current.ConfigRevision == 0 {
			current.ConfigRevision = 1
		}
		current.UpdateTokenOutbox = prunedUpdateOutbox(current)
		s.state.Devices[id] = current
		// Save even unchanged revisions: a previous primary rename followed by
		// failed directory sync must never be acknowledged without durable retry.
		e = s.save()
		if e != nil && !statePublished(e) {
			s.state.Devices[id] = previous
		}
		envelope.ConfigRevision = current.ConfigRevision
	}
	s.mu.Unlock()
	if e != nil {
		updateHTTPError(rw, r, e)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(rw).Encode(envelope)
}
func prunedUpdateOutbox(d Device) string {
	if !d.UpdateOutboxExpires.After(time.Now()) {
		return ""
	}
	return d.UpdateTokenOutbox
}
func updateHTTPError(rw http.ResponseWriter, r *http.Request, e error) {
	if errors.Is(e, ErrUpdateUnauthorized) {
		rw.Header().Set("WWW-Authenticate", `Bearer realm="device-config"`)
		http.Error(rw, "Device authentication required", 401)
		return
	}
	if errors.Is(e, ErrUpdateForbidden) {
		http.Error(rw, "Device unavailable", 403)
		return
	}
	http.Error(rw, "Configuration unavailable", 503)
}

// Read at most the protocol bound; retained for callers that stream configs.
func readDeviceConfig(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 1024*1024 {
		return nil, errors.New("configuration too large")
	}
	return b, nil
}
