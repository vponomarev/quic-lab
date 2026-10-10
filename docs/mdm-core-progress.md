# MDM core progress

2026-10-08. Branch: codex/mdm-core, based on v0.9.0.
Accepted plan: docs/superpowers/plans/2026-10-04-mdm-core.md.
Historical checkpoints below describe staged implementation. MDM and telemetry are now deployed. See [0.9.1 acceptance](mdm-stable-091-acceptance.md) for current release status; older statements about production or unwired runtime are superseded.

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
Service now resolves the durable lifecycle owner (Task 3); test owner injection remains available.
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

## Task 3 — voluntary lifecycle and consent
Implemented and tested: separate AES-GCM/Android Keystore state in noBackupFilesDir,
atomic writes, durable generations, pending registration credentials before redeem,
single binding even while paused, immediate local pause, bounded best-effort notification,
resume with a fresh server epoch, deletion, rights changes, late-response rejection,
cleanup recovery hooks, and strict independent invitation-link parser.
Consent screen, separate MDM link/QR dispatch, settings entry, permission editing,
pause/delete/resume, persisted runtime owner, notification navigation, launch restoration and
post-unlock boot path are implemented. Invitation opening is read-only, with all permission
checkboxes initially off. Controller response delivery and state mutations share one lock.
The server now records granted rights separately from requested rights, auditing changes.
Remote config/VPN action consumers remain deliberately unacknowledged until Tasks 4/5.
Cleanup hooks must be connected to the actual configuration layer in Task 4 before rollout.

Verification:
- Storage and lifecycle contracts observed RED before implementation.
- API30 storage/lifecycle: 8 tests PASS (7.38s).
- Final combined storage/lifecycle/shared-budget/FGS/persistence: 16 tests PASS (8.275s).
- Final APK/test APK build and lint PASS.
- A lint finding caught InputStream.readNBytes (API33); replaced with bounded API30-compatible reading.

Latest checks (2026-10-08):
- Runtime missing poll contract observed RED, then GREEN.
- Final dedicated Redmi API30 MDM suite: 14 PASS / 7.255s.
- MDM + existing Android navigation: 16 PASS / 13.433s before the final rights wire addition.
- APK, instrumentation APK and lint PASS. Consent screen visually inspected on Redmi.
- UI test direct instrumentation launch timed out; normal Android VIEW launch worked.
  Test now uses the existing project's shell-launch/lifecycle-monitor approach.
- Server grant-reporting contract observed RED, then full internal/mdm race suite x3
  PASS / 2.783s and vet PASS on Linux.
- Actual reboot/unlock acceptance and API34/35 FGS runtime still pending integration acceptance.
- No production MDM deployment; no remote config or VPN command has been applied.

Next: implement Task 4 atomic
current/external configuration layers and mutator rights, Task 5 VPN commands,
Task 6 admin/bootstrap, Task 7 integrated acceptance and whole-branch review.
Telemetry, remote helper, and research mode remain outside this core package.

## Task 4 — configuration transaction foundation (in progress)

Implemented the bounded schema-1 document validator, used by server SetDesired and
the mobile native API. It rejects unknown fields, duplicate JSON keys, null/type
coercion, invalid profile references, invalid routing/pool values and unsafe
credential formats. Invalid input never advances the desired revision.

Added MdmConfigurationStore: encrypted AtomicFile snapshots, current/external
transitions, same-profile credential preservation with explicit removal, binding
ownership, revision deduplication, and idempotent external cleanup. No runtime
config reader, mutator or poll consumer is connected yet: this is a storage
foundation, NOT completed remote configuration management.

Verification on 2026-10-08:
- RED: missing ValidateDocument / ValidateMDMConfiguration and Android store.
- Routing-type regression RED, then GREEN after aligning mode with Android Int.
- Linux internal/mdm race x3 PASS (2.829s), vet PASS; mobile TestMDM race PASS.
- APK + instrumentation APK + lint PASS.
- Dedicated Redmi dc69eb2c API30: final 21 tests PASS / 12.104s
  (configuration store, MDM storage/lifecycle, existing VPN configuration/profiles).
- Note12Pro and production untouched by this MDM work.

Remaining Task4 gates: capture complete existing native identity bundles; make
configuration/profile/identity readers authoritative; enforce permissions on
every editor/import/update mutation including stale drafts; connect pause/delete
cleanup with active VPN and persistent recovery; synchronize applied revision
and report failures. The store's detach alone does not stop any VPN.

### Task 4 — authoritative Android readers and local mutation guard

Added MdmConfiguration + ConfigurationPreferences. After initialization, profile
metadata/settings, DNS, shared budget, diagnostic flags and encrypted identity
bundles read the atomic runtime snapshot. Legacy preferences cannot overwrite it.
External projections are not written into vpn-configuration.json. Original native
identity bundles (including registration/update metadata) survive overlays.
Explicit credential removal does not fall back to the old legacy key file.

Local edits/imports/updates share the configuration lock with MDM generations;
old drafts cannot write after a remote apply or detach. Profile-editor validation
runs before key persistence. RTT/reserve preference listeners remain functional.
Normal registered-profile updates can restore server projection per profile after
current-mode management; they do not globally override other managed profiles.

Verification:
- RED: missing adapter; then API30 reader/restoration/draft tests PASS.
- RED: local cell_mib edit failed to override retained limit_bytes; fixed.
- RED: post-MDM registered-profile update remained hidden by the overlay; fixed.
- Existing identity transaction test now waits on the shared configuration lock,
  replacing the former VpnIdentity monitor while retaining its blocking assertion.
- Final APK/test APK + lint PASS.
- Redmi dc69eb2c API30: final 57 tests PASS / 49.228s (MDM config/store/lifecycle,
  VPN config/profiles, update, RTT/reserve/budget, imports and managed VLESS).

Task4 remains IN PROGRESS. The adapter currently requires stopped VPN.
The production poll consumer and pause/delete runtime hook are NOT wired yet.
Next: coordinate live VPN stop/restart and interrupted apply recovery, authoritative
applied-revision acknowledgement/events, cleanup under process death, and integrated
lifecycle acceptance. Confirm config-read/start serialization before enabling live
poll application. Review storage/read performance with management active.
Additional RED: Activity recreation restored an external draft under a new editor's
generation. Fixed by carrying the original generation in saved draft state and
rejecting stale state across repeated recreation. Stale values are not restored.
Final PreferenceDraft + AndroidNavigation: 3 tests PASS / 6.480s.
No production release or Note12Pro change.

### 2026-10-08 — accelerated radio telemetry slice

User brought Wi-Fi/cell telemetry forward. Implementation and acceptance: docs/mdm-radio-telemetry.md; separate plan docs/superpowers/plans/2026-10-08-mdm-radio-telemetry.md. Core task4/5 remote apply/commands remain unfinished and are not exposed as working actions in admin. Server MDM routes/admin are wired for enrollment, lifecycle and telemetry. Default delivery now includes LTE; local Wi-Fi-only prohibition takes priority.

### 2026-10-08 — WEB management implementation, acceptance pending

See docs/mdm-web-management-acceptance.md and accepted WEB plan. Runtime now consumes desired configuration and VPN commands through serialized main-thread coordination with durable cleanup/revision acknowledgement and command deduplication. Report/inventory requires config consent. The server editor and user-card ownership links are deployed; old clients are gated by web-control-v1. Candidate Android 0.9.1-pre.9 (49) installed on Redmi but not publicly published. Real phone config/VPN consent and live traffic acceptance remain required. This supersedes earlier statements that the poll consumer is unwired; it does not mark full MDM acceptance complete.
