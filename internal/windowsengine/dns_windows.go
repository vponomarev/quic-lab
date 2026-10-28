//go:build windows

package windowsengine

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"
)

var runDNSCommand = networkScript

const recoverDNSScript = `$ErrorActionPreference='Stop';foreach($r in @(Get-DnsClientNrptRule)){if($r.Comment -match '^QUICLab-Windows-DNS\|\{[0-9A-Fa-f-]+\}\|[0-9]+\|[0-9]+$'){$parts=$r.Comment.Split('|');$p=Get-Process -Id ([int]$parts[2]) -ErrorAction SilentlyContinue;if($null -eq $p -or $p.StartTime.ToUniversalTime().Ticks -ne [long]$parts[3]){Remove-DnsClientNrptRule -Name $r.Name -Force}}}`

// RecoverWindowsDNS removes only abandoned rules; live owners and foreign policies remain.
func RecoverWindowsDNS(ctx context.Context) error {
	_, err := runDNSCommand(ctx, recoverDNSScript)
	if err != nil {
		return errors.New("stale owned DNS cleanup failed")
	}
	return nil
}

func (l *tunLink) configureDNS(ctx context.Context, s Settings) error {
	if !s.FullIPv4() {
		return nil
	}
	l.cleanupMu.Lock()
	defer l.cleanupMu.Unlock()
	l.dnsOwner = "QUICLab-Windows-DNS|" + l.guid.String()
	script := recoverDNSScript + `;if(@(Get-DnsClientNrptPolicy -Effective).Count -gt 0){throw 'Existing DNS policy'};$p=Get-Process -Id ` + strconv.Itoa(os.Getpid()) + `;$comment='` + l.dnsOwner + `|'+$p.Id+'|'+$p.StartTime.ToUniversalTime().Ticks;Add-DnsClientNrptRule -Namespace '.' -NameServers '` + s.DNSAddress() + `' -DisplayName 'QUIC Lab IPv4 VPN' -Comment $comment;Clear-DnsClientCache`
	if _, e := runDNSCommand(ctx, script); e != nil {
		return errors.New("owned DNS policy setup failed")
	}
	return nil
}
func (l *tunLink) cleanupDNS() error {
	l.cleanupMu.Lock()
	defer l.cleanupMu.Unlock()
	if l.dnsOwner == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := `$ErrorActionPreference='Stop';foreach($r in @(Get-DnsClientNrptRule)){if($null -ne $r.Comment -and $r.Comment.StartsWith('` + l.dnsOwner + `|')){Remove-DnsClientNrptRule -Name $r.Name -Force}};Clear-DnsClientCache`
	if _, e := runDNSCommand(ctx, script); e != nil {
		return errors.New("owned DNS policy cleanup failed")
	}
	l.dnsOwner = ""
	return nil
}
