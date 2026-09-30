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

Next: B3, progress-based path health and profile selection. Remaining 15 tasks are not complete. This is a development build, not a release or a verified whole-MVP implementation.

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