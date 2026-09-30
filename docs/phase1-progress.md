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

Next: B1, stable demux session and bounded retention. Remaining 17 tasks are not complete. This is a development build, not a release or a verified whole-MVP implementation.

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
