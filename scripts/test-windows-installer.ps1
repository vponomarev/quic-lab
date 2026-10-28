$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot '../desktop/windows/installer/Service.Common.ps1')
$ServiceName='QuicLabInstallerTest'
$InstallDir='C:\Program Files\QUIC Lab Installer Test'
$StateDir='C:\ProgramData\QUIC Lab Installer Test'
$exe=Join-Path $InstallDir 'quic-windows-service.exe'
$desktopSID='S-1-5-21-100-200-300-1001'
$valid='"'+$exe+'" -engine -owner-sid '+$desktopSID+' -data-dir "'+$StateDir+'"'
$script:record=$null
function Get-CimInstance {param($ClassName,$Filter) return $script:record}
if($null -ne (Get-OwnedService)){throw 'Absent service accepted as owned'}
$script:record=[pscustomobject]@{PathName=$valid;StartName='LocalSystem'}
if((Get-OwnedService).Owner -ne $desktopSID){throw 'Existing service owner not preserved'}
foreach($bad in @($valid.Replace($exe,'C:\other.exe'),($valid+' -fixture'),$valid.Replace($StateDir,'C:\private'),$valid.Replace($desktopSID,'S-1-5-18'))){
 $script:record=[pscustomobject]@{PathName=$bad;StartName='LocalSystem'}
 $refused=$false;try{$null=Get-OwnedService}catch{$refused=$true};if(-not $refused){throw 'Foreign service registration accepted'}
}
$script:record=[pscustomobject]@{PathName=$valid;StartName='OtherUser'}
$refused=$false;try{$null=Get-OwnedService}catch{$refused=$true};if(-not $refused){throw 'Foreign service account accepted'}
Write-Output 'Service ownership, owner preservation and refusal contracts PASS'

foreach($case in @(
 @{Public=$true; Sddl='O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;GRGX;;;BU)';Accept=$true},
 @{Public=$true; Sddl='O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FW;;;BU)';Accept=$false},
 @{Public=$false;Sddl='O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;GR;;;BU)';Accept=$false},
 @{Public=$false;Sddl='O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)';Accept=$true}
)){
 $acl=[Security.AccessControl.FileSecurity]::new();$acl.SetSecurityDescriptorSddlForm($case.Sddl)
 $accepted=$true;try{Assert-TrustedAcl $acl $case.Public}catch{$accepted=$false}
 if($accepted -ne $case.Accept){throw 'Installer ACL policy failed'}
}
Write-Output 'Private state confidentiality and executable write protection PASS'

# Execute the real hash/path verifier on a small isolated package.
$temp=Join-Path ([IO.Path]::GetTempPath()) ('quic-installer-test-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path (Join-Path $temp 'ui') -Force|Out-Null
try {
 $InstallDir=$temp
 $manifest=[ordered]@{}
 foreach($name in @('quic-windows-service.exe','quic-windows-engine.exe','wintun.dll','ui/QuicLab.Windows.exe')){
  $path=Join-Path $temp $name;[IO.File]::WriteAllText($path,'controlled payload');$manifest[$name]=(Get-FileHash $path).Hash
 }
 function Get-AuthenticodeSignature {param($LiteralPath) return [pscustomobject]@{Status='Valid';SignerCertificate=[pscustomobject]@{Subject='CN=WireGuard LLC'}}}
 $manifest|ConvertTo-Json|Set-Content (Join-Path $temp 'payload-sha256.json')
 Assert-Payload
 [IO.File]::WriteAllText((Join-Path $temp 'quic-windows-service.exe'),'tampered')
 $refused=$false;try{Assert-Payload}catch{$refused=$true};if(-not $refused){throw 'Tampered service accepted'}
 [IO.File]::WriteAllText((Join-Path $temp 'quic-windows-service.exe'),'controlled payload')
 $manifest['../escape']='A'*64;$manifest|ConvertTo-Json|Set-Content (Join-Path $temp 'payload-sha256.json')
 $refused=$false;try{Assert-Payload}catch{$refused=$true};if(-not $refused){throw 'Escaping manifest accepted'}
 Write-Output 'Installer payload integrity contracts PASS'
}finally{
 $resolved=[IO.Path]::GetFullPath($temp);$parent=[IO.Path]::GetFullPath([IO.Path]::GetTempPath())
 if($resolved.StartsWith($parent,[StringComparison]::OrdinalIgnoreCase) -and [IO.Path]::GetFileName($resolved).StartsWith('quic-installer-test-')){Remove-Item -LiteralPath $resolved -Recurse -Force}
}
