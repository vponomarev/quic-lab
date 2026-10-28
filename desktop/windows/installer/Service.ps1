param(
 [Parameter(Mandatory)][ValidateSet('Prepare','Install','Remove','Resume','Owner')][string]$Action,
 [Parameter(Mandatory)][string]$InstallDir,
 [Parameter(Mandatory)][string]$StateDir,
 [string]$OwnerSID,
 [string]$OwnerFile,
 [ValidatePattern('^QuicLab[A-Za-z0-9]+$')][string]$ServiceName='QuicLabWindowsPreview'
)
$ErrorActionPreference='Stop'

. (Join-Path $PSScriptRoot 'Service.Common.ps1')

try {
 $me=[Security.Principal.WindowsIdentity]::GetCurrent()
 if(-not ([Security.Principal.WindowsPrincipal]::new($me)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){throw 'Administrator required'}
 if(-not [Environment]::Is64BitProcess){throw 'Windows PowerShell x64 required'}
 if($Action -eq 'Owner'){
  if(-not [IO.Path]::IsPathRooted($OwnerFile)){throw 'Absolute owner handoff file required'}
  [IO.File]::WriteAllText($OwnerFile,(Get-DesktopOwnerSID));exit 0
 }
 $InstallDir=[IO.Path]::GetFullPath($InstallDir).TrimEnd('\');$StateDir=[IO.Path]::GetFullPath($StateDir).TrimEnd('\')
 $exe=Join-Path $InstallDir 'quic-windows-service.exe'
 Assert-TrustedPath $InstallDir;Assert-TrustedPath $StateDir
 $owned=Get-OwnedService
 if($Action -eq 'Remove'){
  Stop-OwnedService $owned
  if($owned){& "$env:SystemRoot/System32/sc.exe" delete $ServiceName|Out-Null;if($LASTEXITCODE -ne 0){throw 'Service deletion failed'}}
  Write-Output 'Service removed; private settings retained.';exit 0
 }
 if($Action -eq 'Resume'){
  if($owned -and (Get-Service $ServiceName).Status -eq 'Stopped'){Start-Service $ServiceName}
  exit 0
 }
 if($owned){$OwnerSID=$owned.Owner}
 if($OwnerSID -notmatch '^S-1-(?:5-21|12-1)-[0-9-]+$'){throw 'Invalid desktop user SID'}
 $sid=[Security.Principal.SecurityIdentifier]::new($OwnerSID)
 $null=$sid.Translate([Security.Principal.NTAccount])
 if($Action -eq 'Prepare'){
  # Fail before replacing any files when another application owns this service.
  Assert-TrustedTree $InstallDir $true;Assert-TrustedTree $StateDir $false
  Set-PrivateDirectory $InstallDir $true;Set-PrivateDirectory $StateDir $false
  Stop-OwnedService $owned
  Write-Output 'Prepared; VPN will remain off.';exit 0
 }
 Set-PrivateDirectory $InstallDir $true;Set-PrivateDirectory $StateDir $false
 Assert-Payload
 $created=$false
 try {
  if(-not $owned){
   $command='"'+$exe+'" -engine -owner-sid '+$OwnerSID+' -data-dir "'+$StateDir+'"'
   New-Service -Name $ServiceName -BinaryPathName $command -DisplayName 'QUIC Lab Windows' -StartupType Automatic|Out-Null
   $created=$true
  }else{Set-Service -Name $ServiceName -StartupType Automatic}
  Start-Service -Name $ServiceName
  (Get-Service $ServiceName).WaitForStatus('Running',[TimeSpan]::FromSeconds(30))
  Start-Sleep -Milliseconds 500
  if((Get-Service $ServiceName).Status -ne 'Running'){throw 'Service exited during startup'}
 }catch{
  if($created){Stop-Service $ServiceName -ErrorAction SilentlyContinue;& "$env:SystemRoot/System32/sc.exe" delete $ServiceName|Out-Null}
  throw
 }
 Write-Output 'Service installed; VPN is OFF.'
}catch{Write-Error $_ -ErrorAction Continue;exit 1}
