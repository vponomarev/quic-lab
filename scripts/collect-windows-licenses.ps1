param(
 [Parameter(Mandatory)][string]$DotNetRoot,
 [Parameter(Mandatory)][string]$NuGetPackages,
 [string]$RuntimeVersion='10.0.13',
 [Parameter(Mandatory)][string]$Output
)
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$Output=[IO.Path]::GetFullPath($Output)
if(Test-Path -LiteralPath $Output){throw 'Output directory already exists; choose a fresh directory'}
New-Item -ItemType Directory -Path $Output | Out-Null
$oldGOOS=$env:GOOS;$oldGOARCH=$env:GOARCH;$oldCGO=$env:CGO_ENABLED
Push-Location $root
try {
 $env:GOOS='windows';$env:GOARCH='amd64';$env:CGO_ENABLED='0'
 $lines=& go list -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}|{{if .Module.Replace}}{{.Module.Replace.Version}}{{end}}{{end}}' ./cmd/windows-service ./cmd/windows-vpn-engine
 if($LASTEXITCODE -ne 0){throw 'Windows dependency enumeration failed'}
 $manifest=@()
 foreach($line in ($lines | Where-Object {$_} | Sort-Object -Unique)) {
  $parts=$line.Split('|');$module=$parts[0];$version=$parts[1];$dir=$parts[2];$replacement=$parts[3]
  if($module -eq 'quiclab'){continue}
  $destination=Join-Path $Output ('go/'+$module.Replace('/','_'))
  $files=@(Get-ChildItem -LiteralPath $dir -Recurse -File | Where-Object {$_.Name -match '^(LICENSE|LICENCE|NOTICE|COPYING|COPYRIGHT|OFL)([.-].*)?$'})
  if($files.Count -eq 0){throw "No license found for $module"}
  $copied=@()
  foreach($file in $files){
   $relative=$file.FullName.Substring($dir.Length+1)
   $target=Join-Path $destination $relative
   New-Item -ItemType Directory -Path ([IO.Path]::GetDirectoryName($target)) -Force | Out-Null
   Copy-Item -LiteralPath $file.FullName -Destination $target
   $copied+=$target.Substring($Output.Length+1).Replace('\','/')
  }
  $manifest+=[ordered]@{module=$module;version=$version;replacement_version=$replacement;licenses=$copied}
 }
 $goRoot=& go env GOROOT
 New-Item -ItemType Directory -Path (Join-Path $Output 'go-runtime') | Out-Null
 Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination (Join-Path $Output 'go-runtime/LICENSE')
 New-Item -ItemType Directory -Path (Join-Path $Output 'dotnet') | Out-Null
 foreach($name in @('LICENSE.txt','ThirdPartyNotices.txt')){Copy-Item -LiteralPath (Join-Path $DotNetRoot $name) -Destination (Join-Path $Output "dotnet/$name")}
 foreach($package in @('microsoft.netcore.app.runtime.win-x64','microsoft.windowsdesktop.app.runtime.win-x64')){
  $packageRoot=Join-Path $NuGetPackages "$package/$RuntimeVersion"
  $runtimeLicenses=@(Get-ChildItem -LiteralPath $packageRoot -Recurse -File | Where-Object {$_.Name -match '^(LICENSE|LICENCE|NOTICE|COPYING|COPYRIGHT|OFL|THIRD-PARTY-NOTICES)([.-].*)?$'})
  if($runtimeLicenses.Count -eq 0){throw "Missing runtime licenses for $package $RuntimeVersion"}
  foreach($file in $runtimeLicenses){
   $relative=$file.FullName.Substring($packageRoot.Length+1)
   $target=Join-Path $Output "dotnet/$package/$RuntimeVersion/$relative"
   New-Item -ItemType Directory -Path ([IO.Path]::GetDirectoryName($target)) -Force | Out-Null
   Copy-Item -LiteralPath $file.FullName -Destination $target
  }
 }
 Copy-Item -LiteralPath (Join-Path $root 'internal/awg/netstack/LICENSE') -Destination (Join-Path $Output 'go/amnezia-netstack-LICENSE')
 $manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $Output 'go-modules.json') -Encoding utf8
 Write-Output "Collected licenses for $($manifest.Count) Windows Go dependencies and Go/.NET runtimes"
} finally {
 Pop-Location
 $env:GOOS=$oldGOOS;$env:GOARCH=$oldGOARCH;$env:CGO_ENABLED=$oldCGO
}