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

Next: A3, bind routing to stable exits and verify live multiple-VPN behavior. Remaining 18 tasks are not complete. This is a development build, not a release or a verified whole-MVP implementation.

Private ledger: .superpowers/sdd/2026-09-30-phase1-01-model-lifecycle/progress.md.

## A2 — complete

Added synchronized Go lifecycle states and Android session-owning VpnExitController. Restart invalidates the prior generation and closes only that exit. Incompatible exits reject subsequent events. Single and multiple service paths use controller ownership; single-session callbacks capture their original controller so a new whole-service run cannot accept old callbacks with coincident generation numbers. Main-looper service commands remain serialized. Stop-all invalidates callbacks and attempts every close even if one fails; service cleanup still releases TUN. First explicit Stop shows a one-time notice, stored locally.

Verified: Linux go test -race ./internal/vpnmodel -count=1 and go vet ./internal/vpnmodel passed. Android assembleDebug, assembleDebugAndroidTest, lintDebug passed. Device VpnExitLifecycleTest + VpnConfigurationTest: OK (11 tests). Lifecycle cases cover independent restart, stale callbacks, incompatibility, startup failure, and close failure. These tests use controlled session resources; real multi-tunnel traffic/route blocking is the A3 live gate, not claimed here.

Test history: initial Go run failed on missing Runtime/Event APIs. Android's first three lifecycle checks were first executed after implementation (workflow deviation); the added close-failure test was observed RED (only one of two sessions closed), then fixed and observed GREEN. Do not describe all Android lifecycle tests as observed red-to-green.
