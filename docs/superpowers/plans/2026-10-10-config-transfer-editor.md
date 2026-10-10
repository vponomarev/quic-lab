# Configuration transfer and temporary WEB editor — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement task-by-task. Preserve native execution in this session; one final independent review. Steps use checkboxes for tracking.

**Goal:** Переносить конфигурацию целиком/частями с предпросмотром и редактировать телефон через ограниченную браузерную сессию без постоянной MDM-привязки.

**Architecture:** Общий переносимый документ использует проверенную schema 1 MDM, но не содержит регистрацию устройства, права и служебные очереди. Подготовка кандидата отделена от атомарного применения. Временная сессия использует существующий WEB-редактор и независимый HTTPS-канал, собственные краткоживущие полномочия и локальную проверку на телефоне; постоянная MDM-привязка не создаётся.

**Tech Stack:** Go, Kotlin/Android, existing gomobile bridge, HTML/JS, existing filippo.io/age for password-protected files.

**Spec:** `docs/superpowers/specs/2026-10-04-client-management-design.md`, sections 7–8, 9.3, 10–12; current configuration/report contracts and accepted amendments through 0.9.1.

Status: prepared 2026-10-10; implementation not started. This plan supplies implementation decisions for the already accepted product design. Execution starts after review of this document.

## Global constraints

- Перенос: весь конфиг, выбранные профили, правила маршрутизации/выбор приложений, диагностика. Новую доменную маршрутизацию не добавлять.
- Default export contains no credentials. Explicit export with VPN secrets is password encrypted; MDM binding, management credentials, consents, enrollment/update tokens, diagnostic authentication, telemetry queues and traffic counters never travel in this format.
- New registration/identity is never synthesized silently. Explicit secret export may copy existing VPN credentials; warn about concurrent use, especially AWG. This does not create an independently revocable device.
- Import modes: add new, update explicitly mapped profiles, replace all with separate confirmation. Missing secrets preserve existing credentials only for explicitly matched profiles; new incomplete profiles remain disabled and visibly require credentials.
- Preview and full validation precede Apply. No partial commit, silent conflict overwrite or implicit VPN start. A running VPN may restart for changed settings; explicit user Stop during the operation wins.
- MDM config ownership remains authoritative. Local import cannot bypass active management; an authorized MDM editor saves desired state first, then reports delivery/applied/error separately.
- Temporary editor: default 30 minutes; choices 15/30/60 minutes. Extension requires renewed phone approval. Pause/delete MDM revokes its sessions locally immediately.
- No permanent phone web server, shell, arbitrary file access, group edits, Remote Helper or new telemetry consent in this iteration.
- Run Go binaries/tests on Linux 192.168.5.214. Windows is for Android builds/ADB. Never claim Android 14/15, reboot/process recovery, or browser tests passed without evidence.

## Review focus

1. Same profile name on two phones must not match identity automatically (Task 1).
2. Preview becomes stale while MDM or local settings change; Apply must reject it (Tasks 2, 4).
3. Truncated encrypted file or excessive scrypt work factor must fail before any mutation (Task 1).
4. Pairing twice, revoked consent, clock changes and server restart must not revive temporary access (Tasks 3–4).
5. Imported configuration must not reset LTE consumption or suppress manual Stop (Tasks 2, 5).

## File boundaries

- New `internal/configtransfer/{document,merge,crypto}.go` and tests: portable format, deterministic candidate merge, age envelope. Reuse `internal/mdm.ValidateDocument`, not a second validator.
- New `mobile/config_transfer.go`: bounded gomobile string/byte wrappers.
- New Android `ConfigurationTransfer.kt`, `ConfigurationTransferActivity.kt`, `LocalConfigurationApply.kt`: SAF file IO, preview, identity selection and local atomic application.
- Modify `MdmConfiguration.kt`, `MdmConfigurationStore.kt`, `MdmApplyCoordinator.kt` only at explicit shared snapshot/compile/commit boundaries; retain MDM authorization.
- New `internal/editor/{store,http}.go` and tests: temporary sessions, one-time pairing and bounded document exchange.
- New Android `TemporaryEditorActivity.kt`, `TemporaryEditorRuntime.kt`: consent, countdown, independent channel and local revocation.
- Modify `internal/admin/mdm_editor.{go,html,js}`, add `internal/admin/editor.go`: shared form with different authenticated endpoints, never transfer MDM API rights to a temporary token.
- Modify `AppSettingsActivity.kt`, `ProfileScanActivity.kt`, manifest: configuration transfer and temporary editor entry points.
- Modify backup page script delivery to satisfy existing CSP without unsafe-inline.

