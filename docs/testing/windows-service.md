# Windows service checkpoint W2 — 2026-10-14

This is a no-network fixture milestone, not a working VPN beta. The UI explicitly labels its Start operation as a test. W3 replaces FixtureBackend with an actual verified network backend.

## Implemented

- Go SCM adapter, manual-start `QuicLabWindowsDev`; fixed local pipe `quic-lab-windows-dev-v1`.
- Local-only Microsoft go-winio 0.6.4 listener, first-instance reservation, DACL for SYSTEM/admins/assigned owner, client process token SID checked before commands. Assigned owner or SYSTEM may issue requests. Other admins can administer SCM but do not receive implicit application control through the SID gate.
- One length-prefixed little-endian JSON request per connection, 1 MiB cap, version/id/command validation, 16 concurrent connections and bounded deadlines. Unknown fields rejected; responses never carry raw backend errors or configuration.
- .NET client checks pipe owner (SYSTEM or Administrators), pipe server PID and current running SCM PID before sending configuration. A foreground fixture accepts a separately supplied expected PID only in explicit developer mode. It must not become a normal production fallback.
- DPAPI under the service identity; encrypted config atomically replaced with write-through after file flush. State directory ACL is protected. Closing the UI does not stop a running backend.
- WPF import/validate/apply/test-start/stop/status window. Explicit async command progress; bounded file input. Profile import at this checkpoint uses the shared model JSON, not the final Android profile/QR importer planned in W5.

## Evidence

Windows 10 Pro 19045 x64, .NET SDK 10.0.402 installed in ignored task workspace; SHA512 verified against official Microsoft release metadata. No machine-wide SDK/PATH change. Linux go1.26.8 builds native executables.

Native Windows tests: DPAPI roundtrip/corruption, encrypted store restart/corruption, pipe client authorization, wrong server PID, duplicate listener, SYSTEM-only ACL denying ordinary user. Linux race tests: strict/truncated/oversize requests, command lifecycle, persistence failure, no secret leakage. Shared protocol-v1.json is consumed by Go and .NET.

End-to-end .NET -> foreground Go fixture passed config apply/start/status across a second client/stop. LocalSystem SCM service installation and running state verified; the same cycle from non-elevated vitaly passed, including SCM identity checks. Initial direct OpenProcess approach failed on this workstation; replaced with documented pipe ownership plus SCM PID verification, without expanding process rights.

WPF builds without warnings. Accessibility tree confirms labels and controls. Pixel capture/input did not complete (capture frame timeout / unavailable input geometry); visual layout and interactive click acceptance are not claimed.

## Open acceptance

SCM stop/update/start passed with matching binary hashes; persisted config loaded after restart. A WPF STA harness exercised real XAML button event bindings, validate/apply/start, close/reopen without stopping the backend, then Stop. Hardware mouse input/pixel layout acceptance remains unverified. SCM uninstall passed; the normal client fails closed after removal. A separately created ordinary Windows account received ERROR_ACCESS_DENIED from the installed service pipe; the test account was removed by its exact SID and absence was verified. W3 real-network rollback remains pending. The manual-start service is a development fixture only. No Wintun driver, routes, DNS, production binaries or phones changed.

Server account `windows.beta.test` (QUIC/HTTPS/AWG/VLESS) exists for future W3/W4 tests. Its private profile remains at `/var/lib/quic-lab/windows-client-test/profile.json` on the server, mode 0600; credentials are never part of the repository.

## Scripts and dependencies

`scripts/test-windows-service.ps1` requires elevation, verifies source/copy hash, refuses service/directory replacement, installs only the fixed test service. Remove verifies exact executable path before stopping/deleting service; retains files for audit. Use the normal UAC prompt; do not automate it.

Build UI/tests with the pinned SDK under desktop/windows/global.json. Go build remains on Linux; run the cross-compiled test exe on Windows. Do not interpret fake-backend success as VPN readiness.

References: [named pipe security](https://learn.microsoft.com/en-us/windows/win32/ipc/named-pipe-security-and-access-rights), [GetSecurityInfo](https://learn.microsoft.com/windows/win32/api/aclapi/nf-aclapi-getsecurityinfo), [SDK installation](https://learn.microsoft.com/en-us/dotnet/core/tools/dotnet-install-script).

## Review fixes and deferred minor findings

The review reproduced hostile state-directory precreation on actual NTFS: a SYSTEM-intended store accepted a directory owned by the ordinary user. Regression failed before the fix and passes now. Directory creation supplies restrictive security attributes atomically; preexisting foreign owners and reparse ancestors are rejected before DACL repair. Installer rejects a preexisting state directory. The fixed binary was installed and restarted; non-admin .NET/WPF tests passed again.

Deferred minor findings (before distribution): UI file import has a check/reopen race around its size bound; DPAPI native plaintext output is freed without explicit zeroing. The service still enforces frame/config bounds. This checkpoint is not a distributable client.

Automatic approval review initially required explicit local-account authorization. The user subsequently authorized it; scripts/test-windows-service-account.ps1 completed the separate-account denial test and removed the account. No temporary qltest accounts, development SCM service or fixture processes remain. Protected test binaries and encrypted fixture state are retained for audit. Final native tests, Linux race tests, Windows vet, WPF build (zero warnings/errors), and removed-service rejection passed.
