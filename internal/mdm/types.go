package mdm

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrConflict     = errors.New("conflict")
	ErrInvalid      = errors.New("invalid request")
	ErrLimit        = errors.New("queue full")
	ErrPaused       = errors.New("paused")
)

type Rights struct {
	Config      bool   `json:"config"`
	VPN         bool   `json:"vpn"`
	Telemetry   bool   `json:"telemetry"`
	Geo         bool   `json:"geo"`
	Coordinates bool   `json:"coordinates"`
	LANMode     string `json:"lanMode"`
}
type Binding struct {
	ID              string `json:"id"`
	Epoch           int64  `json:"epoch"`
	RequestedRights Rights `json:"requestedRights"`
	GrantedRights   Rights `json:"grantedRights"`
	Active          bool   `json:"active"`
}
type Invitation struct {
	Token           string    `json:"token"`
	ExpiresAt       time.Time `json:"expiresAt"`
	RequestedRights Rights    `json:"requestedRights"`
}
type EnrollmentRequest struct {
	Version        int    `json:"version"`
	Token          string `json:"token"`
	RegistrationID string `json:"registrationId"`
	Secret         string `json:"secret"`
}
type ConfigRevision struct {
	ExpectedGeneration int64           `json:"expectedGeneration"`
	Revision           int64           `json:"revision"`
	Mode               string          `json:"mode"`
	Document           json.RawMessage `json:"document"`
}
type Command struct {
	ID        string    `json:"id"`
	BindingID string    `json:"bindingId"`
	Epoch     int64     `json:"epoch"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Kind      string    `json:"kind"`
}
type Event struct {
	ID         string    `json:"id"`
	Actor      string    `json:"actor"`
	Kind       string    `json:"kind"`
	CommandID  string    `json:"commandId,omitempty"`
	Revision   int64     `json:"revision,omitempty"`
	Result     string    `json:"result,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}
type SyncRequest struct {
	Version         int     `json:"version"`
	BindingID       string  `json:"bindingId"`
	Epoch           int64   `json:"epoch"`
	AppliedRevision int64   `json:"appliedRevision"`
	Events          []Event `json:"events"`
	Wait            bool    `json:"wait,omitempty"`
	GrantedRights   *Rights `json:"grantedRights,omitempty"`
}
type SyncResponse struct {
	Telemetry     TelemetryPolicy `json:"telemetry"`
	Version       int             `json:"version"`
	Epoch         int64           `json:"epoch"`
	DesiredConfig *ConfigRevision `json:"desiredConfig,omitempty"`
	Commands      []Command       `json:"commands"`
	ServerTime    time.Time       `json:"serverTime"`
}
