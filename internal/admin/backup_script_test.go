package admin

import (
	"bytes"
	"strings"
	"testing"
)

func TestBackupPollingRespectsCSP(t *testing.T) {
	var b bytes.Buffer
	err := backupPageTemplate.Execute(&b, struct {
		Base, CSRF, Request string
		Jobs                []any
	}{Base: "/lab/"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "<script>") || !strings.Contains(b.String(), `src="/lab/backups/poll.js"`) {
		t.Fatal("backup polling must use same-origin external script")
	}
}
