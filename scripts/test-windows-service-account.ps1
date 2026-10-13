param([Parameter(Mandatory=$true)][string]$TestBinary,[Parameter(Mandatory=$true)][string]$ExpectedSHA256)
$ErrorActionPreference='Stop'
$identity=[Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal($identity)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {throw 'Administrator required'}
if ((Get-Service QuicLabWindowsDev).Status -ne 'Running') {throw 'Test service must be running'}
if ((Get-FileHash -LiteralPath $TestBinary -Algorithm SHA256).Hash -ne $ExpectedSHA256) {throw 'Test binary mismatch'}
$root=Join-Path $env:ProgramFiles 'QUIC Lab Windows Dev'
$test=Join-Path $root 'windowsclient.test.exe'
Copy-Item -LiteralPath $TestBinary -Destination $test -Force
if ((Get-FileHash -LiteralPath $test -Algorithm SHA256).Hash -ne $ExpectedSHA256) {throw 'Copied test mismatch'}
$name='qltest'+[Guid]::NewGuid().ToString('N').Substring(0,8)
$password=ConvertTo-SecureString ([Guid]::NewGuid().ToString('N')+'!aA9') -AsPlainText -Force
$account=$null
$result=[ordered]@{Test='foreign-account-pipe-denial';Success=$false;Removed=$false;Time=(Get-Date).ToUniversalTime().ToString('o')}
try {
 $account=New-LocalUser -Name $name -Password $password -AccountExpires (Get-Date).AddHours(1) -Description 'Temporary QUIC Lab IPC denial test'
 $result.AccountSID=$account.SID.Value
 Add-LocalGroupMember -SID 'S-1-5-32-545' -Member $account
 $credential=New-Object Management.Automation.PSCredential(($env:COMPUTERNAME+'\'+$name),$password)
 $process=Start-Process -FilePath $test -ArgumentList @('-test.run=TestInstalledServiceRejectsForeignAccount','-service-denial','-test.v','-test.timeout=10s') -Credential $credential -WorkingDirectory $root -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $root 'account-test.log') -RedirectStandardError (Join-Path $root 'account-test-error.log')
 if (-not $process.WaitForExit(20000)) {Stop-Process -Id $process.Id;throw 'Test timeout'}
 $process.Refresh()
 $result.ExitCode=$process.ExitCode
 $output=Get-Content -Raw -LiteralPath (Join-Path $root 'account-test.log')
 if ($output -notmatch '--- PASS: TestInstalledServiceRejectsForeignAccount' -or $output -match 'FAIL') {throw 'Foreign account test failed'}
 $result.Success=$true
} catch { $result.Error=$_.Exception.GetType().Name;throw } finally {
 if ($account) {Remove-LocalUser -SID $account.SID;$result.Removed=$true}
 $password.Dispose()
 $result | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $root 'account-test-result.json') -Encoding UTF8
}
