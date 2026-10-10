package configtransfer

import (
	"encoding/json"
	"strings"
	"testing"
)

const fixture = `{"schema":1,"profiles":[{"id":"default","name":"Main","settings":{"transport":"quic","endpoint":"vpn.example:443"}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":[],"dns":{"mode":"tunnel","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}`

func TestExportSelection(t *testing.T) {
	raw, err := Export([]byte(fixture), Selection{Sections: []string{"diagnostics"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	var b struct{ Payload map[string]json.RawMessage }
	if json.Unmarshal(raw, &b) != nil {
		t.Fatal("bad envelope")
	}
	if len(b.Payload) != 1 || b.Payload["diagnostics"] == nil {
		t.Fatal("unselected sections leaked")
	}
	for _, s := range []Selection{{}, {Sections: []string{"unknown"}}, {Sections: []string{"profiles"}, ProfileIDs: []string{"missing"}}} {
		if _, err := Export([]byte(fixture), s, false); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	bad := strings.Replace(fixture, `"schema":1`, `"schema":1,"mdmToken":"secret"`, 1)
	if _, err := Export([]byte(bad), Selection{Sections: []string{"profiles"}}, false); err == nil {
		t.Fatal("private envelope accepted")
	}
}
func TestExportStripsCredentials(t *testing.T) {
	raw := strings.Replace(fixture, `"settings":`, `"identity":{"vless_uri":"vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"},"settings":`, 1)
	out, err := Export([]byte(raw), Selection{Sections: []string{"profiles"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "vless://") || strings.Contains(string(out), "identity") {
		t.Fatal("credentials leaked")
	}
}
