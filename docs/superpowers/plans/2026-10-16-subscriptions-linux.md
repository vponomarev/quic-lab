# Subscriptions then Linux implementation plan

Use superpowers:executing-plans inline. User explicitly requested autonomous execution.
Spec: ../specs/2026-10-16-subscriptions-linux-design.md

- [x] S1: Add Android instrumentation parser tests (plain/base64, limits, duplicate identity, unsupported, key rotation). Observe RED; implement VlessSubscriptions parsing and reconciliation.
- [x] S2: Extend encrypted runtime transaction for subscription metadata; implement list/add/refresh UI and preview, preserving MDM checks and current selection. Tests for idempotence/local settings/failure.
- [x] S3: Build/lint APK and run selected instrumentation on Note9, preserve current personal config/VPN. Record unsupported cases honestly.
- [x] L1: Add Linux config/lifecycle tests and production TUN backend/CLI. Reject unsafe capture and loop endpoints. Observe RED/GREEN.
- [x] L2: Use isolated namespace fixture on test host, real QUIC/HTTPS TCP/UDP and cleanup; package CLI and service template.
- [x] R: Review changes, record verification and remaining scope; commit only task files.

Review focus: URL secrets/errors, partial subscription failure and no-op refresh, MDM race, non-owned Linux network resources, failed-start cleanup.
