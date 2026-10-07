# 0.9.0 regression and publication

Date: 2026-10-08 Europe/Moscow. Phase one closes under the owner's explicitly revised scope, not by claiming all original acceptance gates passed.

## Verified

- Fresh native AAR from release source; Android 0.9.0 / versionCode 40, existing signer SHA256 1b815915bdc41b179ecbe5d106e06a2ca83205f573a964979dd7af4e357db68c.
- Linux Go full suite and vet PASS.
- Full race suite PASS with the previously documented checkptr exception only for upstream Xray VLESS inbound/outbound packages:
  `go test -race -gcflags=github.com/xtls/xray-core/proxy/vless/outbound=-d=checkptr=0 -gcflags=github.com/xtls/xray-core/proxy/vless/inbound=-d=checkptr=0 ./... -timeout=300s`.
  This is not an unqualified checkptr pass.
- Installer: 58 tests PASS. Packaging fixture was stale after adding client-diagnostics.md; corrected and rerun. No installer product change.
- AWG/transit firewall tests PASS in separate network namespaces. Initial host-level invocation stopped at an existing test-chain name; repeated in isolation without removing host chains.
- Default admission corrected from 15 to 30 to match the owner's requirement. New regression observed failure at device 16, then PASS; updated old default assertion; final admin + cmd/server race PASS. Explicit configured values and maximum 512 preserved.
- Android assembleDebug/assembleDebugAndroidTest/lintDebug PASS.
- Dedicated Redmi Note 9 Pro/API30: standard instrumentation reported `OK (144 tests)`, 66.016s. Opt-in tests without arguments are not credited as live coverage; BondDeviceTest and PhaseOneSoakTest excluded from this standard run.
- Initial Android suite was interrupted after editor tests encountered restored VPN activity. The saved interrupted-run intent intentionally resumed VPN on launcher entry. A standalone lifecycle test performed explicit Stop; clean-process repeat passed. This is recorded as fixture preparation, not suppression of a product defect.
- Separate actual PhaseOneSoakTest with soak_seconds=60: `OK (1 test)`, 67.295s; separate-UID TCP/UDP, screen off. This does not certify eight-hour endurance.
- Focused independent review: no critical/important findings in runtime/release changes; admission correction reviewed separately.

## Deployment

quic-demo.vpnc.ru updated from the amd64 archive using packaged installers. quic-lab, AWG, VLESS and transit units active; all four installed executables byte-match archive binaries. Existing public_tls_min=1.0 preserved. Default effective admission is now 30.

Public capabilities report 0.9.0 / 40. Public APK downloaded independently and SHA256 matches:
`5acd5b496d15acffb35fe038a5a057b08cd46c3d055171001c5d650da3f9c9ef`.
Landing page HTTP 200. Config/APK/binary backups retained privately on server. Identities were not reset or replaced.

APK installed on dedicated Redmi. Primary phone not connected and not modified. Post-deployment interactive phone check waits for unlock; pre-deployment APK/network smoke is complete.

Linux arm64 archives are cross-built only. No arm64 runtime claim. Go compilation uses module-selected toolchain; base system Go version is not substituted for module requirements.

## Deferred / accepted limitations

Road QUIC live-RTT stall and same-path MTU experiment unresolved; full B4/E2/E5 physical, VLESS/Echo, fault/load and fresh eight-hour final-build acceptance moved to phase two. MIUI unattended process restart and all-app Wi-Fi-return limitations remain explicit. Prior pre.10 eight-hour success is historical evidence only.

MDM core now follows 0.9 before phase two. Telemetry/remote helper follow as MDM extensions. Research mode remains planned, not implemented.
