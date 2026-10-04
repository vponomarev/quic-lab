# VLESS settings and Phase 1 closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the approved single-inbound VLESS settings extension, then complete the outstanding Phase 1 acceptance gates.
**Architecture:** Existing admin/device store remains authoritative. A revisioned VLESS apply coordinator synchronizes worker settings and a runtime managed SNI route overlay; manual routes remain separate. Client export/import carries the selected short ID and SpiderX without silently discarding them.
**Tech Stack:** Existing Go/Xray, Kotlin/Android, HTML/CSS/JS, Linux/systemd; no new framework.
**Spec:** [Approved design](../specs/2026-10-04-vless-settings-phase1-closure-design.md).
**Execution:** Inline implementation; independent final review. Scope/design approved by the user on 2026-10-04. Detailed implementation plan awaits review.

## Global Constraints

- One VLESS inbound, TCP/RAW, TLS or REALITY. No simultaneous inbounds, native Mux switch, subscriptions, WS/XHTTP or Windows.
- Keep existing default fingerprint, first short ID and SpiderX `/` behavior for existing profiles.
- Preserve users/devices/revocations across failed apply and restart. Never restore an entire identity-state backup.
- Keep optional web TLS 1.0, VPN mTLS TLS 1.3, loopback PROXY backend and separate AWG port.
- Run Go tests/binaries and server/load/browser checks on 192.168.5.214 in the isolated checkout; Windows is editing/build/ADB only.
- Capacity limit >=30; 31st admission is rejected only when a test fixture explicitly sets limit 30.
- Required physical and 8-hour tests are not replaced by smoke tests. Publication/deployment already authorized by the user.

## Review Focus

- Crash after worker apply but before route commit: recover deterministically, never export an unconfirmed profile (Task 3).
- Concurrent revocation during settings rollback: revoked device stays revoked (Task 3).
- Preexisting manual route resembles managed VLESS route: reject ambiguous ownership and preserve manual configuration (Task 2).
- Encoded SpiderX path/query and legacy empty fields: round-trip without corruption or changing defaults (Task 1).
- Failed target check, stale form or key regeneration: retain draft, no silent credential rotation (Task 4).

## Task 0 — Publish the verified reconnect fix

Files: android/app/build.gradle.kts; docs/phase1-progress.md.
- [x] Root cause fixed and independently reviewed in 7da42c3. Linux mobile suite, focused race tests, Android build/lint and 40-second physical outage recovery passed.
- [x] Bump client to 0.8.0-pre.10 / code 30 and rebuild APK/lint using the already rebuilt native library.
- [x] Atomically replace the public APK with a backup and verify served SHA256/version. Record exact commit and artifact digest; do not claim Phase 1 complete.

## Task 1 — REALITY export and client parameters

Files: internal/vlessserver/config.go, export.go and their tests; internal/vless/config.go, import.go and tests; mobile/vless_import.go, gateway.go, gateway_vless.go and tests; android/app/src/main/java/ru/vpnc/quiclab/VlessImport.kt and existing profile persistence adapters; matching Android import tests.
Interfaces: server Config gains RealityExportShortID string and RealitySpiderX string (JSON reality_export_short_id/reality_spider_x); client vless.Config gains SpiderX string. Extend the existing mobile/profile JSON contract with vless_spider_x. Empty selected ID uses first allowed ID; empty SpiderX means `/`.
- [ ] Add TestRealityExportSelectionAndSpiderXRoundTrip: legacy defaults, selected allowed ID, unknown ID rejected, path/query/percent-encoding preserved by URI and supported JSON import; no private key in export.
- [ ] Run focused vless/vlessserver/mobile import tests on Linux; confirm new contract fails before implementation.
- [ ] Implement normalization/validation and carry SpiderX through all persistence/native layers. Accept absolute-path references only; reject fragment/control characters and scheme/host substitutions. Keep existing security-mode validation.
- [ ] Repeat focused Go tests and Android import/persistence instrumentation. Build native AAR when Go changes affect the client.
- [ ] Commit the independently verified contract change.

## Task 2 — Runtime managed SNI route overlay

Files: cmd/server/sni.go, sni_routes.go, main.go and route tests; new cmd/server/vless_routes.go and test; scripts/install-server.py and installer tests.
Interfaces: managedVLESSRoutes(c vlessserver.Config) []sniRoute; managedRouteController.Validate(c vlessserver.Config) error; managedRouteController.Apply(ctx context.Context, c vlessserver.Config, revision uint64) error. Controller publishes immutable effective-route snapshots atomically; new handshakes read the snapshot, established streams retain their backend.
- [ ] Add TestManagedVLESSRouteOverlay: TLS selected SNI, all REALITY accepted names, manual route preservation, case-insensitive local/manual collisions, loopback/PROXY requirement, self-routing rejection, ambiguous legacy ownership rejected.
- [ ] Run Linux cmd/server tests to confirm the new route contract is missing.
- [ ] Separate manual routes from the uniquely identifiable legacy managed route; use durable VLESS config to construct the managed overlay at startup. Installer uses the same ownership rules and does not recreate stale routes. Wire the controller without granting the web form arbitrary route editing.
- [ ] Run cmd/server race tests and Python installer tests; verify old/new handshake snapshots and unchanged web/mTLS routing.
- [ ] Commit.

