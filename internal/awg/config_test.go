package awg

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testConfig() string {
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	return "[Interface]\nPrivateKey = " + key + "\nAddress = 10.0.0.2\nDNS = 1.1.1.1\nJc = 5\nJmin = 50\nJmax = 1000\nS1 = 134\nS2 = 90\nH1 = 101\nH2 = 102\nH3 = 103\nH4 = 104\n[Peer]\nPublicKey = " + key + "\nPresharedKey = " + key + "\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = vpn.example.org:52000\nPersistentKeepalive = 0\n"
}
func TestConfigMetadataAndIPC(t *testing.T) {
	c, e := Parse(testConfig())
	if e != nil {
		t.Fatal(e)
	}
	if c.Address != "10.0.0.2" || !c.IPv6Ignored || len(c.Allowed) != 1 {
		t.Fatal("IPv4 normalization")
	}
	meta := c.Metadata()
	if strings.Contains(meta, "private") || strings.Contains(meta, c.private) || strings.Contains(meta, "MDEy") {
		t.Fatal("secret in metadata")
	}
	ipc := c.IPC("192.0.2.1:52000")
	if !strings.Contains(ipc, "persistent_keepalive_interval=0\n") || strings.Contains(ipc, "::/0") {
		t.Fatal("IPC translation")
	}
}
func TestRejectUnsafeOrUnsupportedConfig(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(testConfig(), "Jc = 5", "Jc = 999", 1),
		strings.Replace(testConfig(), "Jmin = 50", "Jmin = 1100", 1),
		testConfig() + "[Peer]\nPublicKey = secret\n",
		testConfig() + "PostUp = secret-command\n",
		strings.Replace(testConfig(), "Jc = 5", "S3 = 3", 1),
		strings.Replace(testConfig(), "PrivateKey = ", "PrivateKey = secret", 1),
		strings.Replace(testConfig(), "0.0.0.0/0, ", "", 1),
		strings.Replace(testConfig(), "DNS = 1.1.1.1", "DNS = dns.example.org", 1),
	} {
		_, e := Parse(raw)
		if e == nil {
			t.Fatal("invalid config accepted")
		}
		if strings.Contains(e.Error(), "secret") {
			t.Fatal("error disclosed field value")
		}
	}
}
