# Phase one execution progress

Execution: inline, selected by owner; no subagents unless requested. Plan: docs/superpowers/plans/2026-09-30-phase1-00-roadmap.md.

## A1 — complete

Portable Go model, gomobile validator, and Android atomic configuration projection implemented. Stable legacy IDs, DNS/routes/apps and encrypted identity references are preserved. Ordinary QUIC/HTTPS/AWG remain standalone; existing max_availability becomes demux. Profile import publishes the snapshot and rolls back a newly added profile and identity if writing fails.

Verification: Linux `go test ./internal/vpnmodel ./mobile -count=1 -timeout=120s` and `go vet ./internal/vpnmodel` passed. Android debug and test APK builds plus lint passed. Connected Android device: `VpnConfigurationTest`, OK (7 tests). Observed RED before implementation for migration and import; final GREEN includes forced write failure, rollback, idempotence, settings and identity preservation, ordinary transport compatibility, and empty drafts. Tests use isolated preferences/files and synthetic keys; no live tunnel was started.

## Rulings and transition constraints

- Disabled blank drafts are valid; activation still requires endpoint validation.
- Legacy SharedPreferences remain authoritative during A1 and are retained unchanged as the migration fallback. The versioned JSON is an atomic projection refreshed on load/import; encrypted identities stay in their existing files. Lifecycle consumers arrive in A2. Before introducing new model-only editing, replace this projection with an explicit ownership/version transition so local edits cannot be overwritten.
- Existing ordinary QUIC/HTTPS remain standalone; only max_availability maps to demux. No automatic grouping by hostname.
- Installed task-start helper requires numeric headings but plans use A1 etc.; use equivalent exact task extraction with BASE and private ledger.

Next: C1, shared LTE accounting epoch and ledger. Remaining 14 tasks are not complete. This is a development build, not a release or a verified whole-MVP implementation.

Private ledger: .superpowers/sdd/2026-09-30-phase1-01-model-lifecycle/progress.md.

## A2 — complete

Added synchronized Go lifecycle states and Android session-owning VpnExitController. Restart invalidates the prior generation and closes only that exit. Incompatible exits reject subsequent events. Single and multiple service paths use controller ownership; single-session callbacks capture their original controller so a new whole-service run cannot accept old callbacks with coincident generation numbers. Main-looper service commands remain serialized. Stop-all invalidates callbacks and attempts every close even if one fails; service cleanup still releases TUN. First explicit Stop shows a one-time notice, stored locally.

Verified: Linux go test -race ./internal/vpnmodel -count=1 and go vet ./internal/vpnmodel passed. Android assembleDebug, assembleDebugAndroidTest, lintDebug passed. Device VpnExitLifecycleTest + VpnConfigurationTest: OK (11 tests). Lifecycle cases cover independent restart, stale callbacks, incompatibility, startup failure, and close failure. These tests use controlled session resources; real multi-tunnel traffic/route blocking is the A3 live gate, not claimed here.

Test history: initial Go run failed on missing Runtime/Event APIs. Android's first three lifecycle checks were first executed after implementation (workflow deviation); the added close-failure test was observed RED (only one of two sessions closed), then fixed and observed GREEN. Do not describe all Android lifecycle tests as observed red-to-green.

## A3 — complete

Implemented explicit BlockExit, preserving rule ownership while canceling this exit's flows; duplicate SetGateway for the same session is now idempotent. Android routes validate stable exit IDs and pause through BlockExit. Existing routing policy needs no algorithm change: priority, unknown UID and unmatched-direct cases already have regression coverage. Multiple+bond remains disallowed until B4.

Observed behavioral RED: duplicate attachment canceled the existing handler; stub BlockExit did not cancel paused flows. Final Linux checks passed: go test ./internal/routing ./mobile -count=1 -timeout=120s; targeted routing/multiple tests with -race; go vet ./internal/routing ./mobile. AAR/APK/lint and instrumentation plus both probe APK builds passed.

MultipleLiveTest now accepts an optional synthetic profile_file and restores the configuration snapshot after test cleanup. Dedicated Linux server/responders were prepared on isolated ports without changing running services. Phone was initially disconnected; the device gate was completed after reconnection, as recorded below.

A3 final device gate (2026-09-30): MultipleLiveTest ran with multiple_live=true, probe_host=192.168.5.214 and an isolated synthetic profile. OK (1 test), 34.341 s; not a skipped/default invocation. Real TCP/UDP over QUIC/HTTPS, app UID and subnet priority, unmatched direct traffic, pause-without-fallback, other-exit survival, resume/reorder and single QUIC/HTTPS regression passed. The test restored profile metadata; no VPN service remained afterwards. Temporary server/responders stopped and synthetic profile removed from Android external storage. Final Linux routing/mobile suite passed again. Local evidence: artifacts/a3-live-result.log and artifacts/a3-live-report.txt (not committed; diagnostics can contain user network metadata).

