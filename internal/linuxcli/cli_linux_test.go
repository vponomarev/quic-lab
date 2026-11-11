//go:build linux && !android

package linuxcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunUsesIndependentFlagsWithoutStartingVPN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, []byte(`{"transport":"quic","endpoint":"198.18.0.2:443","targets":["192.0.2.2/32"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if code := Run([]string{"-validate", "-config", path}); code != 0 {
			t.Fatal("validate", code)
		}
	}
	if code := Run([]string{"-unknown-option"}); code != 2 {
		t.Fatal("unknown flag", code)
	}
	if code := Run([]string{"-help"}); code != 0 {
		t.Fatal("help", code)
	}
}
