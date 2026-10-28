//go:build windows

package windowsengine

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"

	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"unicode/utf16"
)

var runRouteCommand = networkScript

// Routes target the owned adapter; full IPv4 also adds a separately owned DNS policy.
func (l *tunLink) Configure(ctx context.Context, s Settings) error {
	targets, _ := json.Marshal(s.Targets)
	script := `$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; $a=@(Get-NetAdapter -IncludeHidden | Where-Object {$_.InterfaceGuid -eq '` + l.guid.String() + `'}); if($a.Count -ne 1){throw 'Owned adapter missing'}; $idx=$a[0].ifIndex; if(Get-NetIPAddress -AddressFamily IPv4 -IPAddress '10.254.253.1' -ErrorAction SilentlyContinue){throw 'Adapter address already in use'}; Set-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv4 -DadTransmits 0; New-NetIPAddress -InterfaceIndex $idx -IPAddress '10.254.253.1' -PrefixLength 32 -AddressFamily IPv4 -PolicyStore ActiveStore | Out-Null; Set-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv4 -NlMtuBytes 1280 -InterfaceMetric 5000 -DadTransmits 0; $targets=ConvertFrom-Json '` + string(targets) + `'; foreach($target in $targets){New-NetRoute -DestinationPrefix $target -InterfaceIndex $idx -NextHop '0.0.0.0' -RouteMetric 1 -PolicyStore ActiveStore | Out-Null}; $ready=$false; for($n=0;$n -lt 50;$n++){ $ip=Get-NetIPAddress -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue; if($ip.AddressState -eq 'Preferred'){$ready=$true;break};Start-Sleep -Milliseconds 100}; if(-not $ready){throw 'Adapter address not ready'}; @{owner=$idx;table=@(Get-NetRoute -AddressFamily IPv4 -PolicyStore ActiveStore | ForEach-Object {@{target=$_.DestinationPrefix;interface_index=$_.InterfaceIndex}})} | ConvertTo-Json -Depth 3 -Compress;`
	out, err := runRouteCommand(ctx, script)
	if err != nil {
		return errors.New("owned adapter route setup failed")
	}

	var result struct {
		Owner uint32           `json:"owner"`
		Table []RouteSelection `json:"table"`
	}
	if json.Unmarshal(out, &result) != nil {
		return errors.New("invalid route selection response")
	}
	// Every requested prefix must exist on the ready owned adapter, with no
	// equal or more-specific foreign route. One table snapshot covers every
	// address in each prefix, unlike repeated lookups of prefix bases only.
	if err := VerifyRouteTable(result.Owner, s.Targets, result.Table); err != nil {
		return err
	}
	return l.configureDNS(ctx, s)
}

func networkScript(ctx context.Context, script string) ([]byte, error) {
	// The command line stays bounded even for thousands of generated routes.
	units := utf16.Encode([]rune(`[Console]::InputEncoding=[Text.Encoding]::UTF8;$s=[Console]::In.ReadToEnd();& ([scriptblock]::Create($s))`))
	b := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(b[2*i:], v)
	}
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	command.Stdin = strings.NewReader(script)
	hideWindow(command)
	return command.Output()
}
