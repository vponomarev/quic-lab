# Remote probe sequence Implementation Plan

> Execute inline using superpowers:executing-plans, TDD and one fresh final reviewer.

**Goal:** Report all-source results and expose capability-gated remote Wi-Fi/LTE sequences.
**Architecture:** Existing sequence/network leases, explicit command kind and local owner guard.
**Tech Stack:** Kotlin, Go, JavaScript, existing dependencies.
**Spec:** ../specs/2026-11-04-remote-probe-sequence.md

## Global Constraints
- Base eea0f15, existing worktree; exclude unrelated dirty files. Linux Go tests only.
- Retain legacy command semantics, 5minTTL,2workers,256nodes,45min,32sources, existing report bounds.
- Preserve Note9 profiles/rights/network state; no production/push/merge.

## Review Focus
- Downgraded clients must not receive new commands; reports need matching start/retry identity.
- Ownership revocation during acquisition/between phases/worker start stops further work and releases leases.
- Aggregate must use all sources or be absent, never silently truncate; membership change invalidates fingerprint.
- Retry/cancel target exact run/schema/source and cannot cross binding generations.
- Quiet mode rejects start/retry while permitted cancellation works; UI requests match displayed protocol.

## Task 1: Server command contract
Files: internal/mdm/probe.go,probe_phases.go,control.go,types.go,new probe_sequence_test.go; internal/admin/mdm_probe.go.
- [x] RED newkind/capability/idempotency/downgrade/report ownership/interrupted phases tests.
- [x] Implement request mode, newkind gating, matching report command and phase interruption. GREEN native tests/race/vet.

## Task 2: Android aggregate and owned sequence
Files: MdmProbeReport,Control,Controller,Models,DeviceReport,Runtime; VlessProbeCoordinator/Sequence; tests.
- [x] RED bounded aggregate, old-server omission, remote schema2 ownership and negotiation tests.
- [x] RED sequence revoke before/during acquisition and before second phase/worker; retain local and legacy behavior.
- [x] Wire remote owner/target, newkind and protocol guards; build/device tests GREEN.

## Task 3: Admin controls and final verification
Files: mdm_probe.js/html,mdm_probe_js_test.cjs; docs/testing.
- [x] RED/GREEN explicit phased start, cap-gated retry/cancel and submitted mode.
- [x] Full Linux suite, Android assemble/lint/regression, independent review+RED/GREEN fixes.
- [x] Evidence, update prior plan, scope commit; retain local branch.

Ruling: autonomous execution previously requested; no repeated approval. Explicit PowerShell logs/ledger replace skill shell helpers on Windows.

Evidence: [remote-probe-sequence-2026-11-04](../../testing/remote-probe-sequence-2026-11-04.md). Full Linux/race/vet PASS; Android build/lint PASS and 167/167 device tests; JS PASS. Two Important review findings fixed with RED/GREEN, one Minor deferred. Source hashes checked against isolated Linux test tree.