Scope: two logical tunnels to one isolated Linux server over the available network. This does not certify two physical servers, Wi-Fi/LTE migration, demux session continuity or long-duration soak; those belong to later gates.

## B1 — complete with C1/C2 integration gate

Added Options with 120-second default disconnect grace, bounded pending records (1024 per session / 64 per flow), and configurable server grace (0 defaults to 120; accepted explicit range 1–3600 seconds). Reliable record ageing pauses while all paths are absent and resumes on reconnect; datagrams retain their short expiry. PathInfo carries independent path/profile/network identity and generation; stale replacement/removal callbacks cannot remove the current path. Scheduler, LTE classification and stats support named paths. Receive queues remain bounded.

Flow-ID exhaustion enters draining. New TCP/UDP flows can rotate to a new session without canceling the Gateway root/TUN or existing TCP sockets. Retirees are capped at three plus current and have a bounded drain timeout. CREATE receives a server-generated fresh secret; expired-token resume is rejected. Wire change bumps bond ALPN to quic-lab-bond/2, requiring matching client/server versions; no production deployment or phone APK installation was performed in B1.

Transitional limitation: automatic draining rotation with a finite legacy LTE budget explicitly returns an error, leaving old flows alive. This prevents budget reset/double allowance. C1/C2 must share accounting across current and retired sessions before lifting this guard; the C2 plan now includes the regression/removal gate. Therefore the complete MVP budget contract is not claimed at B1.

Verification: full Linux suite `go test ./internal/bond ./internal/gateway ./mobile ./cmd/server ./cmd/demux -count=1 -timeout=120s` passed; targeted bond/retention/mobile integration tests with race passed; vet passed. Final AAR/APK and lint build passed (artifacts/b1-final-build.log). Tests exercise 119/120-second boundaries using an adjusted disconnect timestamp, not a two-minute wall-clock soak; real pending payload delivery after a scaled outage and real TCP preservation across session rotation passed. Existing slow-reader regression still passes. No Android device test was required by B1; B4/E5 remain responsible for device/network acceptance.

## B2 — complete

Added the bounded HTTPS WebSocket bond adapter and /tunnel/bond with quic-lab-bond-v1 negotiation, 4096-byte JSON handshake and binary records up to 1080 bytes. Both transports use the same mTLS-authenticated registry. Server root context keeps the session alive independently of its initial HTTPS request. Welcome precedes path attachment to prevent binary traffic racing the handshake. Server and optional demux HTTPS listener are wired; QUIC ALPN was already versioned in B1.

Linux integration tests preserve one real TCP socket and the same UDP mapping across QUIC-to-HTTPS and HTTPS-to-QUIC replacement. A client without its certificate cannot join with a borrowed token. Framing, oversize rejection, cancellation and bounded nonblocking queue tests pass. The blocked writer is injected deterministically at the socket boundary; this is not a kernel-level stalled-link or physical Wi-Fi/LTE acceptance test.

Verification: go test ./internal/bondhttps ./internal/bondquic ./internal/gateway ./mobile ./cmd/demux ./cmd/server -count=1 -timeout=120s passed; targeted race tests and package vet passed. Android AAR/APK/lint succeeded (artifacts/b2-build.log). No APK installation or production deployment. Initial cross-transport tests failed for missing APIs, and HTTPS-first failed on the former QUIC-only guard before implementation; additional negative coverage was first run green.
## B3 — complete (policy and telemetry; execution in B4)

Added portable per-exit profile/network decisions, generation-safe dial feedback, all-auto-networks-before-reserve ordering, disabled-profile exclusion, optional rare reserve probes, retry backoff and return hysteresis. Pending user data and outstanding sized probes have independent stall timers; tiny ping success cannot reset either. Three recovered stalled connections within ten minutes recommend carousel without enabling it automatically. Idle traffic does not reset retry backoff.

Bond reports per-generation acknowledged payload, pending bytes, profile/network identity and sized-probe state. Full-record probes are bounded to one outstanding probe per path and echo a working-size payload. Mobile bond diagnostics expose stalled paths. B4 will execute policy commands and schedule probes after shared budget and authorization gates; this is not yet Android automatic profile selection. Probe health concerns the transport path, not final Internet egress.

