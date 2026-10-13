param([Parameter(Mandatory)][string]$Source,[Parameter(Mandatory)][string]$ExpectedHash,[switch]$SecurityOnly)
$ErrorActionPreference='Stop'
$identity=[Security.Principal.WindowsIdentity]::GetCurrent()
if (-not ([Security.Principal.WindowsPrincipal]::new($identity)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){throw 'Administrator required'}
$destination=Join-Path $env:ProgramFiles ('QUIC Lab Engine MVP '+$ExpectedHash.Substring(0,8)+'-'+[guid]::NewGuid().ToString('N').Substring(0,8))
if(Test-Path -LiteralPath $destination){throw 'Refusing existing test directory'}
if((Get-FileHash -LiteralPath (Join-Path $Source 'quic-windows-engine.exe')).Hash -ne $ExpectedHash){throw 'Engine hash mismatch'}
if((Get-AuthenticodeSignature (Join-Path $Source 'wintun.dll')).Status -ne 'Valid'){throw 'Wintun signature invalid'}
New-Item -ItemType Directory -Path $destination | Out-Null
Copy-Item -LiteralPath (Join-Path $Source 'quic-windows-engine.exe') -Destination $destination
Copy-Item -LiteralPath (Join-Path $Source 'wintun.dll') -Destination $destination
if((Get-FileHash (Join-Path $destination 'quic-windows-engine.exe')).Hash -ne $ExpectedHash){throw 'Copied engine hash mismatch'}
if((Get-AuthenticodeSignature (Join-Path $destination 'wintun.dll')).Status -ne 'Valid'){throw 'Copied DLL signature invalid'}
$private=Join-Path $destination 'private'
New-Item -ItemType Directory -Path $private | Out-Null
& icacls.exe $private /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if($LASTEXITCODE -ne 0){throw 'Private ACL failed'}
$before=@(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object InterfaceIndex | Select-Object InterfaceIndex,NextHop,RouteMetric) | ConvertTo-Json -Compress
$dnsBefore=@(Get-DnsClientServerAddress | Sort-Object InterfaceIndex,AddressFamily | Select-Object InterfaceIndex,AddressFamily,ServerAddresses) | ConvertTo-Json -Compress -Depth 4
$result=@{Started=(Get-Date).ToString('o');Tests=@();Success=$false}
try {
 foreach($transport in @('quic','https','quic','https')) {
  $config=Join-Path $private ($transport+'.json')
  Copy-Item -LiteralPath (Join-Path $Source ('mvp-private/'+$transport+'.json')) -Destination $config
  if($SecurityOnly){continue}
  $out=Join-Path $destination ($transport+'.log'); $err=Join-Path $destination ($transport+'.err.log')
  $args='-config "'+$config+'" -duration 90s -probe -expected-exit 168.119.242.240'
  $p=Start-Process -FilePath (Join-Path $destination 'quic-windows-engine.exe') -ArgumentList $args -WindowStyle Hidden -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
  if(-not $p.WaitForExit(110000)) {Stop-Process -Id $p.Id;throw 'Engine timeout'}
  $log=Get-Content -LiteralPath $out -Raw
  $pass=($log -match 'dns_udp_pass' -and $log -match 'dns_tcp_pass' -and $log -match 'https_exit ip=168\.119\.242\.240' -and $log -match 'engine_stopped' -and $log -notmatch 'failed')
  $result.Tests+=@{Transport=$transport;Pass=$pass}
  if(-not $pass){throw "$transport probe failed"}
 }
 # A rejected TLS identity must never install live routes or survive as a TUN.
 $bad=Get-Content (Join-Path $private 'quic.json') -Raw | ConvertFrom-Json
 $bad.verify_name='wrong.invalid'
 $badConfig=Join-Path $private 'rejected.json'
 [IO.File]::WriteAllText($badConfig,($bad | ConvertTo-Json),[Text.UTF8Encoding]::new($false))
 $out=Join-Path $destination 'rejected.log';$err=Join-Path $destination 'rejected.err.log'
 $p=Start-Process -FilePath (Join-Path $destination 'quic-windows-engine.exe') -ArgumentList ('-config "'+$badConfig+'" -duration 30s -probe -expected-exit 168.119.242.240') -WindowStyle Hidden -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
 if(-not $p.WaitForExit(45000)){Stop-Process -Id $p.Id;throw 'Rejected identity timeout'}
 $result.RejectedIdentity=(Get-Content $out -Raw) -match 'start_failed' -and -not ((Get-Content $out -Raw) -match 'engine_running')
 if(-not $result.RejectedIdentity){throw 'TLS rejection not verified; inspect rejected.log'}
 # Pre-existing local host route must not silently win over a requested VPN route.
 $conflict=Get-Content (Join-Path $private 'quic.json') -Raw | ConvertFrom-Json
 $local=@(Get-NetIPAddress -InterfaceIndex $conflict.interface_index -AddressFamily IPv4 | Where-Object AddressState -eq 'Preferred')[0].IPAddress
 if(-not $local){throw 'Cannot select local route conflict fixture'}
 $conflict.targets=@($local+'/32')
 $conflictConfig=Join-Path $private 'conflict.json'
 [IO.File]::WriteAllText($conflictConfig,($conflict | ConvertTo-Json),[Text.UTF8Encoding]::new($false))
 $out=Join-Path $destination 'conflict.log';$err=Join-Path $destination 'conflict.err.log'
 $p=Start-Process -FilePath (Join-Path $destination 'quic-windows-engine.exe') -ArgumentList ('-config "'+$conflictConfig+'" -duration 30s') -WindowStyle Hidden -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
 if(-not $p.WaitForExit(45000)){Stop-Process -Id $p.Id;throw 'Conflict test timeout'}
 $result.ConflictingRouteRejected=(Get-Content $out -Raw) -match 'start_failed' -and -not ((Get-Content $out -Raw) -match 'engine_running')
 if(-not $result.ConflictingRouteRejected){throw 'Conflicting route accepted'}
 # Kill only the exact child returned here; check automatic driver-owned resource cleanup.
 $out=Join-Path $destination 'crash.log';$err=Join-Path $destination 'crash.err.log'
 $p=Start-Process -FilePath (Join-Path $destination 'quic-windows-engine.exe') -ArgumentList ('-config "'+(Join-Path $private 'quic.json')+'" -duration 60s') -WindowStyle Hidden -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
 $ready=$false
 for($n=0;$n -lt 100;$n++){if((Get-Content $out -Raw -ErrorAction SilentlyContinue) -match 'engine_running'){$ready=$true;break};if($p.HasExited){break};Start-Sleep -Milliseconds 200}
 if(-not $ready){if(-not $p.HasExited){Stop-Process -Id $p.Id};throw 'Crash test did not start'}
 Stop-Process -Id $p.Id -Force
 $p.WaitForExit()
 Start-Sleep -Seconds 2
 $result.CrashAdapterCleanup=@(Get-NetAdapter -IncludeHidden | Where-Object Name -like 'QUIC Lab MVP *').Count -eq 0
 if(-not $result.CrashAdapterCleanup){throw 'Adapter remains after crash'}
 $result.Success=$true
} catch {$result.Error=$_.Exception.Message} finally {
 $after=@(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object InterfaceIndex | Select-Object InterfaceIndex,NextHop,RouteMetric) | ConvertTo-Json -Compress
 $dnsAfter=@(Get-DnsClientServerAddress | Sort-Object InterfaceIndex,AddressFamily | Select-Object InterfaceIndex,AddressFamily,ServerAddresses) | ConvertTo-Json -Compress -Depth 4
 $result.DefaultRoutesUnchanged=($before -eq $after);$result.DNSUnchanged=($dnsBefore -eq $dnsAfter)
 $result.AdapterCount=@(Get-NetAdapter -IncludeHidden | Where-Object Name -like 'QUIC Lab MVP *').Count
 $result.Success=$result.Success -and $result.DefaultRoutesUnchanged -and $result.DNSUnchanged -and $result.AdapterCount -eq 0
 $result | ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 (Join-Path $destination 'result.json')
}





