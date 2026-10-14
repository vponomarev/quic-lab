//go:build windows

package windowsengine

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNetworkScriptLargePayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := networkScript(ctx, "$x='"+strings.Repeat("x", 70000)+"';[Console]::Write($x.Length)")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "70000" {
		t.Fatalf("unexpected result %q", out)
	}
}
