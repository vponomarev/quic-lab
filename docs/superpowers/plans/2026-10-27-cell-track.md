# Cell Track Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans task-by-task.

**Goal:** Visualize device cell-derived movement in the existing admin.
**Architecture:** Bounded telemetry range reader; authenticated track and resolver endpoints; vendored Leaflet and plain JavaScript embedded in Go.
**Tech Stack:** Go, JavaScript, Leaflet 1.9.4, Mylnikov open API, OSM.
**Spec:** docs/superpowers/specs/2026-10-27-cell-track.md

## Constraints and review focus
Preserve raw telemetry and settings. No deploy in this task. No external transmission of timestamps/device/Wi-Fi identity. Unknown locations and collection gaps must not become continuous routes. Test same timestamps, provider outages, numeric strings/missing coordinates, auth and CSRF, cancellation and unsupported technology.

- [x] Add failing tests for date range boundaries and provider parsing/cache/auth; verify red.
- [x] Implement internal/mdm telemetry range reader and internal/admin/mdm_track.go endpoints/resolver; verify green.
- [x] Add embedded map UI, local Leaflet assets, timeline and gap tests; verify using node and Go.
- [x] Review full diff, run focused Go tests/vet and UI smoke check; document results.

## Execution ledger
Initial choice: reuse existing isolated phase1-vpn worktree. Map is on the existing device history page; no new login or frontend build pipeline.

Final: red/green and Linux package tests/vet pass; Node tests pass; actual Handler browser smoke with synthetic data passes. Reviewer CSP/ManagementOnly findings fixed with regression coverage. Evidence: docs/testing/cell-track-2026-10-27.md. No deployment.
