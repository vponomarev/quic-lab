param(
 [Parameter(Mandatory)][string]$DotNet,
 [Parameter(Mandatory)][string]$ISCC,
 [Parameter(Mandatory)][string]$Wintun,
 [Parameter(Mandatory)][string]$WintunLicense,
 [Parameter(Mandatory)][string]$ThirdPartyLicenses,
 [Parameter(Mandatory)][string]$Output,
 [string]$Version='0.1.0-beta.2',
 [string]$Revision='development'
)
$ErrorActionPreference='Stop'
if($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.]+)?$'){throw 'Invalid version'}
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$Output=[IO.Path]::GetFullPath($Output)
if(Test-Path -LiteralPath $Output){throw 'Output directory already exists; choose a fresh directory'}
$payload=Join-Path $Output 'package'
New-Item -ItemType Directory -Path $payload -Force|Out-Null
$env:DOTNET_CLI_TELEMETRY_OPTOUT='1';$env:DOTNET_SKIP_FIRST_TIME_EXPERIENCE='1'
$env:GOOS='windows';$env:GOARCH='amd64';$env:CGO_ENABLED='0'
Push-Location $root
try{
 & go build -trimpath -buildvcs=false -ldflags='-s -w' -o (Join-Path $payload 'quic-windows-service.exe') ./cmd/windows-service
 if($LASTEXITCODE -ne 0){throw 'Service build failed'}
 & go build -trimpath -buildvcs=false -ldflags='-s -w' -o (Join-Path $payload 'quic-windows-engine.exe') ./cmd/windows-vpn-engine
 if($LASTEXITCODE -ne 0){throw 'Engine build failed'}
 & $DotNet publish desktop/windows/QuicLab.Windows -c Release -r win-x64 --self-contained true -p:Version=0.1.0 -p:InformationalVersion=$Version -o (Join-Path $payload 'ui')
 if($LASTEXITCODE -ne 0){throw 'UI publish failed'}
 $sig=Get-AuthenticodeSignature -LiteralPath $Wintun
 if($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notmatch 'WireGuard'){throw 'Wintun signature invalid'}
 Copy-Item -LiteralPath $Wintun -Destination (Join-Path $payload 'wintun.dll')
 Copy-Item -LiteralPath $WintunLicense -Destination (Join-Path $payload 'LICENSE-Wintun.txt')
 Copy-Item -LiteralPath $ThirdPartyLicenses -Destination (Join-Path $payload 'licenses') -Recurse
 foreach($name in @('Service.ps1','Service.Common.ps1','SetupInfo.txt')){Copy-Item -LiteralPath (Join-Path $root "desktop/windows/installer/$name") -Destination $payload}
 Copy-Item -LiteralPath (Join-Path $root 'THIRD_PARTY_NOTICES.md') -Destination $payload
 [IO.File]::WriteAllText((Join-Path $payload 'VERSION.txt'),"Windows $Version`nSource: $Revision`n")
 $manifest=[ordered]@{}
 foreach($file in Get-ChildItem -LiteralPath $payload -Recurse -File|Sort-Object FullName){
  $relative=$file.FullName.Substring($payload.Length+1).Replace('\','/')
  $manifest[$relative]=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
 }
 $manifest|ConvertTo-Json -Depth 3|Set-Content -LiteralPath (Join-Path $payload 'payload-sha256.json') -Encoding UTF8
 & $ISCC "/DPayloadDir=$payload" "/DAppVersion=$Version" "/O$Output" (Join-Path $root 'desktop/windows/installer/QuicLab.iss')
 if($LASTEXITCODE -ne 0){throw 'Setup compiler failed'}
 $setup=Join-Path $Output "quic-lab-windows-$Version-x64-setup.exe"
 ((Get-FileHash -LiteralPath $setup).Hash.ToLowerInvariant()+'  '+[IO.Path]::GetFileName($setup))|Set-Content -LiteralPath (Join-Path $Output 'SHA256SUMS.txt') -Encoding ascii
 Write-Output "Built $setup"
}finally{Pop-Location}
