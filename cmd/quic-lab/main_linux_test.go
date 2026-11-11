//go:build linux && !android

package main

import (
	"reflect"
	"testing"
)

func TestInvocationModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		mode string
		rest []string
	}{
		{"quic-lab", nil, "gui", nil},
		{"quic-lab", []string{"gui"}, "gui", []string{}},
		{"quic-lab-client", nil, "engine", nil},
		{"quic-lab-client", []string{"-config", "/etc/quic-lab/client.json"}, "engine", []string{"-config", "/etc/quic-lab/client.json"}},
		{"quic-lab", []string{"-validate"}, "engine", []string{"-validate"}},
		{"quic-lab", []string{"engine", "-validate"}, "engine", []string{"-validate"}},
		{"quic-lab-gui", nil, "gui", nil},
		{"quic-lab", []string{"control"}, "control", []string{}},
		{"quic-lab", []string{"install", "--user", "vitaly"}, "install", []string{"--user", "vitaly"}},
		{"quic-lab", []string{"status"}, "status", []string{}},
		{"quic-lab-network", []string{"start"}, "network", []string{"start"}},
		{"quic-lab", []string{"network", "stop"}, "network", []string{"stop"}},
	} {
		mode, args := invocation(tc.name, tc.args)
		if mode != tc.mode || !reflect.DeepEqual(args, tc.rest) {
			t.Fatalf("%s %v: %s %v", tc.name, tc.args, mode, args)
		}
	}
}