## Task 1 — Portable format, merge and encryption

**Interfaces** (`internal/configtransfer`): `Export(document []byte, selection Selection, includeSecrets bool) ([]byte,error)`; `Prepare(base, bundle []byte, options ImportOptions) (Preview,error)`; `Encrypt(raw,password []byte) ([]byte,error)`; `Decrypt(raw,password []byte) ([]byte,error)`.

`Selection` selects profile IDs and named sections (profiles, routing, network, diagnostics). `ImportOptions` includes mode (add/update/replace), explicit source-to-target profile mapping and fresh IDs supplied by caller. `Preview` contains validated full candidate and redacted change descriptions, never raw secret values. Bundle envelope: format="quic-lab-config", version=1, selected sections, credentials="omitted" or "included", payload. Payload uses existing schema field semantics; validate complete candidate after merging. Maximum decoded payload 1 MiB; encrypted input 2 MiB. No archive/compression/path extraction.

- [ ] Write failing tests: add remaps all profile references; update requires explicit mapping; duplicate names do not match; routing-only does not alter credentials; replace detects missing references; omitted secrets do not erase existing ones; new secretless profiles disabled; unknown/duplicate/null fields, version and oversize input rejected.
- [ ] Run `go test ./internal/configtransfer -count=1` on Linux; record expected failure before implementation.
- [ ] Implement envelope and merge. Selection omits nonselected sections entirely. Partial routing requires referenced profiles to be mapped or selected; never silently routes to another exit. Preserve base enabled/current choice when adding profiles.
- [ ] Add age passphrase encryption using existing library. Use a bounded supported scrypt factor (16 for export, maximum 18 for import), one worker, no secrets in errors/logs; verify entire stream before returning plaintext. Password input and KDF run off Android main thread.
- [ ] Test wrong password, altered/truncated final chunk, oversize plaintext and excessive work factor; all fail without returning partial plaintext. Verify randomized ciphertext and round trip.
- [ ] Add gomobile wrappers and tests, rebuild AAR using established Linux build workflow; run `go test ./internal/configtransfer ./mobile -count=1`.
- [ ] Commit self-contained format implementation and format documentation.

## Task 2 — Android transfer, preview and atomic local apply

**Interfaces:** `ConfigurationTransfer.prepare(context, bundle, options): TransferPreview`; `LocalConfigurationApply.apply(preview): TransferResult`. Preview holds candidate, current configuration generation, management generation and redacted changes. Export obtains secrets only after explicit selection; portable identity fields are certificate/key/AWG/VLESS, not the full private native bundle.

- [ ] Add failing instrumentation tests in `ConfigurationTransferTest.kt`: stale preview rejected, no MDM binding/consent transferred, explicit mapping retains private update metadata, missing apps displayed, no mutation on canceled import.
- [ ] Add a local commit operation in `MdmConfigurationStore` using its existing encrypted AtomicFile. Expose compile/snapshot reuse without weakening `MdmConfiguration.apply` authorization. Store local operation/rollback in the local encrypted configuration state, not a fake MDM record.
- [ ] Implement serialized apply: validate and compare generations, record rollback and wasRunning/userStop, stop if necessary, atomically publish candidate, restart only if still authorized and user did not Stop. Recover interrupted publication without resetting counters. Keep latest rollback until next successful application; explicit restore checks current authority.
- [ ] Implement settings screen with SAF Open/CreateDocument, section selection, export-with-secrets opt-in/password, three import modes, mapping, preview and Apply. Plain import of identity-bearing data rejected; credentials require encrypted format. Secret values never rendered in diff.
- [ ] Active MDM ownership blocks direct local writes and links to the MDM editor. No alternate route through import. Local export remains explicit and never exports management authority.
- [ ] Run instrumentation for import/cancel/conflict/rollback/manualStop/LTE counter continuity plus existing MDM configuration/apply suites; Gradle assembleDebug/assembleDebugAndroidTest/lintDebug.
- [ ] Commit Android transfer and acceptance evidence.

## Task 3 — Temporary editor broker