Verification: Linux go test ./internal/pathpolicy ./internal/bond ./mobile -count=1 -timeout=120s passed; final policy/bond suite and race rerun after the backoff fix passed; vet passed. Android AAR/APK/lint passed (artifacts/b3-build.log). Tests include volume-stall observations, tiny-reply/large-record fault injection, hysteresis, reserve opt-in, independent exits, generation/dial timeout and backoff reset. Regression caught idle time resetting backoff before the fix. No Android runtime/network acceptance claimed; the connected phone was detected but APK was not installed.
## Intermediate release checkpoint — v0.8.0-pre.1

Owner requested publishing and deploying the A/B checkpoint before group C. Android version 0.8.0-pre.1 (21), paired Linux amd64/arm64 server archives and SHA256SUMS. Fresh full Go/race/vet, Android build/lint, installer unit/container checks and live phone/server validations are recorded in docs/release-v0.8.0-pre.1.md. Standard Android suite: 35 passed and 16 explicit prerequisites/opt-ins skipped; additional live multiple-exit, bond migration, authenticated/public three-transport Echo and per-transport screen policy tests passed. Three stale Android test assumptions were reproduced and corrected; application behavior was not changed to satisfy them.

quic-demo.vpnc.ru main server and public APK updated with root-only rollback backups. Initial state backup noticed a live socket/directory change; original service was restarted before retrying. Successful retry archived persistent identity/configuration data, installed the verified server, published APK atomically and checked service/public download. Public APK SHA256 matches the release. Existing AWG/transit workers remain running. No group C work has started; B4 and later acceptance remain pending.

## Post-release Wi-Fi return investigation (2026-10-01)

On the owner's Android/MIUI device, ordinary QUIC with all-app routing (mode 0) preserves the gateway session but application TCP sockets abort when Wi-Fi becomes the physical default network, before QUIC migrates. The same persistent-TCP reproduction fails on APK v0.7.4 with the current profile/server, so this is not established as a client-version regression. The owner confirmed the previously working configuration used selected applications (mode 1); the private pre-release preferences backup agrees. Selected-app routing passed the same automated Wi-Fi/LTE/Wi-Fi and short screen-off test on pre.1.

A diagnostic all-app-except-Android-UID1000 run passed twice. This narrows the interaction to capturing system-UID traffic on this device; it does not identify the exact vendor socket-abort implementation. Excluding system services changes routing semantics and is not silently enabled. No server/client transport fix or pre.2 release is claimed. Group C remains paused. Local ignored diagnostic logs contain private device metadata and must not be published.

A separate Linux fault-injection test exposes bond UDP expiration before a delayed hedge. It is tracked independently; the owner's failing profile did not enable bond.

Owner decision (2026-10-01): confirmed Brawl Stars now survives Wi-Fi return with selected applications. All-app routing on the tested Android/MIUI device is an accepted phase-one limitation. Remediation is deferred beyond phase one as [POST-P1-01](post-phase1-backlog.md), not a phase-one acceptance blocker. This deferral does not cover the separate bond UDP hedge defect.

## Morning diagnosis — 2026-10-01

Read-only inspection; phone VPN/settings unchanged. Android historical process exit reports 01:28:35 local, reason 13 OTHER KILLS BY SYSTEM, description AutoLockOffClean. This confirms system termination of the app, not a demonstrated transport failure.

Owner reports car hotspot Wi-Fi available while VPN remains on LTE until a manual restart. Exact earlier client evidence is unavailable: the 400-entry journal starts at 07:48:37 local. Server journal records QUIC open at 06:55:07 and close/reopen at 07:39:03/05 (owner estimated 07:29). These server events alone do not establish physical path selection.

Later retained evidence: at 07:59:07 The hotspot supplies DHCP vendorInfo ANDROID_METERED; client sees Wi-Fi available and validated but no standby preparation/migration until this Network disappears at 07:59:20. At 08:00:54 another Wi-Fi Network becomes available; standby succeeds, validation follows, and migration completes at 08:00:59. Reserve prefs are empty, so metered_wifi=false default applies. VpnReserveSettings.permits and VpnSession.tick exclude metered Wi-Fi from standby probing and preference migration while LTE works; initial connection does not apply that reserve filter. This is strong evidence for the earlier car behavior, but the exact earlier interval cannot be proved from overwritten logs. Same SSID alone is not used as path identity: key is Android networkHandle.

Existing user option: РЕЗЕРВНАЯ СЕТЬ VPN → Разрешить проверки лимитного Wi-Fi. Enabling it permits probing and automatic preference subject to validation/health/RTT; may consume hotspot SIM traffic. Not enabled during diagnosis. Delayed Internet startup remains unproven; retry backoff is bounded at 30s, not a permanent disable on first timeout. Diagnostic limitation: flow events evict control/lifecycle events from the 400-entry journal; investigation needs longer-lived control history. Separate from accepted all-app/UID1000 limitation.

