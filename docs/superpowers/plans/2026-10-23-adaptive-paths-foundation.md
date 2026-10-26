# Adaptive paths foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Preserve subscription identities across the transferred update and make aggregate probe source validation consistent, providing a tested base for adaptive groups and two-network diagnostics.

**Architecture:** Reuse existing Kotlin subscription merge and probe source models. Preserve legacy profile IDs rather than rewriting references; resolve current subscription entries against stored identities. Share aggregate-source lookup between start and periodic validation.

**Tech Stack:** Kotlin/Android instrumentation, Go mobile AAR, Gradle/JDK 17, ADB Note9.

**Spec:** ../specs/2026-10-23-adaptive-paths-design.md

## Global Constraints

- Preserve all 20 modified tracked and 5 new handoff files; never reset the worktree.
- User approved development and Note9 debugging on 2026-10-23; use serial dc69eb2c, preserve installed app data and physical-network state.
- This is the first independently testable sub-plan. Subsequent plans cover group selection/HA adapters, phased probe UI/MDM, Windows and Linux. This plan does not claim those features delivered.
- Tests first; no publishing or production server changes. No secrets in logs.
- Native execution chosen by executor to retain context across tightly coupled subscription/source changes; user already explicitly requested implementation, so no repeated permission checkpoint.

## Review Focus

1. Legacy profile is currently selected or referenced by another profile: retain its ID, settings and references (task 1).
2. Refreshed credentials/SNI: update credentials while preserving node identity, invalidate probe fingerprint (task 1).
3. Same endpoint with distinct WS paths/security: do not merge unrelated nodes (task 1).
4. Legacy selected keys group probe sources incorrectly: aggregation must locate retained legacy IDs (task 2).
5. Aggregate source is added/removed/reordered during a run: unchanged/reordered snapshots stay valid, membership/credential changes invalidate (task 3).

## File map

- `android/app/src/main/java/ru/vpnc/quiclab/VlessSubscriptions.kt`: identity matching, retained profile lookup and selected-key migration.
- `android/app/src/main/java/ru/vpnc/quiclab/VlessProbeModels.kt`: source lookup and aggregation of subscription-owned profiles.
- `android/app/src/main/java/ru/vpnc/quiclab/VlessProbeCoordinator.kt`: same source resolution at start and periodic revalidation.
- `android/app/src/androidTest/java/ru/vpnc/quiclab/VlessSubscriptionsTest.kt`: upgrade fixtures and refresh assertions.
- `android/app/src/androidTest/java/ru/vpnc/quiclab/VlessProbeStoreTest.kt`: aggregate snapshot stability/invalidation.

## Task 1: Preserve pre-update standalone subscription profiles

**Interfaces:** Existing `parse(bytes): Parsed`, `merge(before, group, parsed): JSONObject`. Add pure lookup `profileId(before: JSONObject, group: String, entry: Entry): String` only if needed by task 2; old IDs remain valid.

- [x] Add legacy snapshot fixture using the previous key SHA256 of literal `vpn.example.org:443|tls|`; derive its deterministic UUID as the previous version did, not through current parser. Assert one profile remains, same ID/currentProfileId/local rules, refreshed UUID/SNI after merge. Add distinct transport/path non-coalescing case.
- [x] Build and install test APK, run `VlessSubscriptionsTest` on Note9; observe expected duplicate-profile failure.
- [x] Resolve current entries to existing subscription-owned profiles using their original ID and canonical identity. Reject ambiguous matches rather than overwrite; preserve legacy IDs and references, update only imported connection identity as existing merge does.
- [x] Run all `VlessSubscriptionsTest` tests, expect success; check input snapshot unchanged and current-format idempotence.
- [x] Record focused diff and test evidence; do not accidentally commit inherited changes as new implementation.

## Task 2: Keep selected metadata and probe grouping coherent

**Interfaces:** Reuse task 1 identity/profile lookup; `selected(c, rawURL): Set<String>` returns current keys where stored profiles establish unambiguous correspondence. Preserve temporarily missing entries.

- [x] Add test for legacy selected keys mapping to refreshed entries and subscription membership retaining the old profile ID. Assert a profile belonging to a different group is excluded.
- [x] Run tests and observe mismatch before implementation.
- [x] Share compatibility lookup between subscription selection and source aggregation without needing subscription URL/credentials in public results.
- [x] Run subscription and probe model/store tests; expect all pass.
- [x] Record evidence and checkpoint only this task's changes together with required task 1 changes.

## Task 3: Validate aggregate probe snapshots consistently

**Interfaces:** Add `findProbeSource(sources: List<ProbeSource>, id: String): ProbeSource?` in `VlessProbeModels.kt`. Used both when starting and checking an active run.

- [x] Add tests: aggregate lookup returns current aggregate; reordering sources retains fingerprint; changed member credentials or removed source changes fingerprint; unknown ID returns null. Assert real source data and fingerprint behavior.
- [x] Run instrumentation and observe failure before helper/integration exists.
- [x] Implement shared lookup and use in both coordinator branches. Keep max-node enforcement and cancellation behavior.
- [x] Run full probe instrumentation classes, then Android assemble/lint. Confirm no regression in source labels or credential stripping.
- [x] Fresh review of the incremental diff; document remaining group/HA/UI work without claiming release readiness.

## Execution commands

Set JAVA_HOME to `C:/Users/vitaly/.jdks/quic-lab-jdk17`, ANDROID_HOME to `C:/Users/vitaly/AppData/Local/Android/Sdk`, GRADLE_USER_HOME to `C:/Users/vitaly/.gradle`.

Build: `android/gradlew.bat -p android assembleDebug assembleDebugAndroidTest lintDebug --console=plain --offline`. Install app/test with `adb -s dc69eb2c install -r <apk>` (never uninstall to resolve a signature conflict). Tests: `adb -s dc69eb2c shell am instrument -w -r -e class ru.vpnc.quiclab.VlessSubscriptionsTest,ru.vpnc.quiclab.VlessProbeStoreTest ru.vpnc.quiclab.test/androidx.test.runner.AndroidJUnitRunner`.

Expected: Gradle BUILD SUCCESSFUL; instrumentation OK, no failed tests. Instrumentation fixture uses synthetic profiles and separate storage, not the user's subscription. Test installation must preserve app data. Runtime fixture tests beyond these classes require inspection before running.

## Coverage boundary

This plan implements foundation step 1 and the aggregate consistency prerequisite of step 2 in the spec. It deliberately does not implement new group configuration, AWG HA adapter, full catalogue limits, two-network probe storage/MDM capability, map UI, or desktop changes. These require dependent implementation plans after the foundation establishes compatibility, not vague unchecked tasks pretending to specify them.
