package protocol

import (
	"encoding/json"
	"errors"
	"net/http"
)

const ControlVersion = 1
const DataVersion = 1

var ErrUpgradeRequired = errors.New("upgrade_required")

// Capabilities are public server metadata, independent of data-plane access.
type Capabilities struct {
	ControlVersion        int      `json:"control_version"`
	DataVersion           int      `json:"data_version"`
	MinAndroidVersionCode int      `json:"min_android_version_code"`
	Features              []string `json:"features,omitempty"`
	AndroidVersionCode    int      `json:"android_version_code,omitempty"`
	AndroidVersionName    string   `json:"android_version_name,omitempty"`
	APKURL                string   `json:"apk_url,omitempty"`
	APKSHA256             string   `json:"apk_sha256,omitempty"`
}

func DefaultCapabilities() Capabilities {
	return Capabilities{ControlVersion: ControlVersion, DataVersion: DataVersion}
}

func (c Capabilities) CheckData(dataVersion, androidVersionCode int) error {
	if c.ControlVersion != ControlVersion || c.DataVersion != dataVersion || androidVersionCode < c.MinAndroidVersionCode {
		return ErrUpgradeRequired
	}
	return nil
}

func CapabilitiesHandler(c Capabilities) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(c)
	})
}
