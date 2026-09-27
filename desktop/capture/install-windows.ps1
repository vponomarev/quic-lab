param([Parameter(Mandatory=$true)][string]$Server)
$ErrorActionPreference = 'Stop'
$destination = Join-Path $env:LOCALAPPDATA 'QUIC Lab Capture'
New-Item -ItemType Directory -Force -Path $destination | Out-Null
$binary = Join-Path $destination 'quic-lab-capture.exe'
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'quic-lab-capture.exe') -Destination $binary -Force
if ($Server -notmatch '^https://[^\s"<>]+/$' -or $Server.Contains('[') -or $Server.Contains(']') -or $Server.Contains('\')) { throw 'Use a plain HTTPS URL ending in /lab/, without Markdown or backslashes.' }
$registration = Start-Process -FilePath $binary -ArgumentList @('-trust-server', ('"' + $Server + '"')) -Wait -PassThru -WindowStyle Hidden
if ($registration.ExitCode -ne 0) { throw ('Server registration failed (exit ' + $registration.ExitCode + '); handler not registered.') }
$key = 'HKCU:\Software\Classes\quic-lab'
if (Test-Path $key) {
 $existing = (Get-Item "$key\shell\open\command" -ErrorAction SilentlyContinue).GetValue('')
 if ($existing -and -not $existing.Contains($binary)) { throw 'quic-lab scheme already belongs to another application.' }
}
New-Item -Path "$key\shell\open\command" -Force | Out-Null
Set-Item -Path $key -Value 'URL:QUIC Lab Capture'
New-ItemProperty -Path $key -Name 'URL Protocol' -Value '' -PropertyType String -Force | Out-Null
Set-Item -Path "$key\shell\open\command" -Value ('"' + $binary + '" -open "%1"')
Write-Host 'Installed. Open the Wireshark page in your lab admin. Wireshark must be installed separately.'
