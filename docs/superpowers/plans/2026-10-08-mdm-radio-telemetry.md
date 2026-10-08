# MDM radio telemetry implementation plan

> **For agentic workers:** Use superpowers:executing-plans. Continue inline; preserve the existing MDM core ledger.

**Goal:** The user can explicitly enable Wi-Fi/cell telemetry on Android and inspect measurements in the authenticated server UI, without a map.

**Architecture:** Use the independent MDM identity/control channel and a separate bounded telemetry queue. A location foreground service samples structured Android radio data; telemetry uploads obey a physical-network delivery policy. The server stores measurements separately from control state and exposes a paginated table.

**Tech Stack:** Kotlin / Android API 30+, existing Go HTTPS server, encrypted local storage, atomic private server files.

**Spec:** docs/superpowers/specs/2026-10-04-client-management-design.md §6.
**Authorization:** 2026-10-08 user brought telemetry into this iteration, with phone-to-server UI acceptance. Existing approved spec governs consent and retention.

## Global constraints
- One voluntary MDM binding; pause stops collection/delivery and manual tracking. All consent defaults off.
- Separate Wi-Fi/cell consent. No GPS coordinates in this slice.
- User override 2026-10-08: delivery defaults to any network including LTE. Local checkbox «Только Wi-Fi» forbids LTE and batches until any Wi-Fi (including metered). The local prohibition also applies to manual tracking; shared LTE budget still applies.
- Revoke consent deletes unuploaded corresponding data. Already uploaded data remains.
- Client queue at most 24 hours / 50 MiB; report discarded count. Control has a separate queue.
- Server history 90 days by default; configurable retention and explicit deletion.
- This user update supersedes §6 default unmetered Wi-Fi policy; consent remains off by default.
- Collect every 30 seconds when permitted; include measurement age/unavailable reason, never fabricate radio identity.
- Network policy checked against the actual bound physical network, including capability changes during an upload.
- No GPS, remote helper or arbitrary commands; unfinished core configuration/command actions remain unavailable in the UI.

## Review focus
- Late cell callbacks / upload responses after revoke: generation guard and queue removal.
- Wi-Fi-only policy bypassed on handover: actual bound transport checked before/during transfer; metered Wi-Fi is allowed.
- Process death/reboot: manual mode does not resume; ordinary collection follows Android permission/FGS restrictions with visible status.
- Duplicate upload after lost response: immutable sample IDs, idempotent server storage.
- Missing SIM/location permission: unavailable fields, no stale cell ID presented as current.

## Task 1: Server telemetry model and storage
Files: internal/mdm/telemetry.go, telemetry_test.go, types.go, control.go, http.go.
- [x] RED tests: active binding + explicit rights, epoch, duplicates, invalid fields, retention, policy wakeup.
- [x] Implement typed Wi-Fi/cell records; separate atomic daily storage, bounded batches; retention and read/list/delete.
- [x] Add authenticated /mdm/v1/telemetry and policy in sync response, metadata labels.
- [x] Linux race tests and vet; record evidence.

## Task 2: Android consent / queue / transport policy
Files: MdmTelemetry.kt, MdmTelemetryStore.kt, MdmTransport.kt, MdmController.kt, MdmModels.kt, mobile/mdm_channel.go; corresponding instrumentation/Go tests.
- [x] RED tests: default off, consent/active gates, pause/revoke cleanup, 24h/50MiB bounds, idempotent acknowledgement.
- [x] Implement encrypted queue independent of control; generation guarded upload; physical policy filters and cancellation.
- [x] Verify tests on Linux and dedicated Redmi.

## Task 3: Structured radio sampling and phone UI
Files: MdmRadioSampler.kt, MdmTelemetryService.kt, MdmTelemetryActivity.kt, MdmEnrollActivity.kt, AndroidManifest.xml.
- [x] Policy consent/lifecycle gates and queue removal verified on API30; foreground collection verified on API31.
- [ ] Full collector process-death and OS-permission revocation instrumentation is a follow-up acceptance matrix.
- [x] Implement location foreground service, 30-second typed samples and age/permission status.
- [x] Add clear consent / ordinary collection / manual tracking controls, delivery status and last upload.
- [x] Build APK + instrumentation + lint; device tests with permission denial/grant.

## Task 4: Admin integration and deployment bootstrap
Files: internal/admin/mdm.go, mdm.html, web.go, web.html; cmd/server/public.go, main.go; relevant tests.
- [x] RED admin auth/CSRF and public route tests.
- [x] Wire MDM store and public endpoint separately from VPN; admin invitation and binding list.
- [x] Add device label, policy editor, latest measurements/history, retention and deletion.
- [x] Test authenticated rendering and access boundaries; Linux server regression.

## Task 5: End-to-end acceptance
- [x] Review changes and resolve important findings.
- [x] Deploy verified server and APK; preserve all existing VPN profiles/configuration.
- [x] Dedicated Redmi: TLS enrollment/upload/retry/pause, Wi-Fi-only blocks delivery, queue drains on Wi-Fi return.
- [ ] Longer process-death/OEM/background-location runtime matrix and API34/35 acceptance remain separate follow-up checks.
- [x] User Note12Pro: install, expose controls; user enables consent; verify actual records in server UI. Never silently enroll or opt the user in.
- [x] Document build/version, server link, verified checks and remaining platform limitations.

Ruling: implement telemetry independently before finishing core tasks 4–5. It uses the existing identity/lifecycle but does not require remote configuration execution; this delivers the user's requested end-to-end slice without enabling unfinished control actions.

Acceptance evidence and remaining platform limits: docs/mdm-radio-telemetry.md.
