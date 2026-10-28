//go:build windows

package windowsengine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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

func TestCombinedDNSSetupKeepsLiveAndForeignRules(t *testing.T) {
	old := runDNSCommand
	defer func() { runDNSCommand = old }()
	// The production PowerShell is executed; only OS mutations are substituted.
	prelude := `$ErrorActionPreference='Stop';$script:removed=@();$script:added=0;$stamp=[datetime]'2026-01-01T00:00:00Z';
 function Get-DnsClientNrptRule {@([pscustomobject]@{Name='stale';Comment='QUICLab-Windows-DNS|{11111111-1111-1111-1111-111111111111}|100|1'},[pscustomobject]@{Name='live';Comment=('QUICLab-Windows-DNS|{22222222-2222-2222-2222-222222222222}|200|'+$stamp.ToUniversalTime().Ticks)},[pscustomobject]@{Name='foreign';Comment='managed by another application'})}
 function Get-Process {param($Id,$ErrorAction);if($Id -ne 100){[pscustomobject]@{Id=$Id;StartTime=$stamp}}}
 function Remove-DnsClientNrptRule {param($Name,[switch]$Force);$script:removed+=$Name}
 function Add-DnsClientNrptRule {param($Namespace,$NameServers,$DisplayName,$Comment);if($script:removed.Count -ne 1 -or $script:removed[0] -ne 'stale'){throw 'Wrong recovery ownership'};$script:added++}
 function Clear-DnsClientCache {}
 `
	for _, conflict := range []bool{false, true} {
		policy := `function Get-DnsClientNrptPolicy {param([switch]$Effective)}`
		if conflict {
			policy = `function Get-DnsClientNrptPolicy {param([switch]$Effective);[pscustomobject]@{Namespace='.'}}`
		}
		runDNSCommand = func(ctx context.Context, s string) ([]byte, error) {
			return networkScript(ctx, prelude+policy+`;try {& {`+s+`}} finally {if($script:removed.Count -ne 1 -or $script:removed[0] -ne 'stale'){throw 'Foreign/live rule removed'}}`)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := (&tunLink{}).configureDNS(ctx, Settings{RoutingMode: "all_ipv4"})
		cancel()
		if conflict != (err != nil) {
			t.Fatalf("foreign policy conflict=%v err=%v", conflict, err)
		}
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
	if err := l.configureDNS(context.Background(), s); err != nil || calls != 1 {
		t.Fatal("DNS recovery and installation must use one bounded command", calls, err)
	}
	if err := l.cleanupDNS(); err != nil || calls != 2 {
		t.Fatal("missing cleanup")
	}
}
