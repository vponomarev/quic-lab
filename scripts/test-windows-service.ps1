param(
 [ValidateSet('Install','Update','Remove')][string]$Action='Install',
 [string]$Binary,
 [string]$ExpectedSHA256,
 [ValidatePattern('^S-1-5-21-[0-9-]+$')][string]$OwnerSID
)
$ErrorActionPreference='Stop'
$identity=[Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal($identity)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {throw 'Administrator required'}
$name='QuicLabWindowsPreview'
$directory=Join-Path $env:ProgramFiles 'QUIC Lab Windows Preview Fixture'
$destination=Join-Path $directory 'quic-windows-service.exe'
$state=Join-Path $env:ProgramData 'QUIC Lab Windows Preview Fixture'
if ($Action -eq 'Install') {
 if (Get-Service -Name $name -ErrorAction SilentlyContinue) {throw 'Test service already exists; refusing replacement'}
 if (-not $OwnerSID -or -not $ExpectedSHA256 -or (Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash -ne $ExpectedSHA256) {throw 'Invalid owner or binary hash'}
 if ((Test-Path -LiteralPath $directory) -or (Test-Path -LiteralPath $state)) {throw 'Test binary/state directory already exists; inspect ownership before reuse'}
 New-Item -ItemType Directory -Path $directory | Out-Null
 Copy-Item -LiteralPath $Binary -Destination $destination
 if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash -ne $ExpectedSHA256) {throw 'Copied binary hash mismatch'}
 $command='"'+$destination+'" -owner-sid '+$OwnerSID+' -data-dir "'+$state+'"'
 New-Service -Name $name -BinaryPathName $command -DisplayName 'QUIC Lab Windows Preview Fixture (no-network fixture)' -StartupType Manual | Out-Null
 try {Start-Service -Name $name;(Get-Service -Name $name).WaitForStatus('Running',[TimeSpan]::FromSeconds(15))} catch {Stop-Service -Name $name -ErrorAction SilentlyContinue;sc.exe delete $name | Out-Null;throw}
} elseif ($Action -eq 'Update') {
 $service=Get-CimInstance Win32_Service -Filter "Name='$name'"
 if (-not $service -or -not $service.PathName.StartsWith('"'+$destination+'" ')) {throw 'Unexpected service path'}
 $stateOwner=(Get-Acl -LiteralPath $state).GetOwner([Security.Principal.SecurityIdentifier]).Value
 if ($stateOwner -notin @('S-1-5-18','S-1-5-32-544')) {throw 'Untrusted state owner'}
 if (-not $ExpectedSHA256 -or (Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash -ne $ExpectedSHA256) {throw 'Binary hash mismatch'}
 Stop-Service -Name $name
 (Get-Service -Name $name).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(15))
 $backup=Join-Path $directory 'quic-windows-service.previous.exe'
 Copy-Item -LiteralPath $destination -Destination $backup -Force
 try {
  Copy-Item -LiteralPath $Binary -Destination $destination -Force
  if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash -ne $ExpectedSHA256) {throw 'Copied hash mismatch'}
  Start-Service -Name $name
  (Get-Service -Name $name).WaitForStatus('Running',[TimeSpan]::FromSeconds(15))
 } catch {Copy-Item -LiteralPath $backup -Destination $destination -Force;Start-Service -Name $name;throw}
} else {
 $service=Get-CimInstance Win32_Service -Filter "Name='$name'"
 if (-not $service) {return}
 if (-not $service.PathName.StartsWith('"'+$destination+'" ')) {throw 'Unexpected service path; refusing removal'}
 Stop-Service -Name $name
 sc.exe delete $name
 if ($LASTEXITCODE -ne 0) {throw 'Service deletion failed'}
 # Preserve binaries/state for audit; never recursively delete a computed path here.
}
