# Client diagnostics delivery Implementation Plan
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Execute inline.
**Goal:** Сохранить историю поездки и доставить её для диагностики без USB.
**Architecture:** Android SQLite журнал с retention и ACK, HTTPS batch uploader по физической сети, Go authenticated bounded storage и закрытая admin timeline.
**Tech Stack:** Kotlin Android API30+, SQLite, JobScheduler, Go1.26.8, existing device tokens.
**Spec:** ../specs/2026-10-07-client-diagnostics-design.md
## Global Constraints
Default enabled, auto only unmetered Wi-Fi, manual LTE; no secret/config/payload/location export. 7d/5MiB events,3d/10MiB summaries,1d/20MiB detail. Server14d/500MiB. Tests Linux; Android Windows. Existing worktree retained.
## Review Focus
- Lost ACK/retry and process death: idempotent batch and durable queue.
- Metered/changed/lost Wi-Fi during upload: stop automatic delivery.
- Disabled or revoked device: reject without writing.
- Large gzip, invalid fields, HTML: bounded input and escaped admin output.
- Changed profile/server: never send old records to a new destination.
## Tasks
- [x] 1. Server: internal/admin/diagnostics.go + tests, web/config/public routing. Test authenticated POST, revoked/missing token, gzip limits, retry/disk retention, admin authorization/XSS; run RED then implement and GREEN on Linux.
- [x] 2. Android DiagnosticsJournal.kt SQLite history+pending ACK; Diagnostics.kt classification and 30s summary. Instrumentation tests retention/classification/settings/default-off persistence/ACK. RED then GREEN.
- [x] 3. DiagnosticsDelivery.kt and job service: network.openConnection over non-VPN network with PKI/no redirect, immutable target, gzip bounded batches, retry/backoff and manual one-shot override. JobScheduler plus live-service timer; no boot VPN. Test policy, failed ACK, endpoint validation and queued delivery.
- [x] 4. DiagnosticsSettings UI integrated in AppSettingsActivity, server timeline entry. Test actual Redmi delivery, disabled collection, opt-in detail, preserved VPN settings.
- [x] 5. Regression, review, documentation, version 0.8.1-pre.1, deployment/test APK. Full phase1 remains open. Publish only verified artifacts.
## Execution ledger
Start: existing clean worktree codex/phase1-vpn at 8b4ebf1; approval already provided in chat, no repeated design gate.
Task 1–4 implemented and reviewed. Fresh verification: Go all-package tests PASS; admin/server race and vet PASS; admin Playwright regression PASS after replacing absolute URL construction with relative journal link (about:blank test fixture).
Android build + lint PASS on API30 target. Diagnostics targeted run: 6 tests PASS including live HTTPS delivery to temporary registered device and rejected-token isolation; server timeline independently verified. Temporary device disabled afterward.
Android broad run: 130 tests, three failures caused by missing/obsolete network arguments; corrected run: 129 tests, one app-picker timeout. App picker passed isolated rerun. Echo tests passed with server :443. WiFiMigrationTest passed with explicit TLS name. Do not present either broad run as wholly green.
Diagnostics UI + current VPN transit smoke: 6 tests PASS (live-upload test opt-in omitted); legacy working profile retained. New UI found two switches, send button and recipients; active VPN with recent transit response.
Final review fixes: API30 compatible bounded response reader (lint), per-profile failure isolation (live invalid+valid credentials test), clear cancellation generation, outbox age cleanup (instrumented stale-file test), directory fsync including identical retry (Go injected sync failure test).
Ruling: legacy diagnostics.json stays local and expires after seven days; never retroactively upload pre-consent detailed records.
Ruling: background JobScheduler period is 15 minutes (Android minimum), active-process timer targets five minutes. OS may defer background work.
Ruling: additional server cap of 5000 batch files bounds filesystem scan cost; oldest records can expire earlier than the configured age.
Limitations: no new eight-hour soak or travel reproduction in this change; no physical WiFi-to-LTE upload-interruption test. Network policy predicates tested, live delivery tested over available physical network. Original road QUIC incident remains open.


Final clean-process Android regression: OK (131 tests), zero failures, 63 seconds; opt-in scenarios skipped by assumptions, BondDeviceTest/PhaseOneSoakTest excluded. Command supplied host/name quic-demo.vpnc.ru and server quic-demo.vpnc.ru:443.
One preceding repeated instrumentation process produced six failures, including Android Package android does not belong to UID errors. Force-stopping target/test processes cleared the condition: isolated 11-test run and subsequent complete 131-test invocation passed without code changes. Root cause of instrumentation UID contamination is not established; do not call it an application fix.
Server deployed and active; website APK SHA256 03688f9a30a672b31b237c9fe3b02e185cab14c596401933e6a4ea74b1e3a6d6 matches local build.
