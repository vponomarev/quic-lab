param(
 [ValidateSet('Install','Remove')][string]$Action='Install',
 [string]$Profile,
 [ValidatePattern('^S-1-5-21-[0-9-]+$')][string]$OwnerSID
)
$ErrorActionPreference='Stop'
$me=[Security.Principal.WindowsIdentity]::GetCurrent()
if(-not ([Security.Principal.WindowsPrincipal]::new($me)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){throw 'Run this script from Windows PowerShell as Administrator'}
if(-not [Environment]::Is64BitProcess){throw '64-bit Windows PowerShell required'}
$name='QuicLabWindowsPreview'
$root=Join-Path $env:ProgramFiles 'QUIC Lab Windows Preview'
$state=Join-Path $env:ProgramData 'QUIC Lab Windows Preview'
$exe=Join-Path $root 'quic-windows-service.exe'
if($Action -eq 'Remove'){
 $svc=Get-CimInstance Win32_Service -Filter "Name='$name'"
 if(-not $svc){Write-Output 'Service not installed';return}
 if(-not $svc.PathName.StartsWith('"'+$exe+'" -engine ')){throw 'Unexpected service path; refusing removal'}
 Stop-Service $name
 (Get-Service $name).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(30))
 sc.exe delete $name|Out-Null
 if($LASTEXITCODE -ne 0){throw 'Service removal failed'}
 Write-Output 'Service removed. Binaries and private profile retained in Program Files/ProgramData for deliberate cleanup.'
 return
}
if(-not $OwnerSID){throw 'Supply the normal desktop user SID from whoami /user'}
if((Get-Service $name -ErrorAction SilentlyContinue) -or (Test-Path -LiteralPath $root) -or (Test-Path -LiteralPath $state)){throw 'Existing preview installation/files found; this first installer does not overwrite them'}
if(-not $Profile -or (Get-Item -LiteralPath $Profile).Length -gt 1048576){throw 'Supply a private engine JSON profile <=1 MiB'}
$profileText=[IO.File]::ReadAllText((Resolve-Path -LiteralPath $Profile).Path,[Text.UTF8Encoding]::new($false,$true))
if([Text.Encoding]::UTF8.GetByteCount($profileText) -gt 1048576){throw 'Profile too large'}
$manifest=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'payload-sha256.json') -Raw|ConvertFrom-Json
if(-not $manifest.'quic-windows-service.exe' -or -not $manifest.'quic-windows-engine.exe' -or -not $manifest.'wintun.dll' -or -not $manifest.'ui/QuicLab.Windows.exe'){throw 'Incomplete payload manifest'}
$privateAcl=[Security.AccessControl.DirectorySecurity]::new()
$privateAcl.SetSecurityDescriptorSddlForm('O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)')
[IO.Directory]::CreateDirectory($state,$privateAcl)|Out-Null
# CreateDirectory may return an existing directory after a race. Never write secrets
# until ownership and every path component have been checked.
for($item=Get-Item -LiteralPath $state -Force;$item;$item=$item.Parent){
 if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse state path refused'}
}
$actualOwner=(Get-Acl -LiteralPath $state).GetOwner([Security.Principal.SecurityIdentifier]).Value
if($actualOwner -notin @('S-1-5-18','S-1-5-32-544')){throw 'Untrusted state owner; profile was not written'}
Set-Acl -LiteralPath $state -AclObject $privateAcl
$verifiedAcl=Get-Acl -LiteralPath $state
if(-not $verifiedAcl.AreAccessRulesProtected){throw 'State ACL inheritance is not protected'}
foreach($rule in $verifiedAcl.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier])){
 if($rule.AccessControlType -eq 'Allow' -and $rule.IdentityReference.Value -notin @('S-1-5-18','S-1-5-32-544')){throw 'Unexpected private state access'}
}

$publicAcl=[Security.AccessControl.DirectorySecurity]::new()
$publicAcl.SetSecurityDescriptorSddlForm('O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;GRGX;;;BU)')
[IO.Directory]::CreateDirectory($root,$publicAcl)|Out-Null
$created=$false
try{
 foreach($entry in $manifest.PSObject.Properties){
  $relative=$entry.Name
  if($relative -notmatch '^[a-zA-Z0-9_. /-]+$' -or $relative.StartsWith('/') -or @($relative.Split('/')|Where-Object {$_ -eq '..' -or $_ -eq '.' -or $_ -eq ''}).Count -gt 0 -or $entry.Value -notmatch '^[0-9a-fA-F]{64}$'){throw 'Invalid manifest path/hash'}
  $source=Join-Path $PSScriptRoot $relative
  $check=Get-Item -LiteralPath $source
  while($check){
   if($check.Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse payload refused'}
   if($check.FullName -eq $PSScriptRoot){break}
   if($check -is [IO.FileInfo]){$check=$check.Directory}else{$check=$check.Parent}
  }
  if(-not $check){throw 'Payload outside package'}
  if((Get-FileHash -LiteralPath $source).Hash -ne $entry.Value){throw 'Source payload hash mismatch'}
  $destination=Join-Path $root $relative
  [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($destination))|Out-Null
  Copy-Item -LiteralPath $source -Destination $destination
  if((Get-FileHash -LiteralPath $destination).Hash -ne $entry.Value){throw 'Installed payload hash mismatch'}
 }
 $sig=Get-AuthenticodeSignature -LiteralPath (Join-Path $root 'wintun.dll')
 if($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notmatch 'WireGuard'){throw 'Wintun signature invalid'}
 $config=Join-Path $state 'engine.json'
 [IO.File]::WriteAllText($config,$profileText,[Text.UTF8Encoding]::new($false))
 & (Join-Path $root 'quic-windows-engine.exe') -config $config -validate
 if($LASTEXITCODE -ne 0){throw 'Invalid engine profile; service not installed'}
 $command='"'+$exe+'" -engine -owner-sid '+$OwnerSID+' -data-dir "'+$state+'"'
 New-Service -Name $name -BinaryPathName $command -DisplayName 'QUIC Lab Windows Preview' -StartupType Automatic|Out-Null
 $created=$true
 Start-Service $name
 (Get-Service $name).WaitForStatus('Running',[TimeSpan]::FromSeconds(30))
 Write-Output ('Installed. Launch '+(Join-Path $root 'ui\QuicLab.Windows.exe')+' as your normal user. VPN is initially OFF.')
}catch{
 if($created){Stop-Service $name -ErrorAction SilentlyContinue;sc.exe delete $name|Out-Null}
 throw
}
