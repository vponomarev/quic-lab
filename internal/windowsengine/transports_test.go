package windowsengine

import (
	"encoding/json"
	"strings"
	"testing"
)

const awgFixture = `[Interface]
PrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=
Address = 10.55.0.2/32
DNS = 1.1.1.1
Jc = 2
Jmin = 40
Jmax = 70
S1 = 0
S2 = 0
H1 = 1
H2 = 2
H3 = 3
H4 = 4
[Peer]
PublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=
Endpoint = vpn.example:51820
AllowedIPs = 0.0.0.0/0
`
const vlessFixture = "vless://01234567-89ab-cdef-0123-456789abcdef@vpn.example:443?security=tls&sni=secure.example&type=tcp"

func TestStandaloneTransportImport(t *testing.T) {
	for _, x := range []struct{ kind, raw string }{{"awg", awgFixture}, {"vless", vlessFixture}} {
		meta, e := ImportTransport(x.kind, x.raw)
		if e != nil || meta.Endpoint == "" {
			t.Fatal(x.kind, e)
		}
		b, _ := json.Marshal(meta)
		if strings.Contains(string(b), "AQEBAQ") || strings.Contains(string(b), "01234567") {
			t.Fatal("secret in metadata")
		}
		s := valid()
		s.Transport = x.kind
		s.Endpoint = meta.Endpoint
		s.Key = x.raw
		s.Certificate = ""
		s.Hostname = ""
		if e = s.Validate(); e != nil {
			t.Fatal(e)
		}
		var g map[string]any
		json.Unmarshal([]byte(s.GatewayJSON()), &g)
		if g[x.kind+"_config"] == nil {
			t.Fatal("missing transport payload")
		}
		if len(s.Targets) != 1 || s.Targets[0] != "198.51.100.1/32" {
			t.Fatal("import altered routes")
		}
	}
}
func TestStandaloneImportRejectsUnsupported(t *testing.T) {
	for _, x := range []struct{ kind, raw string }{{"awg", awgFixture + `PostUp = anything`}, {"vless", strings.Replace(vlessFixture, "type=tcp", "type=ws", 1)}, {"vless", "{}"}} {
		if _, e := ImportTransport(x.kind, x.raw); e == nil {
			t.Fatal("unsupported accepted")
		}
	}
}

func TestTransportRejectsUnusableRoutesAndTrust(t *testing.T) {
	s := valid()
	s.Transport = "awg"
	s.Key = strings.Replace(awgFixture, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 10.55.0.0/24", 1)
	if s.Validate() == nil {
		t.Fatal("AWG route outside AllowedIPs accepted")
	}
	s.Transport = "vless"
	s.Key = vlessFixture
	s.CA = "broken certificate"
	if s.Validate() == nil {
		t.Fatal("invalid VLESS trust accepted")
	}
}