**Interfaces:** new editor Store methods `Create`, `Pair`, `Read`, `Submit`, `Acknowledge`, `Revoke`, with injected clock; all operations carry session ID and distinct browser or device credential. Session contains expiry, paired state, base configuration generation, desired revision and sanitized report. Server restart invalidates all sessions (ephemeral storage).

- [ ] Add failing Go tests for one-time pairing, browser/device credential separation, session isolation, expiry, revocation, replay and concurrent submit conflicts.
- [ ] Create from authenticated administrator WEB session with CSRF, returning QR/link containing one-time pairing token. Pairing link cannot read/edit configuration by itself. Token validity 5 minutes; session lifetime begins after phone consent. Use random 256-bit tokens, hashes in store, constant-time checks, maximum 100 sessions and one pending candidate per session, existing document bounds.
- [ ] Implement HTTPS API `/api/editor/v1/` with independent short-lived device authentication and strict request methods/body limits. Browser mutations remain administrator authenticated + CSRF and tied to initiating session. QR/link tokens must not enter access logs; use URL fragment for deep-link token and scrub browser history.
- [ ] Reuse physically bound `MDMChannel` through explicitly allowlisted editor operations; do not make its destination/path validation arbitrary. Verify trusted TLS, no redirects and cancellation.
- [ ] No permanent desired configuration for unbound phones: latest valid phone report is authoritative. UI shows waiting/applied/error; offline commands expire with session. Audit IDs/action/result only. Erase payload on close/expiry/restart; metadata follows existing audit retention.
- [ ] Run `go test -race ./internal/editor ./internal/admin ./mobile`; commit broker and protocol contract.

## Task 4 — Phone consent and shared WEB form

- [ ] Add failing Android session tests: consent decline, expiry while offline, local revoke without server, monotonic countdown, process death ends temporary session, no persistent binding, no VPN start due to pairing.
- [ ] Add Settings → temporary editor and QR/text pairing route. Show server, edit scope, duration, notice that applying may restart VPN; explicit consent required. Show remaining time and Stop control; stopping cancels pending application before commit.
- [ ] Unmanaged sessions use Task 2 local apply and generation checks. Active MDM sessions require server authorization tied to binding/epoch and use existing desired/applied path. Reject unrelated-server pairing while MDM owns config. Pause/delete invalidates authorization on phone even if broker cannot be reached.
- [ ] Reuse form rendering/validation in `mdm_editor.js` through a small endpoint adapter. Temporary sessions cannot call permanent MDM actions. Add import/export controls to the form, using Task 1 candidate generation and server revision CAS. Server-side reports have no phone secrets; do not pretend they can produce a complete secret export. Explicit secret export is performed on phone in this iteration.
- [ ] Verify two browsers conflict, canceled session, pending offline changes expiring, phone local edit vs browser edit, MDM permission revocation and long/absent application names. Browser never claims applied before acknowledgement.
- [ ] Run server tests + Android session/apply suites, browser end-to-end and responsive checks; commit editor and evidence.

## Task 5 — Stable release tail and acceptance

- [ ] Extract backup inline polling to same-origin external JS, register correct Content-Type, preserve CSP. Verify browser job status updates without manual refresh and encrypted backup download still works.
- [ ] On dedicated authorized test phone verify reboot and allowed OS process termination with MDM active, paused and temporary editor states. Record OEM settings; lack of OS permission is blocked evidence, not a pass. Android 14/15 remain pending until device available.
- [ ] End-to-end on two phones: export/import without secrets, explicit encrypted transfer using disposable VPN credentials, no identities/permissions cloned unexpectedly; configuration edits in browser with VPN off/on, rollback, manual Stop and LTE counter continuity.
- [ ] Run Go regression on Linux, focused race/vet, Android lint/instrumentation and fresh independent review. Record actual outcomes in `docs/configuration-transfer-acceptance.md`.
- [ ] Refresh stale roadmap/schema status to distinguish shipped 0.9.1 from this iteration. Release candidate only after required acceptance; stable publication/deployment follow existing backup/checksum/rollback procedure when release is requested. No production configuration mutations during development.

## Self-review

Coverage: spec 7 → Tasks 3–4; spec 8 → Tasks 1–2/4; session TTL/revocation → Tasks 3–4; audit/permissions → Tasks 1–4; verification tail → Task 5. Remote Helper, direct Wi-Fi hosting, user/group portal are intentionally later iterations. No new product consent is inferred from approval of this plan. Largest implementation risk is local apply extraction: existing permanent MDM authorization and recovery tests must stay green before editor integration.
