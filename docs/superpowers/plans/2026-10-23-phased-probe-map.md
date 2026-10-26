# Sequential physical-network probe and availability map Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Test imported VLESS paths on Wi-Fi then permitted LTE (LTE-only when started on LTE), preserve per-network VLESS/HA results, and expose the chosen availability-map UI.

**Architecture:** Retain the two-worker single-phase probe engine and add a serial sequence controller. A run holds immutable source identity and network plan; each phase acquires its own explicit physical-network lease, drains workers, and releases resources before advancing. Remote MDM v1 stays single-network and receives only schema-v1 snapshots until a v2 capability is implemented, rather than sending an incompatible local envelope.

**Tech Stack:** Kotlin, Android Network binding, existing Go VLESS probe, Android instrumentation.

**Spec:** ../specs/2026-10-23-adaptive-paths-design.md (approved by user in chat; execution authorized).

## Global Constraints

- Existing worktree and dirty changes are the baseline; no reset, no whole-file automatic commits.
- Main VPN profile/session is never changed by diagnostics; no radio toggles. Note9 dc69eb2c is authorized for tests, synthetic fixtures preferred.
- Two workers per phase, max 256 nodes, 20 seconds per node, 45 minutes per run. Preserve shared LTE budget.
- Wi-Fi start plans wifi,cell; LTE start plans cell only. Policy/budget denial and unavailable radio produce neutral skipped phases with reason.
- Local sequence schema 2 is not sent to MDM v1. Legacy rows remain readable; store max 32 sources with bounded per-phase payload.

## Review Focus

1. Cancel during lease acquisition or between phases must prevent next phase and release requested Network.
2. LTE permission or budget revoked between phases/while running must stop LTE without recording bad node health.
3. Physical default changes while explicitly bound network remains present must not silently relabel measurements.
4. Worker callbacks after cancellation must not overwrite final results or start another run before drain.
5. UI must not label untested HA as failure; remote v1 must not receive local v2 envelopes.

## Task 1: Sequence lifecycle and storage

Files: create `VlessProbeSequence.kt`, `VlessProbeSequenceTest.kt`; modify `VlessProbeStore.kt`.
Interface: sequence receives run snapshot, phase names, per-phase lease/work factory, and save callback. `start`, `cancel`, `busy`, `snapshot` own a single run and drain every phase before advancing. Phase snapshots keyed by network in `phases`; top-level rows/network mirror current phase for local legacy readability; `schema=2`.
- [x] RED tests: wifi→cell ordered with close-before-next, cell only; skipped phase preserves wifi success; cancel prevents LTE and stale callback mutation; recovery interrupts queued rows in all phases.
- [x] Implement serial lifecycle on top of VlessProbeEngine; retain local parent ID and monotonic sequence; terminate on canceled/configuration_changed/permission_denied/interrupted, permit advancement after phase network loss.
- [x] GREEN targeted instrumentation and storage compatibility.

## Task 2: Explicit network acquisition and coordinator integration

Files: `VlessProbeNetwork.kt`, `VlessProbeCoordinator.kt`, `VlessProbePolicy.kt`, `MdmProbeControl.kt`; tests `VlessProbePolicyTest.kt`, `VlessProbeSequenceTest.kt`.
- [x] RED pure policy tests for start-network planning, reserve LTE permission, budget, pinned-network loss versus default change, and v1 wire filtering.
- [x] Add acquire(kind, canceled) with bounded cellular request and release callback; retain acquire-current for remote legacy behavior. Check policy/budget before requesting and before work; current LTE needs budget but not reserve permission.
- [x] Integrate local sequence, immutable fingerprint validation, single busy gate including acquisition/drain, resource cleanup and cancellation. Keep remote v1 path and schema compatible.
- [x] GREEN all existing probe/subscription tests plus new tests. Device test both physical bindings with synthetic no-payload work where feasible, restore temporary settings.

## Task 3: Availability map and details

Files: `VlessProbeActivity.kt`, new pure `VlessProbePresentation.kt` and instrumentation tests.
- [x] RED classification tests: VLESS/HA success, HA failure after VLESS success, HA not configured/not tested, skipped/budget/canceled, legacy rows; filter counts use same classification as tiles.
- [x] Implement compact adaptive grid, network result selector, per-phase progress, stable node labels, detail dialog with SNI/transport/RTTs/error stage/time, neutral skipped states, filters and sorting. Details accessible via touch/keyboard, preserve selection across refresh/rotation.
- [x] GREEN build/lint/instrumentation; show actual Activity on Note9 with synthetic preview results in isolated test source, capture screenshot, clean preview source after inspection.

## Verification and integration

Centralize Gradle/ADB in parent (automatic-group agent owns other files). JDK17 `C:/Users/vitaly/.jdks/quic-lab-jdk17`, SDK `C:/Users/vitaly/AppData/Local/Android/Sdk`, Gradle cache `C:/Users/vitaly/.gradle`.
Run assembleDebug assembleDebugAndroidTest lintDebug; install -r app/test; run selected non-destructive instrumentation classes. Run independent review after integration. Native AAR is rebuilt if group transport changes require it. Keep a ledger in ignored `artifacts/phased-probe/`. Git checkpoints deferred until incoming diff can be separated without committing unrelated changes.

Acceptance record: docs/testing/adaptive-groups-probe-map-2026-10-24.md. Physical Wi-Fi acquisition passed; secondary LTE explicitly skipped because reserve is disabled. Debug preview used synthetic data without persistent source injection. Independent reviews found and verified fixes for retry union, cancellation generation, stale lease, busy release, and stale legacy cancellation.
