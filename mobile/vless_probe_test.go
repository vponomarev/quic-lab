package mobile

import (
	"encoding/json"
	"quiclab/internal/vless"
	"testing"
)

func TestVLESSProbeValidation(t *testing.T) {
	cfg := vless.Config{Endpoint: "127.0.0.1:443", UUID: "11111111-1111-4111-8111-111111111111", Security: "tls", ServerName: "fixture.invalid"}
	raw, _ := json.Marshal(cfg)
	p, err := NewVLESSProbe(string(raw), &vlessTestBinder{}, nil, "wifi")
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	p.Close()
	if _, err = p.HTTPSStatus("https://example.invalid"); err == nil {
		t.Fatal("closed probe used")
	}
	for _, bad := range []string{"{}", `{"inbounds":[]}`, string(raw) + "{}"} {
		if p, err := NewVLESSProbe(bad, &vlessTestBinder{}, nil, "wifi"); err == nil {
			p.Close()
			t.Fatal("invalid input admitted")
		}
	}
	if p, err := NewVLESSProbe(string(raw), &vlessTestBinder{}, nil, "cell"); err == nil {
		p.Close()
		t.Fatal("cell budget missing")
	}
}
