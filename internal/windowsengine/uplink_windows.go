//go:build windows

package windowsengine

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// Discover examines only physical IPv4 defaults. Our virtual routes are never candidates.
func DiscoverWindowsUplink(ctx context.Context, s Settings) (Uplink, error) {
	index := uint32(0)
	if s.UplinkMode != "auto" {
		index = s.InterfaceIndex
	}
	script := `$ErrorActionPreference='Stop';$wanted=` + strconv.FormatUint(uint64(index), 10) + `; $adapters=@(Get-NetAdapter -IncludeHidden | Where-Object {$_.Status -eq 'Up' -and (($wanted -gt 0 -and $_.ifIndex -eq $wanted) -or ($wanted -eq 0 -and $_.HardwareInterface))});$choices=@();foreach($a in $adapters){$ipif=Get-NetIPInterface -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue;$ip=@(Get-NetIPAddress -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue|Where-Object {$_.AddressState -eq 'Preferred' -and $_.IPAddress -notlike '169.254.*'}|Sort-Object IPAddress);if($ip.Count -eq 0){continue};foreach($r in @(Get-NetRoute -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -PolicyStore ActiveStore -ErrorAction SilentlyContinue)){$choices+=@{index=[uint32]$a.ifIndex;gateway=[string]$r.NextHop;address=[string]$ip[0].IPAddress;metric=[uint64]$r.RouteMetric+[uint64]$ipif.InterfaceMetric}}};$best=@($choices|Sort-Object @{Expression={$_.metric}},@{Expression={$_.index}});if($best.Count -eq 0){throw 'No IPv4 uplink'};$local=@();foreach($a in @(Get-NetAdapter -IncludeHidden|Where-Object {$_.Status -eq 'Up' -and $_.InterfaceDescription -notlike 'QUIC Lab Tunnel*'})){foreach($r in @(Get-NetRoute -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -PolicyStore ActiveStore -ErrorAction SilentlyContinue|Where-Object {$_.DestinationPrefix -ne '0.0.0.0/0'})){$local+=[string]$r.DestinationPrefix}};$b=$best[0];@{index=$b.index;gateway=$b.gateway;address=$b.address;local=@($local|Sort-Object -Unique)}|ConvertTo-Json -Compress`
	b, e := networkScript(ctx, script)
	if e != nil {
		return Uplink{}, errors.New("no_ipv4_uplink")
	}
	var u Uplink
	if json.Unmarshal(b, &u) != nil || u.Index == 0 {
		return Uplink{}, errors.New("invalid uplink")
	}
	return u, nil
}
func NewWindowsAutomatic(s Settings) *Automatic {
	return NewAutomatic(s, DiscoverWindowsUplink, func(s Settings) ManagedEngine { return New(s, WindowsPlatform{}, &NativeGateway{}) }, WatchWindowsIPv4, 30*time.Second)
}
