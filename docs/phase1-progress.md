# Phase one execution progress

Execution: inline, selected by owner; no subagents unless requested. Plan: docs/superpowers/plans/2026-09-30-phase1-00-roadmap.md.

## A1 — complete

Portable Go model, gomobile validator, and Android atomic configuration projection implemented. Stable legacy IDs, DNS/routes/apps and encrypted identity references are preserved. Ordinary QUIC/HTTPS/AWG remain standalone; existing max_availability becomes demux. Profile import publishes the snapshot and rolls back a newly added profile and identity if writing fails.

Verification: Linux `go test ./internal/vpnmodel ./mobile -count=1 -timeout=120s` and `go vet ./internal/vpnmodel` passed. Android debug and test APK builds plus lint passed. Connected Android device: `VpnConfigurationTest`, OK (7 tests). Observed RED before implementation for migration and import; final GREEN includes forced write failure, rollback, idempotence, settings and identity preservation, ordinary transport compatibility, and empty drafts. Tests use isolated preferences/files and synthetic keys; no live tunnel was started.

## Rulings and transition constraints

- Disabled blank drafts are valid; activation still requires endpoint validation.
- Legacy SharedPreferences remain authoritative during A1 and are retained unchanged as the migration fallback. The versioned JSON is an atomic projection refreshed on load/import; encrypted identities stay in their existing files. Lifecycle consumers arrive in A2. Before introducing new model-only editing, replace this projection with an explicit ownership/version transition so local edits cannot be overwritten.
- Existing ordinary QUIC/HTTPS remain standalone; only max_availability maps to demux. No automatic grouping by hostname.
- Installed task-start helper requires numeric headings but plans use A1 etc.; use equivalent exact task extraction with BASE and private ledger.

Next: C1, shared LTE accounting epoch and ledger. Remaining 14 tasks are not complete. This is a development build, not a release or a verified whole-MVP implementation.

Private ledger: .superpowers/sdd/2026-09-30-phase1-01-model-lifecycle/progress.md.

## A2 — complete

Added synchronized Go lifecycle states and Android session-owning VpnExitController. Restart invalidates the prior generation and closes only that exit. Incompatible exits reject subsequent events. Single and multiple service paths use controller ownership; single-session callbacks capture their original controller so a new whole-service run cannot accept old callbacks with coincident generation numbers. Main-looper service commands remain serialized. Stop-all invalidates callbacks and attempts every close even if one fails; service cleanup still releases TUN. First explicit Stop shows a one-time notice, stored locally.

Verified: Linux go test -race ./internal/vpnmodel -count=1 and go vet ./internal/vpnmodel passed. Android assembleDebug, assembleDebugAndroidTest, lintDebug passed. Device VpnExitLifecycleTest + VpnConfigurationTest: OK (11 tests). Lifecycle cases cover independent restart, stale callbacks, incompatibility, startup failure, and close failure. These tests use controlled session resources; real multi-tunnel traffic/route blocking is the A3 live gate, not claimed here.

Test history: initial Go run failed on missing Runtime/Event APIs. Android's first three lifecycle checks were first executed after implementation (workflow deviation); the added close-failure test was observed RED (only one of two sessions closed), then fixed and observed GREEN. Do not describe all Android lifecycle tests as observed red-to-green.

## A3 — complete

Implemented explicit BlockExit, preserving rule ownership while canceling this exit's flows; duplicate SetGateway for the same session is now idempotent. Android routes validate stable exit IDs and pause through BlockExit. Existing routing policy needs no algorithm change: priority, unknown UID and unmatched-direct cases already have regression coverage. Multiple+bond remains disallowed until B4.

Observed behavioral RED: duplicate attachment canceled the existing handler; stub BlockExit did not cancel paused flows. Final Linux checks passed: go test ./internal/routing ./mobile -count=1 -timeout=120s; targeted routing/multiple tests with -race; go vet ./internal/routing ./mobile. AAR/APK/lint and instrumentation plus both probe APK builds passed.

MultipleLiveTest now accepts an optional synthetic profile_file and restores the configuration snapshot after test cleanup. Dedicated Linux server/responders were prepared on isolated ports without changing running services. Phone was initially disconnected; the device gate was completed after reconnection, as recorded below.

A3 final device gate (2026-09-30): MultipleLiveTest ran with multiple_live=true, probe_host=192.168.5.214 and an isolated synthetic profile. OK (1 test), 34.341 s; not a skipped/default invocation. Real TCP/UDP over QUIC/HTTPS, app UID and subnet priority, unmatched direct traffic, pause-without-fallback, other-exit survival, resume/reorder and single QUIC/HTTPS regression passed. The test restored profile metadata; no VPN service remained afterwards. Temporary server/responders stopped and synthetic profile removed from Android external storage. Final Linux routing/mobile suite passed again. Local evidence: artifacts/a3-live-result.log and artifacts/a3-live-report.txt (not committed; diagnostics can contain user network metadata).

