# Automatic Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate logical connection selection into Android normal VLESS groups and shared-exit HA runtime.

**Architecture:** Preserve VpnSession and ExitRuntime ownership of lifecycle and LTE permissions. Carry logical IDs in private runtime envelopes; do not change portable model version. Keep subscription nodes subordinate to one connection.

**Tech Stack:** Kotlin Android, Go mobile runtime, instrumentation and Go tests.

**Spec:** docs/superpowers/specs/2026-10-23-adaptive-paths-design.md (approved by user after its draft header).

## Global Constraints
- HA has one exit, at most two carriers, no direct fallback.
- Existing LTE budget and reserve permissions remain authoritative.
- Preserve inherited changes; no commits by this worker.
- Parent runs all Gradle and ADB operations.

## Review Focus
- Different connection temporarily unavailable: same-subscription reserve remains possible.
- Retry backoff must not starve another connection.
- Existing connection identity absent: preserve legacy compatibility.
- Healthy primary must not flap on priority alone.
- Group members must not introduce overlapping routing rules or bypass managed restrictions.

### Task 1: HA logical reserve selection
Files: mobile/exit_runtime.go, mobile/exit_runtime_relay_test.go, android/.../VlessRelayRuntime.kt.
Interface: private envelope logical_connections map profile ID -> logical connection ID. Missing ID uses profile ID for compatibility.
- [x] Add test: relay1 and relay2 belong to A, relay3 belongs to B; actual admitted paths must be relay1 and relay3.
- [x] Run go test ./mobile -run TestRelayLogical -count=1; expected behavioral failure.
- [x] Select eligible reserve by differing logical connection before same connection, preserving retry/network/pool guards.
- [x] Run ./mobile tests and relevant package suite.

### Task 2: Normal group fair failover and explicit membership
Files: VlessGroupPolicy.kt, VpnSession.kt, VpnConfiguration.kt, ProfileEditorActivity.kt, AutomaticVpnGroup.kt, AutomaticVpnGroupTest.kt.
Interface: private canonical candidates and logical ID arrays; explicit member profile IDs in local preferences. Root profile owns mode and traffic rules. Reject overlapping membership in multiple VPN plans.
- [x] Add instrumentation for fair selection across A nodes and B, expanded catalog, projection without URI conversion.
- [x] Parent runs RED; implement after failure.
- [x] Expose membership selector for supported VLESS profiles and mode through existing relay exit selection.
- [x] Parent runs GREEN and existing configuration/runtime instrumentation.

### Task 3: Stability and capability reporting
- [x] Add deterministic tests for healthy-primary retention and sustained degradation.
- [x] Integrate degradation using existing useful probe RTT and minimum 8 second continuous improvement; retain healthy primary otherwise.
- [x] Report unsupported AWG transit and mixed normal transports explicitly; these require lifecycle adapters, not inferred capability.

## Execution ledger
Ruling: Reuse parent's approved execution authorization; no additional plan approval gate.
Ruling: Use private envelope logical IDs rather than portable vpnmodel fields to avoid incompatible MDM schema changes.

