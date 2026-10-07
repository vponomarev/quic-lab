# Release reliability: tasks 1 and 2

Scope: road QUIC stall with live RTT; Android process recovery and LTE accounting. Tasks 3–7 remain outside this iteration.
Execution: existing codex/phase1-vpn worktree, baseline 78e1b9a; Go tests on Linux, Android build/ADB on Windows.

## 1. Road QUIC stall — open

- [ ] Establish the mechanism matching the reported live RTT.
- [ ] Confirm a fix with a reproducer.
- [x] Inspect retained trip evidence and test a specific MTU hypothesis.

Incident: 2026-10-06 15:00–23:30 Moscow, QUIC. Later AWG use does not explain it.
Uploaded diagnostics start October 7. The original private client archive only covers October 7 00:00–00:36 Moscow, outside the reported interval. Keep raw evidence outside git.
Server TCP-flow logs during the reported interval peak at 23 concurrent observed flows per session, below 128; this does not rule out client-side or UDP exhaustion.

The real QUIC/TCP echo experiment in mobile/gateway_mtu_recovery_test.go reduces same-path UDP MTU to 1280 after warmup. Bulk transfer times out (observed 149 dropped datagrams), but heartbeat also fails. This is an unresolved candidate, not a reproduction of Telegram's live-RTT symptom.
It is explicitly opt-in (`QUIC_MTU_EXPERIMENT=1 go test ./mobile -run TestQUICDataAfterSamePathMTUReduction -count=1 -v`) and expected to expose the unresolved failure. Normal suite skips it; do not count that skip as a fix.
No speculative QUIC transport change shipped. Further diagnosis needs a matching controlled reproduction or diagnostics from another occurrence. Item 1 remains a release gate.

## 2. Android recovery and LTE budget — implemented, bounded validation

- [x] Atomic durable checkpoint preserving epoch, usage, limit and exhaustion.
- [x] Restore last recorded usage, accepting accounting error (owner decision); do not exhaust remaining allowance after crash.
- [x] Explicit Stop/revoke resets period. Service teardown preserves usage. Failed startup preserves usage but disarms automatic resume.
- [x] Same-boot activity reopening resumes interrupted VPN with existing Android VPN consent; no boot autostart or force-stop bypass.
- [x] Explicit confirmed reset of a damaged checkpoint in common settings; no silent zeroing.
- [x] Configured limit change preserves usage/epoch; unchanged limit preserves exhausted latch.
- [x] Build/lint, Go race suite, Android regressions and focused independent review.
- [x] Genuine SIGKILL + reopening restores VPN and nonzero checkpoint on dedicated Redmi.

Checkpoint target interval: 2 seconds. Android scheduling can delay persistence; no strict byte-loss bound. Dead in-flight reservations and one-shot download grants are not restored.

### Evidence, October 7

- Linux: `go test -race ./mobile ./internal/trafficbudget -count=1` PASS (52.695s / 1.009s).
- Android: assembleDebug, assembleDebugAndroidTest, lintDebug PASS.
- VpnBudgetPersistenceTest + VpnBudgetRunTest + VpnServiceRestartTest: seven tests PASS (6.509 seconds).
- Real lifecycle: Redmi Note 9 Pro, debugger invokes Process.killProcess at a main-thread breakpoint (not force-stop). PID 3622 terminated with SIG9 at 23:47:07 Moscow; logcat confirms Zygote exit.
- Reopening via ordinary launcher, without tapping Connect, creates PID 3941, foreground VPN, START_STICKY. Checkpoint retains epoch lifecycle-regression-20261007 and 1,048,576 bytes. This usage was deliberately seeded for the test, not measured LTE traffic.
- Separate-UID probe confirms HTTPS and UDP DNS with vpn=true, ok=true after recovery. UI confirms AWG/Wi-Fi and exit IPv4.
- Earlier background-only SIGKILL test on this MIUI did not restart the process despite START_STICKY. Therefore universal unattended recovery is NOT certified; reopening recovery is verified.
- Review identified explicit damaged-file reset and failed-start resume issues; fixed. Final review found no important remaining issues; duplicate checkpoint writes removed.

Development APK installed only on dedicated Redmi. Primary phone and public release unchanged. No phase-1 closure or release publication claimed.