Scope: two logical tunnels to one isolated Linux server over the available network. This does not certify two physical servers, Wi-Fi/LTE migration, demux session continuity or long-duration soak; those belong to later gates.

## B1 — complete with C1/C2 integration gate

Added Options with 120-second default disconnect grace, bounded pending records (1024 per session / 64 per flow), and configurable server grace (0 defaults to 120; accepted explicit range 1–3600 seconds). Reliable record ageing pauses while all paths are absent and resumes on reconnect; datagrams retain their short expiry. PathInfo carries independent path/profile/network identity and generation; stale replacement/removal callbacks cannot remove the current path. Scheduler, LTE classification and stats support named paths. Receive queues remain bounded.

Flow-ID exhaustion enters draining. New TCP/UDP flows can rotate to a new session without canceling the Gateway root/TUN or existing TCP sockets. Retirees are capped at three plus current and have a bounded drain timeout. CREATE receives a server-generated fresh secret; expired-token resume is rejected. Wire change bumps bond ALPN to quic-lab-bond/2, requiring matching client/server versions; no production deployment or phone APK installation was performed in B1.

Transitional limitation: automatic draining rotation with a finite legacy LTE budget explicitly returns an error, leaving old flows alive. This prevents budget reset/double allowance. C1/C2 must share accounting across current and retired sessions before lifting this guard; the C2 plan now includes the regression/removal gate. Therefore the complete MVP budget contract is not claimed at B1.

Verification: full Linux suite `go test ./internal/bond ./internal/gateway ./mobile ./cmd/server ./cmd/demux -count=1 -timeout=120s` passed; targeted bond/retention/mobile integration tests with race passed; vet passed. Final AAR/APK and lint build passed (artifacts/b1-final-build.log). Tests exercise 119/120-second boundaries using an adjusted disconnect timestamp, not a two-minute wall-clock soak; real pending payload delivery after a scaled outage and real TCP preservation across session rotation passed. Existing slow-reader regression still passes. No Android device test was required by B1; B4/E5 remain responsible for device/network acceptance.

## B2 — complete

Added the bounded HTTPS WebSocket bond adapter and /tunnel/bond with quic-lab-bond-v1 negotiation, 4096-byte JSON handshake and binary records up to 1080 bytes. Both transports use the same mTLS-authenticated registry. Server root context keeps the session alive independently of its initial HTTPS request. Welcome precedes path attachment to prevent binary traffic racing the handshake. Server and optional demux HTTPS listener are wired; QUIC ALPN was already versioned in B1.

Linux integration tests preserve one real TCP socket and the same UDP mapping across QUIC-to-HTTPS and HTTPS-to-QUIC replacement. A client without its certificate cannot join with a borrowed token. Framing, oversize rejection, cancellation and bounded nonblocking queue tests pass. The blocked writer is injected deterministically at the socket boundary; this is not a kernel-level stalled-link or physical Wi-Fi/LTE acceptance test.

Verification: go test ./internal/bondhttps ./internal/bondquic ./internal/gateway ./mobile ./cmd/demux ./cmd/server -count=1 -timeout=120s passed; targeted race tests and package vet passed. Android AAR/APK/lint succeeded (artifacts/b2-build.log). No APK installation or production deployment. Initial cross-transport tests failed for missing APIs, and HTTPS-first failed on the former QUIC-only guard before implementation; additional negative coverage was first run green.
## B3 — complete (policy and telemetry; execution in B4)

Added portable per-exit profile/network decisions, generation-safe dial feedback, all-auto-networks-before-reserve ordering, disabled-profile exclusion, optional rare reserve probes, retry backoff and return hysteresis. Pending user data and outstanding sized probes have independent stall timers; tiny ping success cannot reset either. Three recovered stalled connections within ten minutes recommend carousel without enabling it automatically. Idle traffic does not reset retry backoff.

Bond reports per-generation acknowledged payload, pending bytes, profile/network identity and sized-probe state. Full-record probes are bounded to one outstanding probe per path and echo a working-size payload. Mobile bond diagnostics expose stalled paths. B4 will execute policy commands and schedule probes after shared budget and authorization gates; this is not yet Android automatic profile selection. Probe health concerns the transport path, not final Internet egress.

Verification: Linux go test ./internal/pathpolicy ./internal/bond ./mobile -count=1 -timeout=120s passed; final policy/bond suite and race rerun after the backoff fix passed; vet passed. Android AAR/APK/lint passed (artifacts/b3-build.log). Tests include volume-stall observations, tiny-reply/large-record fault injection, hysteresis, reserve opt-in, independent exits, generation/dial timeout and backoff reset. Regression caught idle time resetting backoff before the fix. No Android runtime/network acceptance claimed; the connected phone was detected but APK was not installed.
## Intermediate release checkpoint — v0.8.0-pre.1

