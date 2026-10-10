package configtransfer

import (
	"encoding/json"
	"quiclab/internal/mdm"
	"strings"
	"testing"
)

func TestPrepare(t *testing.T) {
	b, err := Export([]byte(fixture), Selection{Sections: []string{"profiles"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	id := "12345678-1234-1234-1234-123456789abc"
	p, err := Prepare([]byte(fixture), b, ImportOptions{Mode: "add", Mapping: map[string]string{"default": id}})
	if err != nil {
		t.Fatal(err)
	}
	var d mdm.ConfigurationDocument
	json.Unmarshal(p.Candidate, &d)
	if len(d.Profiles) != 2 || d.Profiles[1].ID != id || d.CurrentProfileID != "default" || len(p.MissingCredentials) != 1 {
		t.Fatal("add overwrote or enabled profile")
	}
	if _, err := Prepare([]byte(fixture), b, ImportOptions{Mode: "update"}); err == nil {
		t.Fatal("implicit match accepted")
	}
	if _, err := Prepare([]byte(fixture), b, ImportOptions{Mode: "add", Mapping: map[string]string{"default": "default"}}); err == nil {
		t.Fatal("collision accepted")
	}
	p, err = Prepare([]byte(fixture), b, ImportOptions{Mode: "update", Mapping: map[string]string{"default": "default"}})
	if err != nil {
		t.Fatal(err)
	}
	if mdm.ValidateDocument(p.Candidate) != nil {
		t.Fatal("invalid candidate")
	}
	for _, bad := range []string{strings.Replace(string(b), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(b), `"version":1`, `"version":null`, 1), strings.Replace(string(b), `"version":1`, `"version":2`, 1), strings.Replace(string(b), `"version":1`, `"version":1,"command":"shell"`, 1)} {
		if _, err := Prepare([]byte(fixture), []byte(bad), ImportOptions{Mode: "update", Mapping: map[string]string{"default": "default"}}); err == nil {
			t.Fatal("malformed envelope accepted")
		}
	}
}
func TestPartialImport(t *testing.T) {
	b, _ := Export([]byte(fixture), Selection{Sections: []string{"diagnostics"}}, false)
	base := strings.Replace(fixture, `"enabled":true`, `"enabled":false`, 1)
	p, err := Prepare([]byte(base), b, ImportOptions{Mode: "update"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p.Candidate), `"enabled":true`) {
		t.Fatal("section not imported")
	}
	if _, err := Prepare([]byte(base), b, ImportOptions{Mode: "replace"}); err == nil {
		t.Fatal("incomplete replacement accepted")
	}
}

func TestRejectMissingSectionFields(t *testing.T) {
	for _, payload := range []string{`{"diagnostics":{}}`, `{"network":{"dns":{"mode":"system","profileId":"default"}}}`, `{"routing":{"currentProfileId":"default","enabledProfileIds":[],"globalApps":[],"settings":{}}}`} {
		raw := []byte(`{"format":"quic-lab-config","version":1,"credentials":"omitted","payload":` + payload + `}`)
		if _, err := Prepare([]byte(fixture), raw, ImportOptions{Mode: "update", Mapping: map[string]string{"default": "default"}}); err == nil {
			t.Fatal("missing required field accepted", payload)
		}
	}
}

func TestReplacementCannotInheritUnmappedIdentity(t *testing.T) {
	b, err := Export([]byte(fixture), Selection{Sections: []string{"profiles", "routing", "network", "diagnostics"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare([]byte(fixture), b, ImportOptions{Mode: "replace", Mapping: map[string]string{"default": "default"}}); err == nil {
		t.Fatal("replacement reused existing identity ID")
	}
	id := "12345678-1234-1234-1234-123456789abc"
	p, err := Prepare([]byte(fixture), b, ImportOptions{Mode: "replace", Mapping: map[string]string{"default": id}})
	if err != nil {
		t.Fatal(err)
	}
	var d mdm.ConfigurationDocument
	json.Unmarshal(p.Candidate, &d)
	if d.CurrentProfileID != id || d.DNS.ProfileID != id || len(d.EnabledProfileIDs) != 0 || len(p.MissingCredentials) != 1 {
		t.Fatal("references not remapped or incomplete profile enabled")
	}
}
