package vlessserver

import "testing"

func TestProxyProtocolOnlyOnLoopback(t *testing.T) {
	for _, base := range []Config{tlsConfig(), realityConfig()} {
		base.AcceptProxyProtocol = true
		for _, listen := range []string{"0.0.0.0:9444", "192.0.2.1:9444", "localhost:9444"} {
			base.Listen = listen
			if base.Validate() == nil {
				t.Fatalf("trusted public PROXY listener %s", listen)
			}
		}
		base.Listen = "127.0.0.1:9444"
		if err := base.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
