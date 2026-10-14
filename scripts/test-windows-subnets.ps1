param([Parameter(Mandatory)][string]$Package,[Parameter(Mandatory)][string]$ExpectedHash,[Parameter(Mandatory)][string]$Result)
$ErrorActionPreference='Stop'
$me=[Security.Principal.WindowsIdentity]::GetCurrent()
if(-not ([Security.Principal.WindowsPrincipal]::new($me)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){throw 'Administrator required'}
$dir=Join-Path $env:ProgramFiles ('QUIC Lab Subnet Test '+[guid]::NewGuid().ToString('N'))
$old='C:\Program Files\QUIC Lab Engine MVP 4B3BDCD4-679ea5fc\private'
$exe=Join-Path $dir 'quic-windows-engine.exe'
$before=@(Get-NetRoute -DestinationPrefix '0.0.0.0/0'|Sort-Object InterfaceIndex|Select-Object InterfaceIndex,NextHop,RouteMetric)|ConvertTo-Json -Compress
$dnsBefore=@(Get-DnsClientServerAddress|Sort-Object InterfaceIndex,AddressFamily|Select-Object InterfaceIndex,AddressFamily,ServerAddresses)|ConvertTo-Json -Depth 4 -Compress
$r=@{ok=$false;tests=@();directory=$dir};$child=$null;$conflict=$null
try{
 if((Get-FileHash -LiteralPath (Join-Path $Package 'quic-windows-engine.exe')).Hash -ne $ExpectedHash){throw 'Engine hash mismatch'}
 if(@(Get-NetAdapter -IncludeHidden|Where-Object Name -like 'QUIC Lab MVP *').Count){throw 'Another engine is active'}
 $acl=[Security.AccessControl.DirectorySecurity]::new();$acl.SetSecurityDescriptorSddlForm('O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)')
 [IO.Directory]::CreateDirectory($dir,$acl)|Out-Null
 Copy-Item -LiteralPath (Join-Path $Package 'quic-windows-engine.exe') -Destination $exe
 Copy-Item -LiteralPath (Join-Path $Package 'wintun.dll') -Destination $dir
 if((Get-FileHash -LiteralPath $exe).Hash -ne $ExpectedHash){throw 'Installed hash mismatch'}
 $signature=Get-AuthenticodeSignature -LiteralPath (Join-Path $dir 'wintun.dll')
 if($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'WireGuard'){throw 'Wintun signature mismatch'}
 foreach($transport in @('quic','https')){
  $cfg=Get-Content -LiteralPath (Join-Path $old ($transport+'.json')) -Raw|ConvertFrom-Json
  if($cfg.transport -ne $transport -or $cfg.interface_index -ne 25){throw 'Unexpected fixture'}
  $cfg.targets=@('1.1.1.0/30')
  $file=Join-Path $dir ($transport+'.json');[IO.File]::WriteAllText($file,($cfg|ConvertTo-Json -Depth 12),[Text.UTF8Encoding]::new($false))
  $out=Join-Path $dir ($transport+'.log');$err=Join-Path $dir ($transport+'.err')
  $child=Start-Process -FilePath $exe -ArgumentList ('-config "'+$file+'" -duration 90s -probe -expected-exit 168.119.242.240') -WindowStyle Hidden -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
  $child.Handle | Out-Null
  if(-not $child.WaitForExit(110000)){throw 'Probe timeout'}
  $child.Refresh();$text=Get-Content -LiteralPath $out -Raw
  $r.lastOutput=[string]$text;$r.lastExitCode=$child.ExitCode
  if($child.ExitCode -ne 0 -or $text -notmatch 'dns_tcp_pass' -or $text -notmatch 'dns_udp_pass' -or $text -notmatch 'https_exit ip=168.119.242.240' -or $text -notmatch 'engine_stopped'){throw ('Subnet probe failed: '+$transport)}
  $r.tests+=($transport+': subnet HTTPS exit + DNS UDP/TCP + stop PASS')
 }
 # A foreign more-specific route must reject the subnet, even when its first IP selects Wintun.
 if(Get-NetRoute -DestinationPrefix '1.1.1.2/32' -ErrorAction SilentlyContinue){throw 'Conflict fixture already exists'}
 $gateway=@(Get-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex 25|Sort-Object RouteMetric)[0].NextHop
 if(-not $gateway){throw 'No physical gateway'}
 $conflict=New-NetRoute -DestinationPrefix '1.1.1.2/32' -InterfaceIndex 25 -NextHop $gateway -RouteMetric 99 -PolicyStore ActiveStore
 $out=Join-Path $dir 'conflict.log';$err=Join-Path $dir 'conflict.err'
 $child=Start-Process -FilePath $exe -ArgumentList ('-config "'+(Join-Path $dir 'quic.json')+'" -duration 30s') -WindowStyle Hidden -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
 $child.Handle | Out-Null
 if(-not $child.WaitForExit(45000)){throw 'Conflict timeout'}
 $child.Refresh();$text=Get-Content -LiteralPath $out -Raw
  $r.lastOutput=[string]$text;$r.lastExitCode=$child.ExitCode
 if($child.ExitCode -eq 0 -or $text -notmatch 'start_failed' -or $text -match 'engine_running'){throw 'More-specific bypass accepted'}
 $r.tests+='more-specific route rejected PASS';$r.ok=$true
}catch{$r.error=$_.Exception.Message}
finally{
 if($child -and -not $child.HasExited){Stop-Process -Id $child.Id -Force;$child.WaitForExit()}
 if($conflict){$conflict|Remove-NetRoute -Confirm:$false}
 $after=@(Get-NetRoute -DestinationPrefix '0.0.0.0/0'|Sort-Object InterfaceIndex|Select-Object InterfaceIndex,NextHop,RouteMetric)|ConvertTo-Json -Compress
 $dnsAfter=@(Get-DnsClientServerAddress|Sort-Object InterfaceIndex,AddressFamily|Select-Object InterfaceIndex,AddressFamily,ServerAddresses)|ConvertTo-Json -Depth 4 -Compress
 $r.defaultsUnchanged=$before -eq $after;$r.dnsUnchanged=$dnsBefore -eq $dnsAfter
 $adapterCount=& "$env:SystemRoot/System32/WindowsPowerShell/v1.0/powershell.exe" -NoProfile -Command "`$ErrorActionPreference='Stop'; @(Get-NetAdapter -IncludeHidden | Where-Object Name -like 'QUIC Lab MVP *').Count"
 if($LASTEXITCODE -ne 0){$r.ok=$false;$r.cleanupError='Adapter inventory failed';$r.adaptersRemaining=-1}else{$r.adaptersRemaining=[int]$adapterCount}
 $r.ok=$r.ok -and $r.defaultsUnchanged -and $r.dnsUnchanged -and $r.adaptersRemaining -eq 0
 $r|ConvertTo-Json -Depth 5|Set-Content -LiteralPath $Result -Encoding utf8
}
if(-not $r.ok){exit 1}
