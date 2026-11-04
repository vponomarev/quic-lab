# Probe phase reporting Implementation Plan

> Execute inline with superpowers:executing-plans; fresh independent reviewer at completion.

**Goal:** Deliver local Wi-Fi/LTE diagnostic results through negotiated MDM reports and show each phase.
**Architecture:** Optional schema/phases on existing ProbeSnapshot; bounded public projection; existing consent/source/freshness checks.
**Tech Stack:** Kotlin Android, Go, browser JavaScript; no dependencies.
**Spec:** ../specs/2026-11-04-probe-phase-reporting.md

## Global Constraints
- Base b6749a3, preserve unrelated dirty cell/admin/server files; explicit staging only.
- Capability vless_probe_phases_v2; old server wire unchanged; remote commands remain v1.
- 1–2 phases, max256 rows/phase, snapshot768KiB, report inclusion900KiB; existing1MiB request limit.
- No real user VPN/MDM/radio changes, no production rollout. Linux tests only.

## Review Focus
- Old server must never see schema2 or nested phase data.
- Unknown/private fields and nested ownership metadata must not leak.
- Missing/revoked rights, source changes and stale sequence cannot replace valid evidence.
- Skipped/queued/canceled rows must not look like node failures or successes.
- Large phased results must not prevent normal MDM reports; phase selector survives refresh.

## Task 1: Native validation and persistence
Files: internal/mdm/probe.go, new probe_phases.go/test.go, control.go.
- [x] RED roundtrip/validation tests through Store.Report and SaveProbe, schema/capability/source/rights/stale sequence/limits.
- [x] Implement optional schema/phases and bounded validation, keep legacy behavior.
- [x] Linux focused tests/race/vet GREEN.

## Task 2: Android negotiated public projection
Files: new MdmProbeReport.kt and test, MdmProbeControl.kt, MdmDeviceReport.kt, MdmModels.kt, MdmController.kt.
Interface: MdmProbeReport.select(all,sources,state,phasesSupported) returns public snapshot or null; append respects total report bound.
- [x] RED compatibility, phase preservation, private fields, source/owner filtering, oversized-report tests; capability parser test.
- [x] Implement projection and capability handshake, integrate existing report owner.
- [x] Build/install and run isolated Android regressions GREEN.

## Task 3: Admin phase view and delivery
Files: internal/admin/mdm_probe.js/html, new mdm_probe_js_test.cjs; docs/testing evidence.
- [x] RED DOM test Wi-Fi/LTE selection, skipped neutral state/filter, legacy view, no v1 retry/cancel for schema2.
- [x] Implement selector and phase notice; GREEN JS test.
- [x] Full Linux suite, Android assemble/lint/regression, independent review/fixes, evidence and scope commit.

Ruling: execute autonomously under existing authorization; no approval menu. Keep local branch/worktree. Windows skill shell helpers replaced by explicit PowerShell commands and persisted logs.
