# Windows portability вЂ” 2026-10-13

Base: release 0.9.4, commit 77e944d; Windows planning commit 5977117. Linux build host 192.168.5.214. No Windows runtime or VM available yet.

Initial Windows amd64 cross-build: gateway PASS, AWG PASS, VLESS PASS, mobile FAIL, gateway-client FAIL. Both failures: `mobile/research.go:55:42: undefined: FlowOwner`. The research collector is platform-neutral but its owner interface was declared in Linux-only multiple.go.

W0 ruling: move the existing interface unchanged to platform-neutral flow_owner.go. This preserves Android signatures/semantics and does not implement Windows attribution or enable research. Existing owner UID semantics remain Android-specific; Windows attribution is deferred until after beta.

Release-before-development gate: 0.9.4 published on GitHub and production; full Linux race, Android build/lint and Note9 regression completed. Private historical phone notes and the unrelated experimental LTE test are intentionally not part of the release commit.

## W0 verification вЂ” 2026-10-14

Minimal endpoint extraction preserves Android Attach(fd); common attachLink owns and closes accepted endpoints exactly once, including setup failures and rejected duplicate attachment. Worker shutdown is awaited. Four endpoint lifecycle tests run with the race detector.

| Package | Windows amd64 compile | Windows runtime |
|---|---|---|
| internal/gateway | PASS | Not run |
| internal/awg | PASS | Not run |
| internal/vless | PASS | Not run |
| mobile | PASS | Not run |
| cmd/gateway-client | PASS | Not run |

Linux race passed for vpnmodel/routing/pathpolicy/trafficbudget/gateway/awg/vless/mobile, with existing scoped Xray checkptr exclusions. One initial full-suite run timed out in pre-existing TestBothPathsTemporarilyUnavailable (no TUN attachment in that test). Three isolated repetitions passed; a repeat full suite passed (mobile 55.063s). This timing instability remains recorded, not suppressed or relabelled a Windows fix.

Fresh gomobile bind for android/arm64 and android/amd64 at API30 passed; compatibility AAR is test-only, not installed or published. Windows compile does not validate Wintun, routing, SCM or GUI. Production and phones were untouched by W0.

## W1 and L0 verification — 2026-10-14

Portable Controller, validated network transaction journal and isolated Linux backend are implemented. W1 race tests repeated ten times, vet and Windows amd64 test cross-compilation pass. Linux real TUN QUIC/HTTPS TCP/UDP, Stop cleanup and unchanged management route pass; see linux-platform-probe.md. Windows SCM/Wintun/IPC/GUI runtime remains untested pending VM access.

Review reproduced and fixed: Stop missing an unpublished startup cancellation; failed CAS rolling back a foreign matching value; journal encoder accepting more than decoder's 1 MiB limit. Regression tests failed before the fixes and pass afterward.

Recovery policy is conservative: a journal records intent before mutation and applied success afterward. A crash or persistence failure between these records leaves an ambiguous mutation; matching the desired value is insufficient proof. Recovery retains that value, reports a conflict and blocks further Apply until explicit recovery. This is a known recovery boundary, not a claim of automatic system rollback for every crash window. Windows adapters must provide provable ownership of applied resources (adapter/filter identity or OS provenance) and may not implement shared DNS rollback from value equality alone. W3 must test these guarantees with actual Windows APIs.

## Native Windows execution — 2026-10-14

Owner authorized this workstation instead of the unavailable VM. OS: Windows 10 Pro x64, 10.0.19045; about 31 GiB free RAM. No .NET SDK found on PATH or standard Program Files/user installation paths. Go and Git are installed; shared builds/race stay on Linux.

Our cross-compiled vpnclient test executable ran the entire suite ten times natively: PASS. Fake network/backend/store only; no driver, service, routes or DNS changed. This does not establish Wintun, SCM, GUI or Windows 11 acceptance. Initial PowerShell invocation failed argument parsing before tests ran; quoting dotted flags resolved it.

Active Wi-Fi, ZeroTier, VirtualBox host-only and Hyper-V adapters must be preserved. W2 can proceed on this workstation under the revised specification.