Owner requested publishing and deploying the A/B checkpoint before group C. Android version 0.8.0-pre.1 (21), paired Linux amd64/arm64 server archives and SHA256SUMS. Fresh full Go/race/vet, Android build/lint, installer unit/container checks and live phone/server validations are recorded in docs/release-v0.8.0-pre.1.md. Standard Android suite: 35 passed and 16 explicit prerequisites/opt-ins skipped; additional live multiple-exit, bond migration, authenticated/public three-transport Echo and per-transport screen policy tests passed. Three stale Android test assumptions were reproduced and corrected; application behavior was not changed to satisfy them.

quic-demo.vpnc.ru main server and public APK updated with root-only rollback backups. Initial state backup noticed a live socket/directory change; original service was restarted before retrying. Successful retry archived persistent identity/configuration data, installed the verified server, published APK atomically and checked service/public download. Public APK SHA256 matches the release. Existing AWG/transit workers remain running. No group C work has started; B4 and later acceptance remain pending.

## Post-release Wi-Fi return investigation (2026-10-01)

On the owner's Android/MIUI device, ordinary QUIC with all-app routing (mode 0) preserves the gateway session but application TCP sockets abort when Wi-Fi becomes the physical default network, before QUIC migrates. The same persistent-TCP reproduction fails on APK v0.7.4 with the current profile/server, so this is not established as a client-version regression. The owner confirmed the previously working configuration used selected applications (mode 1); the private pre-release preferences backup agrees. Selected-app routing passed the same automated Wi-Fi/LTE/Wi-Fi and short screen-off test on pre.1.

A diagnostic all-app-except-Android-UID1000 run passed twice. This narrows the interaction to capturing system-UID traffic on this device; it does not identify the exact vendor socket-abort implementation. Excluding system services changes routing semantics and is not silently enabled. No server/client transport fix or pre.2 release is claimed. Group C remains paused. Local ignored diagnostic logs contain private device metadata and must not be published.

A separate Linux fault-injection test exposes bond UDP expiration before a delayed hedge. It is tracked independently; the owner's failing profile did not enable bond.

Owner decision (2026-10-01): confirmed Brawl Stars now survives Wi-Fi return with selected applications. All-app routing on the tested Android/MIUI device is an accepted phase-one limitation. Remediation is deferred beyond phase one as [POST-P1-01](post-phase1-backlog.md), not a phase-one acceptance blocker. This deferral does not cover the separate bond UDP hedge defect.

## Morning diagnosis — 2026-10-01

Read-only inspection; phone VPN/settings unchanged. Android historical process exit reports 01:28:35 local, reason 13 OTHER KILLS BY SYSTEM, description AutoLockOffClean. This confirms system termination of the app, not a demonstrated transport failure.

Owner reports car hotspot Wi-Fi available while VPN remains on LTE until a manual restart. Exact earlier client evidence is unavailable: the 400-entry journal starts at 07:48:37 local. Server journal records QUIC open at 06:55:07 and close/reopen at 07:39:03/05 (owner estimated 07:29). These server events alone do not establish physical path selection.

Later retained evidence: at 07:59:07 The hotspot supplies DHCP vendorInfo ANDROID_METERED; client sees Wi-Fi available and validated but no standby preparation/migration until this Network disappears at 07:59:20. At 08:00:54 another Wi-Fi Network becomes available; standby succeeds, validation follows, and migration completes at 08:00:59. Reserve prefs are empty, so metered_wifi=false default applies. VpnReserveSettings.permits and VpnSession.tick exclude metered Wi-Fi from standby probing and preference migration while LTE works; initial connection does not apply that reserve filter. This is strong evidence for the earlier car behavior, but the exact earlier interval cannot be proved from overwritten logs. Same SSID alone is not used as path identity: key is Android networkHandle.

Existing user option: РЕЗЕРВНАЯ СЕТЬ VPN → Разрешить проверки лимитного Wi-Fi. Enabling it permits probing and automatic preference subject to validation/health/RTT; may consume hotspot SIM traffic. Not enabled during diagnosis. Delayed Internet startup remains unproven; retry backoff is bounded at 30s, not a permanent disable on first timeout. Diagnostic limitation: flow events evict control/lifecycle events from the 400-entry journal; investigation needs longer-lived control history. Separate from accepted all-app/UID1000 limitation.

## Metered Wi-Fi fix — v0.8.0-pre.2

