package approuting

import (
	"path"
	"strings"
)

func linuxPathKey(s string) (string, bool) {
	if len(s) < 2 || len(s) > 4096 || s[0] != '/' || path.Clean(s) != s || strings.HasSuffix(s, " (deleted)") {
		return "", false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return "", false
		}
	}
	return s, true
}

// CompileLinuxTUN deliberately keeps Linux case-sensitive path semantics even
// when configuration tooling is built on a different operating system.
func CompileLinuxTUN(p TUNPolicy) (*TUNCompiled, error) { return compileTUN(p, linuxPathKey) }
