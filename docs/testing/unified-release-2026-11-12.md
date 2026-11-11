# Unified release verification — 2026-11-12

Release: `v0.9.5-pre.15`. Runtime build input:
`f4ff5ff99fe016862e96818ba0d6a97ecf93e0f7` (Android79/pre15, Windows version wiring).
The release tag additionally includes packaging recipes, license collection,
two Windows test-project reference repairs and documentation. These later
changes do not alter application/runtime sources.

## Source completeness

Audited tracked application sources, all 23 Go embed inputs, Android resources,
generated MDM source inputs, Windows installer/application/service/engine,
Linux GUI/controller/engine and the four server binaries. Compared the active
checkout with the older primary and phase1 worktrees, including 5,328 archived
source-path occurrences. No missing runtime source was found. Older unmatched
benchmark/overnight-report files are not runtime build inputs. Generated AAR,
APK, SDKs, private profiles and build caches are intentionally not tracked.

Android audit: 444 tracked files; native dependency audit checks all 279 local
compile/embed inputs in the 522-package graph. Embedded MDM JS/HTML matches
tracked source. Build outputs are reconstructed from source rather than copied
from previous published APKs/installers.

## Linux

Build host: isolated tracked-source archive on the authorized Linux test host.
Go 1.26.8. `go test -tags ci ./...` and `go vet -tags ci ./...` both exit 0.
`python3 scripts/test-install-server.py`: 60 tests, PASS.

`scripts/build-server-release.py --version 0.9.5-pre.15` builds amd64 and arm64,
CGO disabled. Both archives contain all four expected ELF executables with
correct architecture and executable mode. amd64 server `-check-config` accepts
the example configuration with a placeholder `CREDENTIALS_DIRECTORY` (syntax
only, no certificates read or listeners opened). arm64 was not executed.

`scripts/build-linux-client-release.py --version 0.9.5-pre.15 --revision
f4ff5ff99fe016862e96818ba0d6a97ecf93e0f7` builds native amd64 GUI with CGO.
Archive includes first-install/update instructions, systemd unit, example
profile, notices and licenses for 54 dependency modules, including Fyne fonts.
Replacement-module metadata records the effective compiled version.
`--help` and dynamic-library resolution pass on the build host. Binary SHA256:
`3f5799c52a12d2fc5ca009faf780ecf21f203cadbee23c7a61f55d4802fcf3d6`, identical
to the binary tested on Ubuntu 24.04.5 in the [prior GUI checks](linux-go-gui-2026-11-11.md).
No new laptop installation was performed during release preparation.

## Windows

Self-contained win-x64 build: Go 1.26.8, .NET SDK 10.0.402 / runtime 10.0.13,
Inno Setup 6.7.3, Wintun 0.14.1. Product version `0.9.5-pre.15`, file version
`0.9.5.0`. Verified all 466 payload SHA256 entries and three AMD64 executables.
Wintun Authenticode is valid (WireGuard LLC). Installer/application unsigned.

Contract/traffic, application-selection, full fake-client WPF acceptance and
installer ownership/ACL/integrity/path-traversal tests PASS. Two test projects
needed missing explicit Compile references for existing application sources.
No application behavior changes were required. Real install/upgrade/uninstall,
UAC interaction and live Windows VPN are outside this release verification.
Licenses include 32 actual Go dependencies, Go, Wintun and actual bundled .NET
runtime packages; collection recipe is tracked in `scripts/collect-windows-licenses.ps1`.

## Android

Version `0.9.5-pre.15` / code79, min API30, target/compile API35, arm64-v8a/x86_64.
JDK17, NDK27.2.12479018, Gradle8.11.1, AGP8.9.2, Kotlin2.1.20.
Fresh Go AAR and APK; both native libraries contain Go1.26.8 metadata and match
between AAR and APK. ELF LOAD and APK alignment validate at 16KB.

Build with explicit PowerShell environment before `scripts/build-android.ps1 -BuildApk`:

```powershell
$env:GOTOOLCHAIN = 'go1.26.8'
./scripts/build-android.ps1 -BuildApk
```

This matters because gomobile's generated module omits the root toolchain
directive; the discarded initial build selected installed Go1.26.0.
Final build and lint PASS: zero errors, 111 warnings, one informational item.
JVM tests: 2 passed, zero failures. Instrumentation test APK compiled but
not installed/executed. Signature v2 verified, certificate continuity with pre.13:
SHA256 `1b815915bdc41b179ecbe5d106e06a2ca83205f573a964979dd7af4e357db68c`.

## Publication

Release assets: APK, Linux desktop archive, Windows setup, two Linux server
archives, `SHA256SUMS.txt`, `release-manifest.json`; GitHub source archives use
the release tag. Only these reviewed files are uploaded; validation logs and
local tool paths remain local. Publication uses a new prerelease tag and
fast-forward source updates; existing releases are preserved.