## Task 3 — Revisioned apply and recovery

Files: internal/admin/vless.go, store.go, new vless_apply.go and tests; internal/vlessserver control/worker preflight as required; cmd/server/main.go integration.
Interfaces: admin.VLESSRouteController exposes Validate(vlessserver.Config) error and Apply(context.Context,vlessserver.Config,uint64) error. Store.SetVLESSRouteController(controller VLESSRouteController). Store.ApplyVLESSConfig(ctx context.Context,c vlessserver.Config,expectedRevision uint64) error. Existing UpdateVLESSConfig delegates for internal callers. Durable transaction records previous/candidate configs, revision and stage; it contains no copied user/device state.
- [ ] Add TestVLESSApplyRecovery with injected failures before/after durable prepare, worker ack, route publish and final commit; include stale revision, worker timeout, route rejection and recovery failure.
- [ ] Add TestVLESSRollbackPreservesConcurrentRevocation and TestVLESSExportsRequireCommittedRevision. Verify pending/recovery-required state cannot export candidate credentials or admit VLESS with ambiguous settings.
- [ ] Run focused admin tests and confirm missing coordination fails.
- [ ] Serialize configuration transactions while allowing revocation updates. Preflight certificate/key match, validity and SNI; candidate worker snapshot always derives from current identities. Require worker and routes to acknowledge the same revision, then commit. Restore previous configuration at a new revision on failure, preserving current identities. Reconcile durable pending state before accepting VLESS on startup.
- [ ] Run admin/vlessserver/server suites with race; verify unrelated services remain usable and startup cannot publish contradictory routes/settings.
- [ ] Commit.

## Task 4 — Usable VLESS settings editor

Files: internal/admin/vless_web.go, web.go, ui.css; new internal/admin/vless_tools.go and tests; browser verification script on Linux.
Interfaces: authenticated GET /vless, POST /vless/config with expected_revision and explicit key-change acknowledgement; POST /vless/generate-key, /vless/generate-short-id, /vless/check-target. Target-check response is structured reachability/TLS/name diagnostics, not a guarantee of protocol availability.
- [ ] Add form/handler tests for authorization/CSRF, blank key preserves existing key, generated key is draft-only, stale revision retains form values, invalid field errors, no secret in logs/status/export, target timeout <=10 seconds and bounded concurrent probes.
- [ ] Confirm tests fail for missing endpoints/validation before implementation.
- [ ] Implement stable tabs Connection/Security/Advanced, conditional TLS/REALITY fields, fingerprint selector, key/ID generation, public key copy, exported-ID selector, SpiderX and applied revision/error status. Keep the already shipped entry-point link. Secret-bearing responses use no-store; generate key via crypto/ecdh X25519 and IDs via crypto/rand. Check only the supplied target/SNI, without redirects/scanning.
- [ ] Browser checks at Full HD and mobile width: no overflow, stable tabs, drafts survive errors, private key never prefilled, key generation requires save and acknowledgement. Repeat server tests and review.
- [ ] Commit.

## Task 5 — Close B4, E2 and E5

Files: existing Phase 1 plans 02/05, docs/phase1-progress.md; scripts/phase1-acceptance.py and docs/phase1-acceptance.md as specified in E5; physical Android test classes from that plan.
Interfaces: retain E5 report schema and isolated Linux fault/load harness. Record exact commit/APK/device/environment plus individual pass/fail/not-run outcomes.
- [ ] Complete VLESS TLS/REALITY live checks with our client and HAPP on isolated credentials; restore production TLS settings afterward.
- [ ] Execute B4 Wi-Fi/LTE prepared-backup <=1s and cold allowed LTE <=10s, carousel, two exits, AWG and E2 comparison/Echo-through-VPN checks.
- [ ] Execute E5 faults: packet-size blackhole, 1/2 MiB freeze, loss/reorder, idle/half-close, budget exhaustion, DNS/IPv6 exclusions, revocation/update and server restart. Fix defects through red/green regressions.
- [ ] Load test 15 progressing devices and at least 30 allowed devices with explicit configured limit, using the agreed 100 Mbit/s target. Document bottlenecks/resources and avoid host-global shaping/firewall edits.
- [ ] Run >=8-hour screen-off soak with TCP/UDP, memory/socket trends and battery measurements; retain logs across interruption. Do not mark unrun gates passed.
- [ ] Run final Go suite/vet/race, Android build/lint/instrumentation and independent code review. Update task states only from evidence.
- [ ] Produce versioned artifacts and release notes, publish GitHub release, deploy server and APK, check served versions/hashes and services. Keep accepted limitations explicit.

## Self-review

All approved scope maps to Tasks 1–4; remaining acceptance maps to Task 5. Each review-focus failure has a named test. Task 0 is independently releasable and already functionally validated. No phase-two feature is a closure prerequisite. User approval of this detailed plan is the remaining Superpowers handoff before implementing Tasks 1–4.