## Implemented decisions and evidence (2026-10-24)
- Explicit membership is stored on the root VLESS profile; empty set preserves existing behavior. Root rules and Normal/HA selector apply to all members. Nested groups and overlapping enabled multiple exits fail closed. Membership checks MDM profile-selection rights.
- Ruling: cap a group at 16 logical connections / 4096 nodes, with at most 256 nodes per source. This avoids silent truncation while retaining each subscribed source's approved catalog bound. Cost if wrong: exceptionally large groups fail with a visible error until limits are revised.
- Ruling: first integrated adapter is VLESS in both modes; AWG and mixed standalone transports are explicitly unavailable in the editor. Existing direct QUIC/HTTPS demux remains intact. This is an incomplete part of the broader requested feature, not an assertion that adapters exist.
- Ruling: private `logical_connections` map belongs to ExitRuntime envelope, not portable model; old clients remain unchanged.
- HA keeps two carriers; reserve admission prefers another logical connection, then allows another node from the same source. Existing LTE eligibility, reserve permission and budget gates still govern admission.
- HA healthy primary retained. Degradation requires primary >600 ms and reserve <half primary RTT, fresh useful samples, 8 seconds continuous evidence, 30 seconds between promotions. No degradation switching is claimed for normal mode yet.
- RED: native logical reserve test admitted relay1+relay2 instead of relay1+relay3; healthy-primary test switched back to first priority. Both GREEN after implementation.
- RED: native sustained degradation test did not switch; GREEN after window implementation. Spike resets window. Generation-change regression also passes.
- RED on Note9 from parent: AdaptiveGroupsTest catalogBeyondLegacy32ParticipatesInFailover and reserveEnvelopeKeepsSubscriptionAsOneConnection both failed for intended missing behavior.
- Native regression command selecting real mobile GoFiles/TestGoFiles except public_awg_test.go, `-run TestRelay|TestRuntime|TestAutoRecovery`, passed 11.5 s.
- Full selected-file native suite ran: failed TestBondVLESSSharedExitContinuity/reset timeout and TestVLESSSocketRefusesUnboundOrUnresolved (legacy test rejects IPv6 while inherited socket factory allows it). These are not hidden; full suite is not green.
- Standard `go test ./mobile` cannot compile on Windows because public_awg_test.go pulls awgserver.Worker.listenReload, defined only on Linux. Elevated WSL inventory has no distributions.
- Pending: parent instrumentation GREEN including AdaptiveGroupsIntegrationTest; fairness RED/GREEN; full native suite findings; physical failover acceptance; normal sustained degradation, bounded background candidate checks, AWG/mixed transport adapters.
- Ruling after transfer review: membership remains local-only, removed from VpnConfiguration portable cache; MdmConfiguration.settingsKeys already omits it. Editor explicitly states export/MDM do not transfer membership. Portable support requires reference remapping and a new client capability, so adding a whitelist alone would be unsafe. Cost: groups must be reassembled after transfer; root profile and existing relay target transfer independently.
- Continuity/reset isolated rerun passed: one destination accept, exact payload, 9 ms warm failover, 10-second outage, same session. Full suite's prior timeout is still recorded.
- Instrumentation fixture issue: initial integration ContextWrapper omitted noBackupFilesDir override, so Note9 personal MDM runtime was read and two exact synthetic test identities could be written there. Fixed override and explicit pre-write isolation guard. One-off opt-in AdaptiveGroupsFixtureCleanupTest prepared for parent review: deletes only exact unreferenced synthetic IDs after both full payload signatures match; does not alter profiles/settings. Earlier finally did not delete identities, only test-prefixed prefs and isolated filesDir. Root cause is fixture isolation, not runtime group projection.
- Fairness RED on Note9: expected recovered connection index1, actual remaining subscription index3. Added logical connection IDs to VlessGroupPolicy and wired VpnSession to private canonical candidate identities; other connections are retried before more nodes of the current source.
- Diversity refinement RED→GREEN: reserve now sorts by another logical source first, then another permitted physical network. Test verifies permitted LTE wins over same-network Wi-Fi and prohibited LTE falls back to Wi-Fi; focused native regression passes12s.
- Parent confirms group fairness and corrected isolated integration tests GREEN (part of49 tests); one-off exact-fixture cleanup passed1 test.
- Independent review finding in progress: individually imported profiles from one subscription need subscription metadata logical identity. Added helper-level and actual personal-runtime projection RED tests, awaiting parent execution. Added local membership persistence test with personal MDM runtime layer (portable key remains excluded deliberately).
- Final logical-cap ruling: up to32 selected physical profile entries (existing store bound), at most16 distinct metadata-resolved logical sources, at most4096 total candidates. Subscription node profiles count as one source; no truncation. UI/save and runtime guards use the same logical counting.
- Parent confirmed subscription helper/runtime identity tests and personal-layer persistence GREEN (part of37 tests; cap was the only expected failure). Cap RED confirmed then implementation adjusted.
- Latest native focused regression TestRelayLogical|TestRelayPool|TestRuntime passed11.626s. Own compiler cache .cache/go-adaptive safely removed after resolved-path containment check. Own logs moved to artifacts/adaptive-groups/ (full-suite failure, isolated continuity pass, latest native pass). Inherited files/cache untouched.
- Scope freeze after final confirmed blocker: overlapping grouped+individual imports must deduplicate canonical nodes within a logical subscription so normal retry indices cannot bypass cooldown. RED test queued with parent.
- Final duplicate blocker RED on Note9: expected3 unique nodes, got5 when grouped and individual imports overlap. Group envelope now deduplicates (logical connection ID, canonical config) before total bound. Parent's77-test batch had only this expected failure; LTE hardware test skipped because LTE is user-disabled. Scope frozen, final focused group verification pending.

## Remaining feature work (not claimed complete)
- Mixed normal transports and AWG transit adapters remain unsupported, explicitly visible in the editor.
- Normal-mode sustained-degradation selection and bounded background reachability checks of inactive candidates are not implemented by this slice; normal mode uses existing failure/cooldown recovery plus logical fairness.
- Group membership transfer/MDM provisioning requires portable schema/reference/capability work; local personal runtime persistence is tested, export omission is stated in UI.
- Hardware acceptance across Wi-Fi/LTE, battery/background lifecycle and full daily-use matrix remain with parent. User-disabled LTE was not enabled.
- Full native suite is not declared green: selected-file run recorded the inherited IPv6 assertion mismatch and one continuity reset timeout; isolated continuity reset rerun passed. Normal package test on Windows is blocked by Linux-only AWG reload implementation. Focused changed-runtime tests and pathpolicy/vpnmodel passed.

Final verification: native targeted regression passed; full-suite platform/baseline failures remain documented above. Latest 23 group/runtime Android tests passed after deduplication; source identity, personal-layer persistence and logical-source cap regressions passed. Debug APK installed on Note9. Wider adapters, physical HA acceptance and ordinary latency degradation remain explicitly pending in docs/testing/adaptive-groups-probe-map-2026-10-24.md.
