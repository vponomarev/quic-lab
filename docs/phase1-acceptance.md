# Phase 1 acceptance evidence

Phase 1 is not accepted until every gate in the closure plan has evidence. Build success and short smoke tests do not substitute for physical failover, load, or an eight-hour run.

## Screen-off Android soak

Use the dedicated Redmi Note 9 Pro (`dc69eb2c`). The user authorized long tests and network switching. USB charging must be recorded; plugged-in battery observations do not prove unplugged endurance or Doze behavior.

Build the optional separate-UID probe and instrumentation:

```text
android/gradlew -p android -PwithProbe :app:assembleDebugAndroidTest :probe:assembleSelectedDebug
adb -s dc69eb2c install -r android/app/build/outputs/apk/androidTest/debug/app-debug-androidTest.apk
adb -s dc69eb2c install -r android/probe/build/outputs/apk/selected/debug/probe-selected-debug.apk
adb -s dc69eb2c shell am instrument -w -e class ru.vpnc.quiclab.PhaseOneSoakTest -e soak_seconds 28800 -e probe_host quic-demo.vpnc.ru ru.vpnc.quiclab.test/androidx.test.runner.AndroidJUnitRunner
```

Run with `soak_seconds 60` first to validate the harness. Unlock once before starting; the test turns the screen off itself. It temporarily routes the probe through the selected VPN profile and restores routing preferences afterward. It stops the VPN afterward. Profiles and keys are not exported or replaced.

The probe performs HTTPS/TCP and DNS/UDP every ten seconds from a separate Android UID, checking that Android reports VPN routing. The VPN application's own sockets are excluded from the VPN and cannot be used as evidence of application traffic traversal. Probe failures fail the test. The probe foreground service is test-only and is not part of the shipped client.

Files survive test interruption:

- VPN app `files/soak<TIMESTAMP>.jsonl`: monotonic/wall time, service activity, health age, screen state, PSS, Java/native heap, descriptor count, traffic counters, battery/charging state, probe sequence.
- Probe app `files/soak<TIMESTAMP>.jsonl`: every TCP/UDP outcome and elapsed time.
- Host instrumentation output: actual start, duration, final JUnit result.

Retrieve using `adb exec-out run-as <package> cat files/<file>`. Do not collect identity files or preferences containing credentials. Record APK SHA256, source revision, uncommitted diff hash, Android/API and server revision alongside evidence. A finished instrumentation run alone is insufficient: inspect memory/descriptor time series for sustained growth. No arbitrary battery threshold is imposed.

This soak uses repeated connections. Persistent single-socket byte continuity, network handoff latency, packet-size blackholes, 1/2 MiB freezes, capacity and throughput require their separate acceptance gates.

## Current status

- Server settings: full Go suite and vet pass on Linux; focused admin/worker/server race pass; real HTTPS/CSP editor browser checks pass.
- Android soak harness: preparation / short validation; eight-hour acceptance not yet complete.
- Physical failover, load and remaining fault matrix: pending; see the closure plan.

## Active run — 2026-10-04

- Short validation: `PhaseOneSoakTest`, 60 seconds, JUnit `OK (1 test)`, wall duration 67.339 s. TCP/UDP through VPN and screen off confirmed.
- Eight-hour run started at **2026-10-04 13:25:20 Europe/Moscow** (first sample UTC millis 1791109520174); scheduled completion approximately 21:25:20. **Result pending.**
- Run ID: `soak1791109515385`; device Redmi Note 9 Pro / Android 11 / API 30 / `dc69eb2c`; USB power connected.
- Client APK SHA256: `529b7c97138f483b2158eada66fb2a0307fbfdca6fedef086d2457548e34808f`.
- Instrumentation APK SHA256: `b599b902d8cdc74ff889b45a32d527717d18d162e8c90a40c1f17a69efdf9c5d`.
- Source baseline: `55b8254` plus the soak harness recorded in the following commit. Production server unchanged during this run.
- Detached Windows runner PID at launch: 21496. Host evidence: `%TEMP%/quic-phase1-soak/run-20261004-132515`. Runner saves instrumentation output and retrieves application/probe JSONL after completion. Logs also remain on the device if host collection is interrupted.
- First two samples: VPN active, screen off, TCP/UDP successful, 110 descriptors. These samples establish startup only and are not an endurance verdict.