## Metered Wi-Fi fix — v0.8.0-pre.2

Preferred Wi-Fi eligibility is now separate from optional background reserve permissions while active on cellular, including ordinary QUIC/HTTPS/AWG. Existing validation, successful QUIC/HTTPS probes, dwell/backoff and latency guards remain. User settings labels explain the distinction. Regression observed RED on pre.1 and GREEN on pre.2 with one persistent TCP socket. Build/lint passed; 14 core Android cases passed with one explicit live skip, then opt-in reserve tests passed separately for all three transports on metered Wi-Fi. Linux mobile standby/migration race tests passed, including recovery after initially lost replies. Server binary/protocol unchanged. Details: [pre.2 release](release-v0.8.0-pre.2.md). Group C has not started.

## C1 — complete (budget core and run ownership)

Added a shared concurrency-safe trafficbudget ledger: atomic reservations, partial write refund, idempotent commit, saturated uint64 counters, incoming overshoot, class totals and copied snapshots. Mobile wrapper validates epoch/nonnegative limit. LabVpnService owns one VpnBudgetRun until whole-service shutdown; restart of an exit must reuse its object. Android lifecycle test verifies retained object and new epoch after Stop/Start.

C2 must attach socket meters, configure the common limit and enforce cellular shutdown. C1 initializes an unlimited unmetered owner; it does not yet implement user-facing traffic limiting or replace legacy bond limits. Keeping the bond rotation guard is intentional until C2.

Verification: observed ledger RED on missing API; Linux go test ./internal/trafficbudget ./mobile -count=1 -timeout=120s passed, ledger race/vet passed. Android AAR/APK/lint/test APK passed; VpnBudgetRunTest and VpnExitLifecycleTest: 5 passed. Published pre.2 APK restored after tests. No release/deploy. Account meter moved from 86% used to 87% used across C1; shared rounded measurement, not exact per-task/model billing. Stop after C1 to preserve the owner's reserve.

## C2 — complete (shared socket accounting and LTE shutdown)

One run-owned meter covers QUIC UDP, HTTPS TCP below TLS, and AWG outer encrypted datagrams, including current/draining bond sessions and multiple exits. Both directions contribute to the same limit; overlay metrics are not added to physical totals. Android supplies immutable per-run budget references to reconnecting sessions, blocks new cell bindings/probes when exhausted and keeps Wi-Fi eligible. Settings now have one global LTE limit; migration imports the selected legacy profile limit once. Whole VPN Stop/Start starts a new epoch; exit reconnect does not. Shared meter replaces legacy bond split limits; finite legacy callers retain their rotation guard.

Physical sockets close on exhaustion. QUIC retains its logical session for Wi-Fi migration. AWG queued closure checks bind generation to avoid closing a replacement Wi-Fi socket. A best-effort authenticated bond control record tells the peer to stop LTE over remaining non-cellular paths; older peers may ignore it, without disabling client enforcement. Snapshot reports receive overrun; IP/TCP headers and kernel TCP retries remain outside accounting. When the next write cannot fit, LTE stops without charging unsent bytes. Separate user/control/copy overlay statistics remain diagnostic, not additional billed bytes. C3 service downloads/consent and E1 main-screen telemetry remain future work.

Verification: Linux go test -race ./internal/trafficbudget ./internal/awg ./mobile -count=1 -timeout=120s and vet passed; bond race suite passed with explicit exclusion of TestReturningWifiDoesNotLoseUDPBeforeHedge (pre-existing untracked Wi-Fi experiment, outside C2). Gateway/server/demux checks passed; demux has no tests. Tests cover socket partial writes, incoming overshoot, all-cell closure, unchanged Wi-Fi, QUIC session continuity, AWG traffic/rebind, successful GSO fallback, bond rotation with a finite shared budget, and QUIC/HTTPS fallback accounting. Android final build/lint passed; TrafficBudgetTest, VpnBudgetRunTest and VpnExitLifecycleTest: 6 passed. Android socket test uses loopback with real datagrams, not a physical LTE handover acceptance test. Published pre.2 restored after tests; VPN may need manual activation. No release/deploy.

Account meter moved from 87% used at C2 start to 90% used: approximately 3 percentage points, shared and rounded rather than exact task billing. Stopped at C2 checkpoint with 10% remaining, above the owner's 7% emergency reserve. Next: C3.

## Phase-two scope decision — strict split tunneling (2026-10-01)

