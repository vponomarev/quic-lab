//go:build windows

package windowsengine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDNSCleanupRetainsOwnerOnFailure(t *testing.T) {
	old := runDNSCommand
	defer func() { runDNSCommand = old }()
	calls := 0
	runDNSCommand = func(context.Context, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("failure")
		}
		return nil, nil
	}
	l := &tunLink{dnsOwner: "QUICLab-Windows-DNS|{11111111-1111-1111-1111-111111111111}"}
	if l.cleanupDNS() == nil || l.dnsOwner == "" {
		t.Fatal("lost cleanup ownership")
	}
	if l.cleanupDNS() != nil || l.dnsOwner != "" {
		t.Fatal("cleanup not retryable")
	}
	if l.cleanupDNS() != nil || calls != 2 {
		t.Fatal("duplicate cleanup")
	}
}
func TestDNSPolicyIsOwnedAndSubnetModeIsUntouched(t *testing.T) {
	old := runDNSCommand
	defer func() { runDNSCommand = old }()
	calls := 0
	runDNSCommand = func(_ context.Context, s string) ([]byte, error) {
		calls++
		if !strings.Contains(s, "QUICLab-Windows-DNS") {
			t.Fatal("missing ownership")
		}
		return nil, nil
	}
	l := &tunLink{}
	s := valid()
	if err := l.configureDNS(context.Background(), s); err != nil || calls != 0 {
		t.Fatal("subnet mode changes DNS")
	}
	s.RoutingMode = "all_ipv4"
	if err := l.configureDNS(context.Background(), s); err != nil || calls != 2 {
		t.Fatal("full mode missing DNS")
	}
	if err := l.cleanupDNS(); err != nil || calls != 3 {
		t.Fatal("missing cleanup")
	}
}
