package mobile

import (
	"encoding/json"
	"quiclab/internal/configtransfer"
)

// Portable configuration only: never an MDM registration or permission document.
func ExportConfiguration(raw, selection string, includeSecrets bool) (string, error) {
	var s configtransfer.Selection
	if err := configtransfer.DecodeOptions([]byte(selection), &s); err != nil {
		return "", err
	}
	b, err := configtransfer.Export([]byte(raw), s, includeSecrets)
	return string(b), err
}
func PrepareConfigurationImport(base, bundle, options string) (string, error) {
	var o configtransfer.ImportOptions
	if err := configtransfer.DecodeOptions([]byte(options), &o); err != nil {
		return "", err
	}
	p, err := configtransfer.Prepare([]byte(base), []byte(bundle), o)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}
func EncryptConfiguration(raw []byte, password string) ([]byte, error) {
	return configtransfer.Encrypt(raw, []byte(password))
}
func DecryptConfiguration(raw []byte, password string) ([]byte, error) {
	return configtransfer.Decrypt(raw, []byte(password))
}