Owner assigned POST-P1-02 to MVP phase two and accepted its absence in phase one. Code review found no common TUN flow-owner authorization in ordinary QUIC/HTTPS/AWG; multiple routing has partial checks and a port-53 override. Exploit reproduction on the user's device is pending. This does not waive phase-one no-direct-fallback requirements. Requirements and acceptance checks recorded in docs/post-phase1-backlog.md and the product specification. No runtime changes; phase-one progress remains 8/20, C3 next.

## Phase-one continuation — 2026-10-03 (in progress)

C3 scoped service transfers and C4 DNS policies implemented. Independent review corrections address authenticated redirects, cancel-versus-publication ordering, terminal callback cleanup, bonded DNS network replacement and single-mode traffic metrics. Full Linux mobile/routing/trafficbudget race tests and vet passed; Android application build/lint and test APK compilation passed after binding/API fixes. Android runtime and MultipleLiveTest acceptance remain pending, so C3/C4 are not yet marked complete.

A newly supplied Redmi Note 9 Pro (Android 11/API 30) is ADB-authorized. Its existing pre.2 app data was backed up privately; the C3/C4 development APK installed without clearing data. MIUI rejected installation of the separate instrumentation package; awaiting the owner's on-device permission. This is not a released APK or server deployment.

D1 device persistence, per-device AWG peers, revocation, legacy migration and grouped administration passed Linux race/vet. Review corrections cover crash-stale reload sockets, bounded bond registration callbacks, and durable identity publication with directory fsync. Final reload-stop lifecycle regression and review closeout remain. D3 TLS identity separation and compatibility preflight are under integration; focused real TLS/QUIC/HTTPS tests pass. D2/D4 and final acceptance remain outstanding.

Device follow-up: after enabling USB installation, ServiceTransferTest, DnsRoutingTest and TrafficBudgetTest passed together (7 tests, 5.237 s) on Redmi Note 9 Pro/API30. MultipleLiveTest first failed because the phone had only LTE and could not reach the private test host. After the owner enabled Wi-Fi, the real opt-in test passed (1 test, 35.946 s), including independent exits and single-mode regression. C3 committed dbda3e2; C4 committed 049dc05. D3 full APK/lint/test build passed and ProfileCompatibilityTest passed (5 tests, 0.389 s); review event-emission correction remains pending. D2 Linux broker/admission race/vet and real encrypted AWG/netstack admission regression passed; UI/Android and final review are still pending.

## Verified integration — 2026-10-03

D1–D3 and E3 are complete. Final scoped reviews passed after durable enrollment-retry correction. Linux full repository tests and vet passed; only the pre-existing exploratory TestReturningWifiDoesNotLoseUDPBeforeHedge was explicitly skipped and remains a required separate fix, not a deferred phase-one exemption. All touched security/concurrency packages also passed targeted race runs. Android APK/lint/test builds passed, as did the registration/import/compatibility instrumentation group (10 reported tests; the optional real-server QR import had no argument and was not exercised). No production deployment or release occurred.

These tasks share Store, protocol and server bootstrap files, so their verified implementation is recorded together in one integration commit rather than splitting interdependent uncommitted hunks. Fourteen of twenty tasks are complete; remaining B4, D4, E1, E2, E4 and E5 retain their acceptance gates. Test host and Android are development fixtures; production remains pre.2 APK/pre.1 server. D3 root capabilities forwarding through installer nginx is explicitly an E4 integration requirement.

## D4 — complete (2026-10-03)

Per-device authenticated configuration updates now preserve local routing, selected transport and economy preferences. Enrollment issues a random bearer; the server stores its hash and an encrypted replay outbox bound to the original enrollment secret. Configuration revisions are durable, and disabled devices cannot fetch updates. Direct HTTPS exposes only the required device-config route; nginx forwarding belongs to E4.

Android confirms a redacted diff, commits server fields and credentials in one encrypted AtomicFile bundle, and restarts only the updated exit. Paused exits keep new metadata for resume. The active DNS owner is captured from the running service; changing its DNS requires stopping VPN before apply. Manual identity replacement cannot overwrite an enrolled profile. APK installation is offered only after hash, package and installed-signer checks, with normal Android user confirmation.

Verification: Linux admin/server race and vet PASS; full server tests after root-route integration PASS. Android build/lint PASS, ProfileUpdateTest + ServiceTransferTest + DeviceEnrollmentTest: OK 16 tests (3.836s). Review found an AtomicFile read/write/recovery race; both regressions failed before the repair and pass after it. Live MultipleLiveTest: OK 1 (34.233s), real TCP/UDP and independent exits after service-factory changes. This live test does not claim an authenticated over-network profile update or a real LTE over-budget APK download; those remain acceptance scenarios. Local implementation milestone: 15/20, not a release or complete MVP acceptance.

## E4 — complete (2026-10-03)

