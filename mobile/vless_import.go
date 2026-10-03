package mobile

import (
	"encoding/json"
	"quiclab/internal/vless"
)

// ImportVLESSConfig returns a canonical profile containing credentials. The
// caller must send this result directly to encrypted storage; never log/display
// it. It accepts bounded URI or standalone outbound input, not subscriptions.
func ImportVLESSConfig(raw string) (string, error) {
	p, err := vless.ParseImport(raw)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ValidateVLESSConfig returns display metadata without UUID or REALITY key.
// It validates the same formats as ImportVLESSConfig and performs no networking.
func ValidateVLESSConfig(raw string) (string, error) {
	p, err := vless.ParseImport(raw)
	if err != nil {
		return "", err
	}
	metadata := struct {
		Name     string `json:"name"`
		Endpoint string `json:"endpoint"`
		Security string `json:"security"`
		Flow     string `json:"flow"`
	}{p.Name, p.Config.Endpoint, p.Config.Security, p.Config.Flow}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
