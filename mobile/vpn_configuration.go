package mobile

import "quiclab/internal/vpnmodel"

// ValidateVpnConfiguration validates only the portable model, not Android local preferences.
func ValidateVpnConfiguration(raw string) error {
 _, err := vpnmodel.Parse([]byte(raw))
 return err
}
