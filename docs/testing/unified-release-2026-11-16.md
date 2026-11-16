# Unified release verification — 0.9.5-pre.16

Runtime build input: `c61108992702da9c2205f314b2ec423db6328e3f`.
The release tag adds only this verification record and checklist updates.
All five binaries were rebuilt; no private profiles, credentials or build logs
are included in release assets. GitHub source archives resolve to the release tag.

## Android

Code81 / 0.9.5-pre.16; API30 minimum, target/compile API35; arm64-v8a and x86_64.
Fresh Go1.26.8 AAR and APK. Build, lint and 15 JVM tests PASS. Lint: zero errors,
111 warnings, one information item. Native AAR/APK bytes match; both ELF LOAD
alignment and APK zipalign validate at 16KB. Embedded MDM HTML/JS matches source.
APK signature v2 valid; existing preview certificate SHA256:
`1b815915bdc41b179ecbe5d106e06a2ca83205f573a964979dd7af4e357db68c`.

Handover behavior was tested on Note12 Pro using the preceding code80 diagnostic
build of the same fix: two Wi-Fi/LTE round trips and an eight-second loss of both
networks, preserving Telegram process, VPN network, QUIC session and existing TCP
flows. See [diagnosis](tcp-stall-diagnosis-2026-11-16.md). Code81 was not installed
on a phone during release packaging; multi-channel behavior has policy tests,
not live field coverage. Long outages past QUIC timeout still lose TCP flows.

## Linux

Fresh tracked-source archive on the authorized Linux build host; Go1.26.8.
`go test -tags ci ./...`, `go vet -tags ci ./...` and all 60 server installer
tests PASS. Server amd64/arm64 archives contain all four ELF binaries, correct
architectures, executable modes and licenses. amd64 example `-check-config`
passes; arm64 was cross-compiled, not executed.

Linux amd64 Go/Fyne GUI archive: 54 dependency licenses; `--help` and dynamic
library resolution PASS. Requires systemd, glibc >= 2.38 and OpenGL/X11/XWayland.
No Python runtime. No new laptop installation in this release verification.

## Windows

Go1.26.8, .NET SDK10.0.402/runtime10.0.13, Inno6.7.3, Wintun0.14.1.
All 466 payload manifest hashes match. Self-contained x64 product version
0.9.5-pre.16 with runtime source revision. Wintun signature valid; application
and installer unsigned. Contracts, application selection, fake-client WPF UI,
and installer ACL/ownership/integrity/path-traversal checks PASS.
No live Windows VPN or fresh install/upgrade test performed.

## Publication validation

Seven planned assets: five binaries, release-manifest.json and SHA256SUMS.txt.
Production publication changes only the APK and three capabilities metadata
fields in server/admin configs. Runtime binaries and routing are unchanged.
The main service must restart to load capabilities; other workers stay running.

The publication transaction was independently reviewed. Failure-injection
checks pass for ordinary rollback, restoration write failure and concurrent
config modification: safe files are restored; incomplete recovery leaves the
main service stopped and reports the retained backup. Success requires public
metadata and downloaded APK hash equality as well as worker PID continuity.
Actual publication results are recorded after deployment.