The installer preserves configured capabilities/SNI and nginx/direct mode, publishes only the required root control/config routes, and snapshots identities.json plus its previous backup while the old service is stopped. The complete manifest records ownership, permissions and file absence. Backup directories are fsynced before the new process starts. Failed upgrades restore the matching state before restarting the old binary; failed snapshots preserve the prior service state. Archives include systemd templates and the manual rollback guide.

Verification: 25 isolated Linux installer tests PASS (0.032s); cmd/server, cmd/demux and internal/admin PASS. A separate transient systemd unit on the authorized Linux host passed DynamicUser/LoadCredential/state-directory/start/reload/restart checks. A second scenario actually replaced the older A3 server binary with the new binary and migrated identity schema 1 to 2 while preserving CA and the users map. All temporary units were stopped and removed; their synthetic state/result artifacts remain for inspection. This did not install over the host's existing services or exercise ACME against a live domain. Rollback fault injection and nginx preservation use isolated mocked installer tests. The amd64 test archive was built, required files inspected and SHA256 verified; it was not published. Both review findings (archive-relative restore command and directory durability) were repaired and re-reviewed. Total implementation milestones: 16/20; B4/E1/E2/E5 remain.

## B4 / E1 / E2 — integration in progress (2026-10-03)

Two simultaneous runtime-managed QUIC/HTTPS exits passed the real Android MultipleLiveTest (58.026s), including TCP/UDP routing, pausing one exit with its traffic blocked, resuming, changing rule priority and restarting the whole VPN. Explicit runtime Stop now retires the authenticated server session before closing paths; seven-session regression tests pass without exhausting retained-session capacity. Accidental path loss retains its existing grace period.

Dashboard model, event adapter, launcher navigation and carousel configuration: 17 Android tests PASS (1.965s), build/lint PASS. Review regressions prevent spare-path failure from marking a healthy exit down and prevent Wi-Fi RTT from appearing as LTE RTT. Diagnostic events from the runtime Gateway now reach the exit event stream (Linux regression PASS). Complete integration and screenshot checks remain pending.

Linux race/vet passed for bond, gateway, path policy and mobile before the latest review fixes. The server-restart test now explicitly checks Reconnect and TUN preservation after abrupt socket closure; it no longer assumes raw QUIC detects silent loss within three seconds. Ten repeated runs passed. Selected-exit Echo passed nine Go tests with race detection; Android diagnostic controls are being implemented. Runtime rotation at flow-ID exhaustion is a confirmed remaining B4 blocker and is being repaired within the configured pool limit. Implementation remains 16/20 complete; no new release or production deployment has occurred.

## Reviewed integration checkpoint — E1 complete; B4/E2 live gates open (2026-10-03)

E1 now displays per-exit transport/network, traffic rates and run totals, fresh RTT and 15-second maximum jitter, exit IPv4, permission-aware radio status and the shared LTE budget. VPN is the default screen without autostart; opaque path identifiers remain in diagnostics. Automatic IPv4 queries use each own Gateway, including subnet profiles, and wait for runtime readiness. The initial premature-query failure was reproduced on the phone and fixed; the final real two-exit test, including both IPv4 results, selected-exit Echo, TCP/UDP, pause/resume and whole-run restart, passed in 56.447s.

Echo diagnostics have bounded workers and cancellation, including blocked stream opening/FIN cleanup, and do not alter route selection. Android owns a diagnostic handle; terminal start/exit failures clear the old RTT and release it, while responder failures remain diagnostic. Both modes retain manual initiation and standalone Echo remains mutually exclusive with VPN.

B4 preserves authenticated sessions across bounded transport pools, explicitly releases server capacity on Stop, and rotates exhausted muxes without exceeding the pool or discarding accepted unacknowledged data. Rotation and QUIC welcome cancellation obey caller/runtime shutdown. With a one-slot pool, new flows wait for the old drain to finish (bounded at 120 seconds); old and replacement transports are not hidden outside the configured count.

Verification: full Go suite and vet PASS; final full mobile race PASS 53.942s, targeted final cancellation regressions PASS 2.019s; other affected race packages PASS. One earlier full race run hit TestBothPathsTemporarilyUnavailable timeout; three focused repeats and the subsequent full run passed, with no speculative production change. Final Android AAR/APK/lint passed; 25 new/configuration tests passed in 2.117s. Final live test passed in 56.447s. Independent reviews are resolved. B4/E1/E2 share service/runtime files and are preserved as one reviewed integration checkpoint rather than unsafe partial-file commits.

