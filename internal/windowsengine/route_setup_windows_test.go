//go:build windows

package windowsengine

import (
	"context"
	"golang.org/x/sys/windows"
	"testing"
	"time"
)

func TestRouteSetupUsesCompleteSnapshotAndRejectsCompetitors(t *testing.T) {
	old := runRouteCommand
	defer func() { runRouteCommand = old }()
	guid, _ := windows.GUIDFromString("{11111111-1111-1111-1111-111111111111}")
	for _, competitor := range []bool{false, true} {
		prelude := `$ErrorActionPreference='Stop';$script:table=@();$script:reads=0;
 function Get-NetAdapter {param([switch]$IncludeHidden);[pscustomobject]@{InterfaceGuid='{11111111-1111-1111-1111-111111111111}';ifIndex=99}}
 function Get-NetIPAddress {param($InterfaceIndex,$AddressFamily,$IPAddress,$ErrorAction);if($InterfaceIndex){[pscustomobject]@{AddressState='Preferred'}}}
 function Set-NetIPInterface {param($InterfaceIndex,$AddressFamily,$DadTransmits,$NlMtuBytes,$InterfaceMetric)}
 function New-NetIPAddress {param($InterfaceIndex,$IPAddress,$PrefixLength,$AddressFamily,$PolicyStore)}
 function New-NetRoute {param($DestinationPrefix,$InterfaceIndex,$NextHop,$RouteMetric,$PolicyStore);$script:table += [pscustomobject]@{DestinationPrefix=$DestinationPrefix;InterfaceIndex=$InterfaceIndex}}
 function Find-NetRoute {throw 'Unbounded per-prefix CIM lookup'}
 function Get-NetRoute {param($AddressFamily,$PolicyStore);$script:reads++;if($script:reads -ne 1){throw 'Repeated route table query'};$script:table};`
		if competitor {
			prelude += `$script:table += [pscustomobject]@{DestinationPrefix='198.51.100.1/32';InterfaceIndex=5};`
		}
		runRouteCommand = func(ctx context.Context, script string) ([]byte, error) { return networkScript(ctx, prelude+script) }
		s := valid()
		s.Targets = []string{"198.51.100.0/24", "203.0.113.0/24"}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := (&tunLink{guid: guid}).Configure(ctx, s)
		cancel()
		if !competitor && err != nil {
			t.Fatal("bounded route setup failed:", err)
		}
		if competitor && (err == nil || err.Error() != "subnet overlaps a competing route") {
			t.Fatal("more-specific foreign route not rejected by table verification:", err)
		}
	}
}
