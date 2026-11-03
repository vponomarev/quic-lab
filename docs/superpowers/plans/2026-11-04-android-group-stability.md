# Android ordinary group stability Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans. Preserve unrelated working changes. Steps track this bounded iteration, not the complete Android roadmap.

**Goal:** React to sustained degradation only after verifying a better ordinary-group candidate, without changing the working route during diagnostics.

**Architecture:** Keep VlessGroupPolicy/MixedGroupSession as the only route owners. Add a bounded observer and deterministic decision policy. Measure the same authenticated HTTPS transaction through each transport; never compare AWG ICMP with VLESS HTTPS timings. HA remains managed by its existing runtime.

**Tech Stack:** Existing Go/gomobile, Kotlin, Android 11 Note9, JDK17. No new dependencies.

**Spec:** ../specs/2026-10-23-adaptive-paths-design.md. User authorized autonomous implementation on 2026-11-04 and supplied Note9.

## Global Constraints

- Work in existing phase1-vpn worktree, baseline 25b75ac; unrelated cell database/server edits remain untouched.
- Go tests run only on isolated Linux checkout. Android builds and ADB on Windows; no publication or production service changes.
- Preserve phone profiles, MDM consent, network settings and LTE budget. No automatic enablement of LTE or reserve permissions.
- One background worker per ordinary group, no probe TUN attachment. Share native budget binder and AWG peer exclusivity.
- Candidate results are invalidated by network/session change, Stop and automatic-mode disablement. Probe errors do not count as active transport failures.

## Review Focus

- Stale results after stop/network change must not switch routes.
- Probe cancellation must not disconnect working Gateway or interfere with user Echo.
- Large subscriptions must not monopolize background checks.
- Initial slowness, brief spikes, incomparable metrics and missing results must not cause oscillation.
- Forbidden LTE/reserve probes, duplicate AWG peers and private configuration must remain protected.

## Task 1: Cancellable comparable measurement

Files: mobile/gateway_path_probe.go, mobile/gateway_path_probe_test.go.
Interface: Gateway.NewPathProbe() returns PathProbe; Measure() returns verified HTTPS duration in milliseconds or error; Stop() cancels only this probe. Five-second timeout; one-shot handle. Uses existing fetchExitIP and tunnel dialer, no direct fallback or new public result fields.
- [x] Write tests for cancellation, timeout, disconnected gateway, single-use and retained working multiplexed tunnel.
- [x] Observe RED on Linux, implement, run targeted mobile tests/race/vet.
- [x] Rebuild Android native library.

## Task 2: Policy and bounded runtime

Files: android/app/src/main/java/ru/vpnc/quiclab/NormalGroupHealth.kt, NormalGroupProber.kt, VpnSession.kt; matching androidTest classes.
Interface: policy accepts timestamped active/candidate HTTPS durations and returns an optional candidate index. Prober executes one job and rejects late completions by generation.
- [x] RED deterministic policy tests: minimum three degraded samples over 30s, 60s minimum dwell; active duration >=600ms; alternative <=60% of active and >=200ms faster, with two successful observations and latest <=90s; recent failure clears qualification; network reset clears evidence.
- [x] RED fairness/lifecycle tests: rotate logical connections before further nodes, one worker, Stop/cancel, late acquisition disposal, no routing changes from probes.
- [x] Implement and connect to existing route owner. Probe rounds every 30s, only current physical network with reserve permission; active then one inactive candidate. Active metrics alone never switch. Faster alternatives alone never switch a healthy path.
- [x] Build/lint and run new plus existing group/policy instrumentation on Note9. Keep published APK for rollback; restore app via launcher after instrumentation.

## Task 3: Review and delivery

- [x] Fresh review of this iteration; fix confirmed important findings with regression tests.
- [x] Record exact verification, phone restoration, decisions and remaining physical acceptance in docs/testing/android-group-stability-2026-11-04.md.
- [x] Commit only this iteration's files; leave production publishing and unrelated changes alone.

Ruling: user explicitly requested autonomous execution; use existing approved architecture and inline implementation, without another design approval round.
Ruling: this iteration uses comparable end-to-end HTTPS latency and conservative thresholds, not raw protocol heartbeat RTT. This adds bounded diagnostic traffic under existing reserve/budget permissions; physical tuning remains separate acceptance.
