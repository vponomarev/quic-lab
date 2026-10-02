package protocol

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCapabilitiesReadableBeforeDataHandshake(t *testing.T) {
	caps := Capabilities{ControlVersion: 1, DataVersion: 9, MinAndroidVersionCode: 45, Features: []string{"bond"}}
	r := httptest.NewRequest("GET", "/api/v1/capabilities", nil)
	w := httptest.NewRecorder()
	CapabilitiesHandler(caps).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("anonymous control returned %d", w.Code)
	}
	var got Capabilities
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DataVersion != 9 || got.ControlVersion != 1 {
		t.Fatalf("versions coupled: %+v", got)
	}
	if !errors.Is(got.CheckData(1, 45), ErrUpgradeRequired) {
		t.Fatal("incompatible data did not require upgrade")
	}
}

func TestCapabilitiesCompatibility(t *testing.T) {
	for _, test := range []struct {
		data, android int
		want          bool
	}{{1, 45, true}, {1, 44, false}, {2, 45, false}} {
		err := (Capabilities{ControlVersion: 1, DataVersion: 1, MinAndroidVersionCode: 45}).CheckData(test.data, test.android)
		if (err == nil) != test.want {
			t.Fatalf("data=%d android=%d: %v", test.data, test.android, err)
		}
	}
}
