package mobile
import (
 "encoding/json"
 "quiclab/internal/mdm"
)
// ValidateMDMConfiguration performs the same bounded, secret-safe validation as
// the management server. It does not apply settings or open network connections.
func ValidateMDMConfiguration(raw string) error { return mdm.ValidateDocument(json.RawMessage(raw)) }
