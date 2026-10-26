# Android pre.11 release verification — 2026-10-25

Published: `0.9.5-pre.11`, version code 76. Final checks and public download verification completed.

## Linux

An isolated copy of the current dirty worktree was transferred to the existing Linux test host after explicit user authorization. Toolchain: Go 1.26.8 linux/amd64. Source/test changes were synchronized before their checks. No test-host service configuration was changed.

- Full `go test ./... -count=1`: PASS; mobile 79.868 s.
- `go vet ./...`: PASS.
- `go test -race .` in `third_party/quic-go`: PASS.
- `python3 scripts/test-install-server.py`: 60 PASS.
- Final full race run with the established Xray checkptr qualification: PASS, exit 0; mobile 81.134 s. Final vet repeated after the diagnostic-only helper change: PASS.

Full race uses the existing project qualification:

```
go test -race \
  -gcflags=github.com/xtls/xray-core/proxy/vless/outbound=-d=checkptr=0 \
  -gcflags=github.com/xtls/xray-core/proxy/vless/inbound=-d=checkptr=0 \
  ./... -count=1 -timeout=300s
```

Unmodified race/checkptr still aborts at upstream Xray outbound.go:288. The same issue and narrow exclusion are documented in release-v0.8.0-regression.md and release-0.9.4.md. Race detection remains enabled everywhere; project packages retain checkptr. No release-build suppression or dependency patch was introduced.

## Test corrections and evidence

- Windows import test incorrectly rejected supported WS/TLS. Added positive WS/TLS coverage and retained unsupported gRPC rejection. Full windowsengine package passed on Windows and Linux.
- Diagnostic receipt test used a fixed date that had aged beyond its 14-day retention. Uses a recent relative timestamp while still checking client receipt versus newer server activity. Three Linux repetitions passed.
- AWG authenticated admission test started two instances of the same peer. It now closes the first before testing spoofed source addresses, preserving exclusive peer ownership. Three repetitions passed.
- tcpdump integration initially failed during the full suite but could not be reproduced in 1000 isolated runs. The test now checks UDP I/O, unique allowed/excluded packet markers, and completion after the excluded exchange; bounded retries accommodate lossy capture. Product filtering was unchanged. 100 focused repetitions and package race passed; subsequent full suites passed this package. Original root cause remains unproven.
- VLESS/XUDP revoke test intermittently read a valid six-byte End frame before EOF and called the connection alive. Diagnostic bytes `000400030300` confirmed this. It now permits only the correctly framed, payload-free session End followed by EOF; other data and timeouts still fail. 300 repetitions and 10 related race repetitions passed.
- Installer archive fixture omitted the mandatory mdm-standalone.md file; fixture corrected, all 60 tests passed.
- One full qualified race run timed out in the historical `TestBothPathsTemporarilyUnavailable` regression. Three focused race repetitions then passed in about 2.03 s each. No timeout was increased and no production change was inferred. The echo helper now includes its last eight events on timeout to make any recurrence diagnosable. This intermittent observation remains disclosed even if the final rerun passes.

## Android and physical HA

Fresh native AAR build; assembleDebug, assembleDebugAndroidTest and lintDebug PASS. APK manifest: package ru.vpnc.quiclab, version76/pre.11, minSdk30, arm64-v8a and x86_64. Signing certificate SHA-256 matches published pre.10: `1b815915bdc41b179ecbe5d106e06a2ca83205f573a964979dd7af4e357db68c`.

Note9 upgrade install succeeded. Final APK regression: 83 passed, one secondary-LTE lease test skipped because saved LTE reserve permission is disabled. Additional updater/profile validation: 15 passed.

Real HA test on final APK: own AWG plus fresh third-party VLESS subscription; warm LTE reserve held before switching. Two independent 90-second TLS/TCP connections, one per direction:

| Handover | HTTPS exchanges | Maximum RTT | Connections | Same session |
| --- | ---: | ---: | ---: | --- |
| AWG/Wi-Fi to VLESS/LTE | 493 | 278 ms | 1 | yes |
| VLESS/LTE to AWG/Wi-Fi | 577 | 512 ms | 1 | yes |

Live test PASS, 208.777 s. Current encrypted configuration restored byte-for-byte, SHA-256 `330eabfd0c8de01d0c3e90213571bd684ba47661d764061076c183b438032e96`. Radio restoration passed; no errors. Updated saved subscription retained. Note12 was not directly operated.

## Publication

APK SHA-256: `e8a7386ad3749eba871441db20854bea313ad0be318f9ef4a016450d242afca3`, 60,489,588 bytes.

Independent review covered relevant lifecycle code, all test corrections, and the publication script, with no critical/important findings. Publishing changes only APK and two capability metadata files; existing server binaries are retained. Main service restart reloads capabilities. Publisher checks expected old/new hashes, preserves ownership and modes, saves rollback files and restores them on ordinary errors. Atomic per file, not across files or power loss.

Public capabilities advertise code 76 / pre.11; independently downloaded APK matches the build SHA-256. Publisher verified service active and unchanged server binary. Rollback location: `/opt/quic-lab/rollback/android-pre11-20261025`.

Detailed local logs and private device backup remain ignored under artifacts/release-pre11/; never include that directory in source publication.
