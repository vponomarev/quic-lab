function Assert-TrustedPath([string]$Path) {
 if(-not [IO.Path]::IsPathRooted($Path)){throw 'Absolute path required'}
 for($item=Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue;$item;$item=$item.Parent){
  if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse installation path refused'}
  $owner=(Get-Acl -LiteralPath $item.FullName).GetOwner([Security.Principal.SecurityIdentifier]).Value
  if($item.FullName -eq $Path -and $owner -notin @('S-1-5-18','S-1-5-32-544')){throw 'Untrusted installation directory owner'}
 }
 if(-not (Test-Path -LiteralPath $Path)){
  $parent=[IO.Path]::GetDirectoryName($Path)
  if(-not (Test-Path -LiteralPath $parent -PathType Container)){throw 'Installation parent missing'}
  for($item=Get-Item -LiteralPath $parent -Force;$item;$item=$item.Parent){if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse installation parent refused'}}
 }
}
function Get-DesktopOwnerSID {
 # The desktop account remains the owner when UAC uses another administrator.
 Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class QuicLabSessionOwner {
 [DllImport("wtsapi32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
 static extern bool WTSQuerySessionInformation(IntPtr server, int session, int kind, out IntPtr value, out int size);
 [DllImport("wtsapi32.dll")] static extern void WTSFreeMemory(IntPtr value);
 public static string Read(int session, int kind) {
  IntPtr value; int size;
  if(!WTSQuerySessionInformation(IntPtr.Zero,session,kind,out value,out size)) throw new System.ComponentModel.Win32Exception();
  try {return Marshal.PtrToStringUni(value) ?? "";} finally {WTSFreeMemory(value);}
 }
}
'@ -ErrorAction Stop
 $session=[Diagnostics.Process]::GetCurrentProcess().SessionId
 $user=[QuicLabSessionOwner]::Read($session,5)
 $domain=[QuicLabSessionOwner]::Read($session,7)
 if(-not $user -or -not $domain){throw 'Interactive Windows desktop user required'}
 return ([Security.Principal.NTAccount]::new($domain,$user)).Translate([Security.Principal.SecurityIdentifier]).Value
}

function Assert-TrustedAcl($Acl,[bool]$PublicRead) {
 $owner=$Acl.GetOwner([Security.Principal.SecurityIdentifier]).Value
 if($owner -notin @('S-1-5-18','S-1-5-32-544')){throw 'Untrusted payload or settings owner'}
 $raw=[Security.AccessControl.RawSecurityDescriptor]::new($Acl.GetSecurityDescriptorSddlForm('All'))
 if($null -eq $raw.DiscretionaryAcl){throw 'Unrestricted ACL refused'}
 $writes=[Security.AccessControl.FileSystemRights]::Write -bor [Security.AccessControl.FileSystemRights]::Delete -bor [Security.AccessControl.FileSystemRights]::DeleteSubdirectoriesAndFiles -bor [Security.AccessControl.FileSystemRights]::ChangePermissions -bor [Security.AccessControl.FileSystemRights]::TakeOwnership
 foreach($rule in $Acl.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier])){
  if($rule.AccessControlType -eq 'Allow' -and $rule.IdentityReference.Value -notin @('S-1-5-18','S-1-5-32-544')){
   if(-not $PublicRead -or ($rule.FileSystemRights -band $writes)){throw 'Permissive payload or settings ACL refused'}
  }
 }
}
function Assert-TrustedTree([string]$Path,[bool]$PublicRead) {
 Assert-TrustedPath $Path
 if(-not (Test-Path -LiteralPath $Path)){return}
 $pending=[Collections.Generic.Stack[string]]::new();$pending.Push($Path)
 while($pending.Count){
  $item=Get-Item -LiteralPath $pending.Pop() -Force
  if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse payload or settings refused'}
  Assert-TrustedAcl (Get-Acl -LiteralPath $item.FullName) $PublicRead
  if($item -is [IO.DirectoryInfo]){foreach($child in Get-ChildItem -LiteralPath $item.FullName -Force){$pending.Push($child.FullName)}}
 }
}

function Set-PrivateDirectory([string]$Path,[bool]$PublicRead) {
 Assert-TrustedPath $Path
 $sddl='O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)'
 if($PublicRead){$sddl+='(A;OICI;GRGX;;;BU)'}
 $acl=[Security.AccessControl.DirectorySecurity]::new();$acl.SetSecurityDescriptorSddlForm($sddl)
 [IO.Directory]::CreateDirectory($Path,$acl)|Out-Null
 Assert-TrustedPath $Path
 Set-Acl -LiteralPath $Path -AclObject $acl
 Assert-TrustedAcl (Get-Acl -LiteralPath $Path) $PublicRead
}
function Get-OwnedService {
 $svc=Get-CimInstance Win32_Service -Filter "Name='$ServiceName'"
 if(-not $svc){return $null}
 $pattern='^"'+[regex]::Escape($exe)+'" -engine -owner-sid (S-1-(?:5-21|12-1)-[0-9-]+) -data-dir "'+[regex]::Escape($StateDir)+'"$'
 if($svc.PathName -notmatch $pattern -or $svc.StartName -ne 'LocalSystem'){throw 'Foreign service registration; refusing changes'}
 return @{Service=$svc;Owner=$Matches[1]}
}
function Stop-OwnedService($owned) {
 if(-not $owned){return}
 $svc=Get-Service -Name $ServiceName
 if($svc.Status -ne 'Stopped'){Stop-Service -Name $ServiceName -ErrorAction Stop;$svc.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(40))}
}
function Assert-Payload {
 $manifest=Get-Content -LiteralPath (Join-Path $InstallDir 'payload-sha256.json') -Raw|ConvertFrom-Json
 foreach($required in @('quic-windows-service.exe','quic-windows-engine.exe','wintun.dll','ui/QuicLab.Windows.exe')){if(-not $manifest.$required){throw 'Incomplete payload'}}
 foreach($entry in $manifest.PSObject.Properties){
  if($entry.Name -notmatch '^[a-zA-Z0-9_. /-]+$' -or $entry.Name.StartsWith('/') -or @($entry.Name.Split('/')|Where-Object {$_ -in @('..','.','')}).Count -gt 0 -or $entry.Value -notmatch '^[a-fA-F0-9]{64}$'){throw 'Invalid payload manifest'}
  $path=Join-Path $InstallDir $entry.Name
  for($item=Get-Item -LiteralPath $path -Force;$item;){
   if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Reparse payload refused'}
   if($item.FullName -eq $InstallDir){break}
   if($item -is [IO.FileInfo]){$item=$item.Directory}else{$item=$item.Parent}
  }
  if(-not $item -or (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -ne $entry.Value){throw 'Payload checksum mismatch'}
 }
 $sig=Get-AuthenticodeSignature -LiteralPath (Join-Path $InstallDir 'wintun.dll')
 if($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notmatch 'WireGuard'){throw 'Wintun signature invalid'}
}
