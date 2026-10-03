package vlessserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfigValidatesWithoutOpeningTLSMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := tlsConfig()
	raw, _ := json.Marshal(c)
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	got, e := ReadConfig(path)
	if e != nil || got.Listen != c.Listen {
		t.Fatal("valid typed config rejected", e)
	}
	for _, bad := range [][]byte{append(append([]byte{}, raw...), []byte("{}")...), []byte("{"), []byte("{\"unknown\":true}")} {
		os.WriteFile(path, bad, 0600)
		if _, e := ReadConfig(path); e != errConfig {
			t.Fatal("invalid config not rejected with sanitized error")
		}
	}
	os.WriteFile(path, raw, 0600)
	os.Chmod(path, 0644)
	if _, e := ReadConfig(path); e != errConfig {
		t.Fatal("public config accepted")
	}
}
