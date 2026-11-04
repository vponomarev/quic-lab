# Portable automatic groups Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline. Preserve unrelated changes. Steps use checkbox tracking.

**Goal:** Preserve explicit automatic-group membership and relay references through configuration transfer and supported MDM clients.
**Architecture:** Extend existing schema1 settings with automatic_group_members; strict graph validation and explicit source-to-target mapping. Advertise automatic_groups_v1 on client/server; prevent delivery to old clients and avoid sending unsupported settings to old servers. Existing atomic runtime apply/rollback remains the owner.
**Tech Stack:** Go validators/transfer/MDM, Kotlin Android, Linux tests and Note9 instrumentation; no dependencies.
**Spec:** ../specs/2026-10-23-adaptive-paths-design.md. User requests autonomous batch progress on 2026-11-04.

## Global Constraints
- Existing isolated phase1-vpn worktree, baseline 6d15055. Do not stage unrelated cell/server edits.
- No production deployment, live MDM writes, real profile changes or radio toggles. Linux tests use approved isolated source checkout; phone tests isolate filesDir, noBackupFilesDir and preferences.
- Keep schema1 and existing bundle version; old strict readers reject unknown settings. No silent membership dropping on import.
- Profile references must map explicitly, including partial export dependencies. No implicit matching by ID/name.
- Retain existing no-nested-group, no-self/duplicate/missing member and multiple-exit overlap rules. At most31 members; runtime logical limit16 and candidate limit4096 remain.

## Review Focus
- Remap references even when source IDs collide with unrelated destination IDs.
- Reject stale/cyclic/group-overlap updates before committing or disconnecting VPN.
- Old-client capability downgrade must stop pending desired-config delivery; old-server round trips must not erase local membership silently.
- Managed group dependencies must be usable only as part of an authorized root, without granting standalone selection.
- Partial import/update/rollback preserves unrelated group settings and secrets; new groups lacking member credentials must not resume VPN.

## Task 1: Native reference contract and MDM gates
Files: internal/mdm/config.go, new group_config.go/tests, report.go/control.go; internal/configtransfer/merge.go, new references.go/tests.
Interfaces: validateGroupReferences(ConfigurationDocument), allowsAutomaticGroups(bindingState,raw), remapProfileReferences(settings,mapping).
- [x] RED tests valid mixed group and graph failures; capability set/sync/downgrade; import add/update/replace references, explicit external dependency mappings, collision rejection and unchanged base.
- [x] Implement strict member-array validation, graph checks, remapping members + relay target, capability gating on all desired writes and sync.
- [x] GREEN Linux mdm/configtransfer suites, race/vet; rebuild AAR.

## Task 2: Android preservation and compatibility
Files: MdmConfiguration.kt, MdmDeviceReport.kt, MdmModels.kt, MdmController.kt, AdaptiveGroups.kt, ConfigurationTransfer.kt/Activity.kt, LocalConfigurationApply.kt, ProfileEditorActivity.kt; new isolated instrumentation.
- [x] RED local snapshot/compile/export/remap/apply/rollback tests and dependency enumeration, incomplete-group resume protection, managed dependency and legacy-server behavior.
- [x] Include membership in snapshot; collect member and relay IDs for mapping UI; show meaningful group changes and updated copy.
- [x] Advertise/support capability, preserve groups on old-server roundtrip, authorize declared managed dependencies without standalone selection.
- [x] GREEN build/lint and isolated Note9 transfer/MDM/group regressions.

## Task 3: Related reliability hardening and delivery
- [x] Exercise deletion/reference and interrupted apply/rollback cases; fix confirmed related defects with RED/GREEN tests.
- [x] Independent final review, fix important findings, full native tests and focused device suite.
- [x] Update roadmap/evidence and commit only iteration changes. Keep branch locally per autonomous instruction.

Ruling: Use already authorized inline autonomous execution; no approval menu. Preserve existing schema and explicit mapping to minimize migration risk. No subscription URLs/refresh secrets become portable metadata; loss of private subscription metadata remains distinct from transferring group membership.
