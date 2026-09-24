package gateway

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoRejectsResumeAndUsesRelativeLink(t *testing.T) {
	h := Demo(slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest("GET", "http://lab/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `href="download"`) {
		t.Fatal("demo link must survive nginx path prefix")
	}
	r = httptest.NewRequest("GET", "http://lab/download", nil)
	r.Header.Set("Range", "bytes=42-")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 416 {
		t.Fatal(w.Code)
	}
}