Formal completion is 17/20. B4 physical Wi-Fi/LTE failover and E2 standalone live ComparisonTest still require a test endpoint reachable over cellular; the private LAN fixture is insufficient. E5 load/fault/8-hour soak and battery acceptance remain unrun. No new release, push or production deployment is included in this checkpoint.

Post-integration Android regressions also PASS: ProfileUpdateTest, ServiceTransferTest, DnsRoutingTest and TrafficBudgetTest, 19 tests in 9.060s. Together with the 25 new/configuration tests, 44 non-live Android checks passed, plus the final live two-exit scenario.


## 2026-10-04 — Android UI, pre.8

- Главный экран без кнопки «Назад», одна кнопка подключения/отключения.
- Четыре раздела: VPN, подключения, диагностика, настройки. Выбор профиля и раздела не пересоздаёт Activity.
- Просмотр/редактирование профиля не меняет выбранный профиль. Отдельные группы параметров и черновики; сохранение оставляет экран открытым, выход с изменениями предлагает сохранить/отменить. Поля восстанавливаются после пересоздания экрана.
- Общие DNS, LTE-бюджет, резервные сети и RTT вынесены из профиля. Изменение активных параметров с перезапуском требует явного согласия и сообщает о разрыве всех выходов.
- Импорт QR, текста VLESS/JSON, файла и PKCS12 сохранён. PKCS12 подготовляется в памяти и записывается в редактируемый профиль только при сохранении.
- Под заголовком «Подключения и трафик» компактный радио-блок: WiFi — SSID/BSSID/сигнал; GSM — технология/MCC/MNC/сигнал/возраст, затем CI/TAC/PCI/частота. GSM другого цвета, сигнал жирный. Доступен без запуска VPN.
- На Redmi Note 9 Pro: 21 UI/model instrumentation test прошёл, включая редактор после recreation, сохранение без закрытия, выбор приложений, радио-формат и метрики. Живая проверка текущего AWG: подключение кнопкой, Wi-Fi, RTT/jitter и exit IPv4 получены; отключение кнопкой.
- Изменение UI не меняет транспортный код; длительные испытания переключений Wi-Fi/LTE в рамках этого изменения не повторялись. Публичная публикация APK — отдельный шаг после просмотра интерфейса.


## 2026-10-04 — Ссылки подключения, pre.9

- На странице выдачи общего QR отображается HTTPS-ссылка того же приглашения; общий срок/лимит, новый секрет не создаётся. VLESS QR сопровождается тем же vless:// URI выбранного устройства. Копирование работает в модальном окне, при запрете clipboard текст выделяется для ручного копирования. Ответы с секретом no-store/no-referrer.
- Android: «Вставить ссылку / конфиг» использует тот же просмотр и импорт, что QR, включая подтверждение HTTPS сервера и регистрацию устройства.
- Проверены admin Go tests на Linux (включая новый link test), clipboard/fallback в Chromium, 6 Android instrumentation tests (live QR test без аргумента пропускается), production VLESS export, публичный SHA256 APK. Сервер и APK pre.9 опубликованы.

### 2026-10-04 — исправление бесконечного восстановления standalone QUIC/HTTPS

На физическом Xiaomi 22101316UG, клиент pre.8, сохранены логи зависшего VPN: повторные reconnect по Wi-Fi/LTE немедленно завершались capabilities context canceled. Служба оставалась foreground, Wi-Fi был VALIDATED. Reconnect отменял прежний g.ctx; connect проверял capabilities до создания нового контекста. Перезапуски production при обновлении админки являлись возможным триггером потери транспорта.

Исправление: новый контекст попытки создаётся до capabilities; отказ проверки отменяет его и очищает running marker. Не ослаблена отмена проверок runtime. Регрессионный тест воспроизвёл ошибку на QUIC и HTTPS до изменения; после изменения прошёл. Полный go test ./mobile PASS на Linux; целевые race-тесты PASS, включая три повторных реальных QUIC/HTTPS reconnect с открытием потоков. Независимое ревью: блокирующих замечаний нет. Android AAR/APK build и lint PASS.

На телефоне установлен локальный исправленный build с текущим номером pre.9 (не отдельный опубликованный релиз). После 40 секунд отключения Wi-Fi и mobile data сети восстановлены в исходное состояние: автоматический reconnect 09:17:54.620 UTC, connected 09:17:54.939 UTC, exit IP подтверждён 09:17:55.478 UTC. Это время от начала попытки, не от физического появления сети. VPN оставлен активным. Проверка не заменяет полную матрицу B4/E5 или длительный прогон; сайт/APK в рамках этого исправления пока не обновлён.

### 2026-10-04 — опубликован Android 0.8.0-pre.10