Preferred Wi-Fi eligibility is now separate from optional background reserve permissions while active on cellular, including ordinary QUIC/HTTPS/AWG. Existing validation, successful QUIC/HTTPS probes, dwell/backoff and latency guards remain. User settings labels explain the distinction. Regression observed RED on pre.1 and GREEN on pre.2 with one persistent TCP socket. Build/lint passed; 14 core Android cases passed with one explicit live skip, then opt-in reserve tests passed separately for all three transports on metered Wi-Fi. Linux mobile standby/migration race tests passed, including recovery after initially lost replies. Server binary/protocol unchanged. Details: [pre.2 release](release-v0.8.0-pre.2.md). Group C has not started.

## C1 — complete (budget core and run ownership)

Added a shared concurrency-safe trafficbudget ledger: atomic reservations, partial write refund, idempotent commit, saturated uint64 counters, incoming overshoot, class totals and copied snapshots. Mobile wrapper validates epoch/nonnegative limit. LabVpnService owns one VpnBudgetRun until whole-service shutdown; restart of an exit must reuse its object. Android lifecycle test verifies retained object and new epoch after Stop/Start.

C2 must attach socket meters, configure the common limit and enforce cellular shutdown. C1 initializes an unlimited unmetered owner; it does not yet implement user-facing traffic limiting or replace legacy bond limits. Keeping the bond rotation guard is intentional until C2.

Verification: observed ledger RED on missing API; Linux go test ./internal/trafficbudget ./mobile -count=1 -timeout=120s passed, ledger race/vet passed. Android AAR/APK/lint/test APK passed; VpnBudgetRunTest and VpnExitLifecycleTest: 5 passed. Published pre.2 APK restored after tests. No release/deploy. Account meter moved from 86% used to 87% used across C1; shared rounded measurement, not exact per-task/model billing. Stop after C1 to preserve the owner's reserve.

## C2 — complete (shared socket accounting and LTE shutdown)

One run-owned meter covers QUIC UDP, HTTPS TCP below TLS, and AWG outer encrypted datagrams, including current/draining bond sessions and multiple exits. Both directions contribute to the same limit; overlay metrics are not added to physical totals. Android supplies immutable per-run budget references to reconnecting sessions, blocks new cell bindings/probes when exhausted and keeps Wi-Fi eligible. Settings now have one global LTE limit; migration imports the selected legacy profile limit once. Whole VPN Stop/Start starts a new epoch; exit reconnect does not. Shared meter replaces legacy bond split limits; finite legacy callers retain their rotation guard.

Physical sockets close on exhaustion. QUIC retains its logical session for Wi-Fi migration. AWG queued closure checks bind generation to avoid closing a replacement Wi-Fi socket. A best-effort authenticated bond control record tells the peer to stop LTE over remaining non-cellular paths; older peers may ignore it, without disabling client enforcement. Snapshot reports receive overrun; IP/TCP headers and kernel TCP retries remain outside accounting. When the next write cannot fit, LTE stops without charging unsent bytes. Separate user/control/copy overlay statistics remain diagnostic, not additional billed bytes. C3 service downloads/consent and E1 main-screen telemetry remain future work.

Verification: Linux go test -race ./internal/trafficbudget ./internal/awg ./mobile -count=1 -timeout=120s and vet passed; bond race suite passed with explicit exclusion of TestReturningWifiDoesNotLoseUDPBeforeHedge (pre-existing untracked Wi-Fi experiment, outside C2). Gateway/server/demux checks passed; demux has no tests. Tests cover socket partial writes, incoming overshoot, all-cell closure, unchanged Wi-Fi, QUIC session continuity, AWG traffic/rebind, successful GSO fallback, bond rotation with a finite shared budget, and QUIC/HTTPS fallback accounting. Android final build/lint passed; TrafficBudgetTest, VpnBudgetRunTest and VpnExitLifecycleTest: 6 passed. Android socket test uses loopback with real datagrams, not a physical LTE handover acceptance test. Published pre.2 restored after tests; VPN may need manual activation. No release/deploy.

Account meter moved from 87% used at C2 start to 90% used: approximately 3 percentage points, shared and rounded rather than exact task billing. Stopped at C2 checkpoint with 10% remaining, above the owner's 7% emergency reserve. Next: C3.

## Phase-two scope decision — strict split tunneling (2026-10-01)

Owner assigned POST-P1-02 to MVP phase two and accepted its absence in phase one. Code review found no common TUN flow-owner authorization in ordinary QUIC/HTTPS/AWG; multiple routing has partial checks and a port-53 override. Exploit reproduction on the user's device is pending. This does not waive phase-one no-direct-fallback requirements. Requirements and acceptance checks recorded in docs/post-phase1-backlog.md and the product specification. No runtime changes; phase-one progress remains 8/20, C3 next.
