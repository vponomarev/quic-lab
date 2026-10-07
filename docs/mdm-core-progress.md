# MDM core progress

2026-10-08. Branch: codex/mdm-core, based on v0.9.0.
Accepted plan: docs/superpowers/plans/2026-10-04-mdm-core.md.
Production remains v0.9.0; MDM is not exposed there.

## Task 1 — server foundation
Implemented in bc3669b: durable private snapshot, hashed credentials, idempotent enrollment,
independent TLS >=1.2 device API, epoch invalidation, command TTL/queue bounds,
configuration revision CAS, event deduplication, 90-day pruning, cancellable long polling.
Linux race (three repeats) and vet passed. Server bootstrap/admin wiring belongs to Task 6;
configuration schema validation belongs to Task 4.

## Task 2 — independent Android control channel
Implemented in 055a6f8: physical-network-bound HTTPS, cancellation on network loss,
TLS verification/no redirects, byte accounting before TLS, shared durable LTE budget,
separate foreground service and boot-receiver skeleton.
Service is test-owned until the durable lifecycle composition is wired.
Shared control leases preserve LTE accounting when VPN stops; offline config/APK transfers
now share that same owner. An explicit VPN Stop defers budget reset while control is active.

Verification:
- Native Linux MDM tests: observed RED, then race PASS (three repeats); vet PASS.
- Android APK/test APK build and lint PASS.
- Dedicated Redmi dc69eb2c / Android 11 (API30): 18 tests PASS, 11.042s.
  Includes real HTTPS to a temporary Linux fixture without VPN, after VPN stop, redirect
  rejection, cancellation/replacement, actual Wi-Fi disable/enable and recovery, budget
  rejection before DNS, FGS Pause, and existing budget/transfer regressions.
- Android 14/15 (API34/35) foreground-service runtime remains UNVERIFIED, a release gate.

## Task 3 — lifecycle foundation in progress
Implemented and tested: separate AES-GCM/Android Keystore state in noBackupFilesDir,
atomic writes, durable generations, pending registration credentials before redeem,
single binding even while paused, immediate local pause, bounded best-effort notification,
resume with a fresh server epoch, deletion, rights changes, late-response rejection,
cleanup recovery hooks, and strict independent invitation-link parser.
No MDM consent screen or production runtime composition is wired yet.
Cleanup hooks must be connected to the actual configuration layer in Task 4 before rollout.

Verification:
- Storage and lifecycle contracts observed RED before implementation.
- API30 storage/lifecycle: 8 tests PASS (7.38s).
- Final combined storage/lifecycle/shared-budget/FGS/persistence: 16 tests PASS (8.275s).
- Final APK/test APK build and lint PASS.
- A lint finding caught InputStream.readNBytes (API33); replaced with bounded API30-compatible reading.

Next: finish Task 3 consent UI and runtime/boot composition; implement Task 4 atomic
current/external configuration layers and mutator rights, Task 5 VPN commands,
Task 6 admin/bootstrap, Task 7 integrated acceptance and whole-branch review.
Telemetry, remote helper, and research mode remain outside this core package.
