# 0.8.0 regression report — Phase 1″ checkpoint

Date: 2026-10-04. Application source/build baseline: 4ed596e; release tag additionally contains documentation-only acceptance updates. Android versionName 0.8.0, versionCode 33. APK rebuilt with fresh native AAR (ARM64/x86_64, API 30 minimum). Existing sideload signing identity retained.

## Results

| Check | Result |
| --- | --- |
| Linux Go 1.26.8: go test ./... -count=1 -timeout=240s | PASS |
| Linux: go vet ./... | PASS |
| Installer regression | 58 tests PASS |
| AWG and transit network rule tests | PASS |
| Playwright admin connection grouping/live updates/expansion/offline | PASS |
| Android native build, assembleDebug, lintDebug, instrumentation build | PASS |
| Redmi Note 9 Pro Android 11: standard instrumentation | 104 passed, 21 opt-in skipped; no failures |
| Final APK live notification/stop and 60-second screen-off TCP/UDP | 2 passed |
| Linux amd64 server archive startup/config/install | PASS on production |
| Linux arm64 server archive | Cross-build only; no ARM64 runtime claim |

Android standard command excludes BondDeviceTest and PhaseOneSoakTest, which require separate explicit parameters. Host/server arguments were quic-demo.vpnc.ru and quic-demo.vpnc.ru:443. Initial invocation omitted these parameters and failed three network tests; the capture test also timed out using the preceding endpoint. Re-running with the correct unified endpoint passed all 104 executed tests, including Dashboard, HTTPS return, server capture and Wi-Fi migration. This run does not close the complete B4/E2/E5 physical matrix.

## Race/checkptr qualification

Unmodified go test -race ./... failed with a checkptr fatal error in upstream xray-core v1.260327.0, VLESS outbound.go:288 and inbound.go:585. Both paths retain an unsafe pointer as uintptr and then convert field-offset arithmetic back to a pointer.

The full suite passed (exit 0, race detector enabled) with checkptr disabled only for those two dependency packages:

    go test -race       -gcflags=github.com/xtls/xray-core/proxy/vless/outbound=-d=checkptr=0       -gcflags=github.com/xtls/xray-core/proxy/vless/inbound=-d=checkptr=0       ./... -timeout=300s

No product source or release build flags were changed to suppress this. This is a qualified race result, not a clean unmodified race/checkptr pass. Upstream unsafe-pointer compatibility remains a tracked validation limitation.

## Deployment verification

quic-demo.vpnc.ru: server, VLESS, AWG and transit workers updated using the packaged installers. All four units active; installed executables byte-match the amd64 archive. Existing configuration retained, including public_tls_min=1.0. Public landing page returns successfully. Downloaded public APK SHA256 matches the release APK:
15c436d67f722d546413f1d19eb05d94a781ddc741905cadb3a65b489498f84f

APK 0.8.0 installed successfully on Redmi Note 9 Pro and Xiaomi 22101316UG. Post-install connection checks on both phones are recorded separately when their screens are unlocked.

## Boundaries

The earlier eight-hour pre.10 run passed (2780 TCP/UDP probes, 2840 active VPN samples, USB powered, brief screen wakes allowed). It is evidence for pre.10, not an eight-hour run of the final 0.8.0 APK.

Full Phase 1 remains open: complete B4/E2/E5 failover/fault/load matrix, 15 active / at least 30 admitted devices and 100 Mbit/s target, full expanded VLESS TLS/REALITY/HAPP matrix, MIUI process-crash recovery. Known all-apps Wi-Fi return limitation and phase-two split-routing hardening remain documented. MDM is deferred until all VPN server stages are complete.

Raw personal-device logs, signing keys, server credentials and production configuration are excluded from GitHub assets. See release-v0.8.0.md, phase1-acceptance.md and the development roadmap.