package mobile

import (
	"encoding/json"
	"quiclab/internal/research"
	"testing"
	"time"
)

func TestResearchExitMetadata(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	c := researchTestCollector(t, o, nil)
	c.exit = research.ExitRef{Route: "vpn", ID: "home", Type: "quic"}
	c.mu.Lock()
	c.active = true
	c.mu.Unlock()
	f := c.openFlow(6, "10.0.0.1", 1000, "1.1.1.1", 443)
	f.add(7, 3, 0, 0)
	c.flush(time.Now().Add(time.Minute))
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, q := range c.queue {
		if q.record.Type != "flow_open" && q.record.Type != "flow_delta" {
			continue
		}
		var d research.FlowData
		if json.Unmarshal(q.record.Data, &d) != nil || d.ID != "home" || d.Route != "vpn" {
			t.Fatal(string(q.record.Data))
		}
	}
}
