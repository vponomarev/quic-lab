# Desktop transport parity Implementation Plan

> Execute inline with superpowers:executing-plans, TDD, one final fresh review.

**Goal:** Advance Linux protocol parity and Windows transport correctness, preserving Android regression.
**Architecture:** Existing protocol cores; shared AWG coverage and desktop gateway entry; OS-specific routes unchanged.
**Tech Stack:** Go, existing Xray/AWG, WPF/Kotlin regression.
**Spec:** ../specs/2026-11-04-desktop-transport-parity.md

## Global Constraints
- Base 390ce1b, existing isolated worktree; preserve unrelated dirty files. Go tests on Linux only (Windows native test binaries cross-built there).
- Numeric IPv4 Linux endpoint, 1..256 subnets; existing no /0, no reserved/endpoint capture, no policy-routing rules.
- Source-only authorized Linux host, synthetic credentials; no production/user VPN/MDM or host network changes.

## Review Focus
- Stored WS fields or invalid TCP/WS combinations cannot silently downgrade to TCP.
- AWG AllowedIPs union must cover every selected IP, with no one-address gaps or IPv6 confusion.
- Linux secret-bearing config projection/error output must omit unrelated credentials; foreign network resources preserved.
- Cancellation/failed startup cleanup and readiness vs verified reachability remain honest.
- Windows regressions, legacy Linux QUIC/HTTPS and Android stored profiles retain compatibility.

## Task 1: Common transport correctness, Windows integration
Files: mobile/gateway_vless.go and tests; internal/awg/coverage.go/tests; internal/windowsengine/transports.go/tests.
Interface: awg.CoversIPv4(allowed []string, target string) bool, no mutation or I/O.
- [x] RED WS decode preservation/invalid stored combination and Windows split AllowedIPs tests on Linux.
- [x] Implement bounded sorted interval coverage and WS-preserving validated decode. GREEN targeted tests/race.

## Task 2: Linux standalone transports
Files: internal/linuxclient/config.go,new transports.go/tests,backend_linux.go; mobile/gateway_linux_client.go,gateway_tun_windows.go,new gateway_desktop.go/tests; cmd/linux-vpn-client/main_linux.go.
Interface: Config AWGConfig,VLESSURI, GatewayJSON() (string,error); shared desktop start behind OS entries.
- [x] RED Linux strict transport config/coverage/trust/projection tests and desktop startup cancellation/roots tests.
- [x] Implement validation/projection, preserve existing Linux route lifecycle. GREEN targeted tests and builds.
- [x] Extend isolated namespace fixtures/scripts; actual TCP/UDP over four protocols (VLESS TCP/WS), Stop cleanup, existing QUIC/HTTPS loss cleanup.

## Task 3: Integrated verification and roadmap
Files: docs/client-roadmap.md,docs/linux-client-preview.md,docs/testing/desktop-transport-parity-2026-11-04.md; build artifacts ignored.
- [x] Full Linux tests/race/vet, Windows cross-build/native nonmutating regression; Android AAR/build/lint/device if available.
- [x] One fresh review, important fixes RED/GREEN and final suite, evidence and consolidated roadmap.
- [x] Scope commit, retain branch, cleanup this plan's scratch only.

Ruling: user's existing autonomy request replaces approval handoffs; PowerShell logs/ledger replace shell helpers. Local commits only.

Evidence: [verification and explicit limitations](../../testing/desktop-transport-parity-2026-11-04.md). Full Go PASS; qualified race/vet PASS with upstream REALITY checkptr exception recorded; Windows 43 native tests PASS/3 opt-in SKIP; Android 167/167 PASS; all five Linux namespace modes TCP+UDP/cleanup PASS across the recorded runs.