VersionCode 30. Включает исправление восстановления standalone QUIC/HTTPS (7da42c3); новый native AAR собран и проверен в предыдущем шаге. Повторная APK сборка/lint после изменения версии PASS. APK атомарно опубликован на /lab/download/quic-lab.apk с backup, без перезапуска серверных служб. SHA256 локальной сборки и заново скачанного публичного APK совпали: f7e42b7504d1a77dc53a05097d3969a20efcdf5266833ffd7b023e62890dcc4f. Отдельный GitHub release для этого исправления пока не создан. Телефон сохраняет ранее установленный локальный pre.9 с тем же исправленным native кодом; для смены только номера версии активный VPN не прерывался.

Расширение VLESS: спецификация согласована, подробный план подготовлен в docs/superpowers/plans/2026-10-04-vless-settings-phase1-closure.md. Реализация расширения и полная приёмка ещё впереди.

### 2026-10-04 — восьмичасовой прогон завершён

Redmi Note 9 Pro, Android11/API30, run soak1791112008307: 14:06:53–22:07:05 МСК, instrumentation `OK (1 test)`, ADB exit0. 2780 TCP/UDP проверок без ошибок и без потери VPN; 2840 наблюдений службы, во всех active=true. Экран включался суммарно 172,325с (30 наблюдаемых переходов), выключен 28626,716с. FD110–111. USB-питание: автономность без зарядки не проверена. Нулевые PSS-измерения не трактовать как реальное потребление памяти. После теста VPN штатно остановлен finally-блоком.

Исходные журналы: `%TEMP%/quic-phase1-soak/run-20261004-140648`, completion.json, instrumentation.log, vpn/probe JSONL. Закрыт длительный прогон, а не вся E5.

Остаток до финализации: B4 — физические Wi-Fi/LTE переключения, подготовленный резерв ≤1с/холодный LTE ≤10с, карусель/два выхода/AWG; E2 — живое сравнение Echo на Wi-Fi/LTE; E5 — матрица сетевых отказов, нагрузка15/допуск≥30 устройств и целевые100Мбит/с, финальные regression/review/release. Расширенные VLESS-настройки требуют завершения live TLS/REALITY проверок собственным клиентом/HAPP и публикации. MDM отложен до полного закрытия стадий VPN-сервера.

### 2026-10-04 — OneKeyClean / pre.11

Исправлена restart policy работающей службы, добавлена подсказка фоновых настроек. RED/GREEN системного контракта, шесть Android проверок и build/lint PASS. На выделенном Redmi запрет автозапуска воспроизвёл OneKeyClean; разрешение автозапуска сохранило VPN при повторной очистке. На основном Xiaomi установлен pre.11, разрешён автозапуск, QUIC и TCP/UDP восстановлены. [Подробности и ограничения](vpn-process-lifecycle-2026-10-04.md): отдельный crash-тест MIUI пока FAIL, перенос LTE-бюджета через гибель процесса не реализован. Эти проверки остаются E5, универсальное восстановление не объявляется готовым. Сайт/GitHub пока без pre.11.


### 2026-10-04 — pre.12: переход из уведомления

ContentIntent уведомления VPN теперь открывает VpnActivity напрямую вместо старого диагностического MainActivity. Старт с иконки уже перенаправлялся на VPN, но обычный Intent уведомления не проходил launcher-проверку. Изменение локальное, сервер/протоколы не менялись. Android assembleDebug/lintDebug PASS. Pre.12/versionCode32 установлен на Xiaomi22101316UG. Реальный клик по уведомлению из шторки открыл VpnActivity (проверены текущий Activity и UI «VPN»/«Отключить»); QUIC, RTT и exit IPv4 подтверждены. VPN оставлен включённым. Публичный APK пока не обновлялся.

### 2026-10-04 — промежуточная фаза 1″ / 0.8.0

По решению владельца текущее состояние выделено в промежуточную фазу 1″. Полная фаза 1 остаётся открытой: B4/E2/E5 и перечисленные выше ограничения не закрываются номером релиза. Сервер и APK обновлены до 0.8.0, Android versionCode33. Состав и ограничения: [release-v0.8.0.md](release-v0.8.0.md); фактическая финальная регрессия и квалификация race/checkptr: [отчёт](release-v0.8.0-regression.md). MDM остаётся отложен до полного завершения стадий VPN-сервера.

## Release reliability follow-up — 2026-10-07

[Tasks 1 and 2 evidence](superpowers/plans/2026-10-07-release-reliability.md): durable LTE accounting and same-boot foreground recovery implemented and tested. Background restart remains subject to MIUI. Road QUIC stall with live RTT remains open: historical client evidence misses the incident; same-path MTU experiment does not match the symptom. Phase 1 and release gates are not closed.
