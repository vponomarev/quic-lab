package main

import (
	"strings"
	"testing"
)

func TestURIValidation(t *testing.T) {
	good := "quic-lab://capture?server=https%3A%2F%2Flab.example%2Flab%2F&id=" + strings.Repeat("a", 64) + "#" + strings.Repeat("b", 64)
	base, _, _, e := launch(good)
	if e != nil || base != "https://lab.example/lab/" {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(good, "https%3A", "http%3A", 1), strings.Replace(good, "quic-lab:", "https:", 1), strings.Replace(good, "#", "#bad", 1), "quic-lab://other"} {
		if _, _, _, e = launch(bad); e == nil {
			t.Fatal("invalid URL accepted", bad)
		}
	}
	for _, bad := range []string{"https://user:pass@example/lab/", "https://example/lab/?x=1", "https://example/../lab/"} {
		if _, e = canonical(bad); e == nil {
			t.Fatal("unsafe base accepted")
		}
	}
}
