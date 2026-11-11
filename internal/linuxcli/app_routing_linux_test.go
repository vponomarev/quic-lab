//go:build linux && !android

package linuxcli

import (
	"bytes"
	"encoding/json"
	"quiclab/internal/approuting"
	"strings"
	"testing"
)

func TestLinuxAppRoutingEventsAreLocalAggregates(t *testing.T) {
	var b bytes.Buffer
	s := &sink{writer: &b}
	s.appRouting(&approuting.Statistics{Enabled: true, DirectTxBytes: 42, Unknown: 2, Shared: 1})
	s.OnEvent(`{"event":"route_selected","path":"/private/application","pid":999}`)
	s.OnEvent(`{"event":"traffic","tx_bytes":7,"rx_bytes":9,"path":"/private/application"}`)
	if strings.Contains(b.String(), "private") || strings.Contains(b.String(), "pid") {
		t.Fatal("identity leaked")
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 {
		t.Fatal(b.String())
	}
	var event struct {
		Event    string
		Counters approuting.Statistics
	}
	if json.Unmarshal([]byte(lines[0]), &event) != nil || event.Event != "app_routing" || event.Counters.DirectTxBytes != 42 {
		t.Fatal(b.String())
	}
}
