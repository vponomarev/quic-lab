# MDM WEB Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (самостоятельное выполнение) or superpowers:subagent-driven-development if explicitly selected. Steps use checkbox syntax for tracking.

**Goal:** Управлять конфигурацией каждого Android-телефона и VPN из WEB, находить MDM в карточке пользователя, подключать телефоны ссылкой или QR.

**Architecture:** Расширить существующий MDM, не создавать второй канал. Сервер разделяет observed/desired и подтверждение применения. Единый Android-координатор сериализует настройку, VPN-команды и добровольную остановку управления.

**Tech Stack:** Go net/http, HTML/JS, Kotlin Android, существующие MDM HTTPS/AtomicFile/Keystore.

**Spec:** docs/superpowers/specs/2026-10-08-mdm-web-management-design.md (принят).

## Global Constraints

- Один MDM binding; config и inventory только при действующем config-согласии. Geo/координаты не выдаются сервером.
- Commands: только vpn_start/vpn_stop, TTL 5 минут, epoch и дедупликация; после паузы старые команды не выполнять.
- current сохраняет настройки; external возвращает личный слой при pause/delete.
- Секреты не входят в observed, обычный GET, diff и аудит. Identity отсутствует = сохранить ключи того же profile ID.
- Аудит 90 дней. Неподдерживаемый клиент не получает исполнимых новых действий.
- Go/race только Linux 192.168.5.214; Android сборка/ADB через Windows. Тесты на Redmi dc69eb2c, всегда явный serial. Note12Pro — финальная согласованная проверка.
- Не выдавать Android/MDM согласия автоматически. Перезапуск VPN для применения допустим; пользовательский Stop имеет приоритет над автоматическим рестартом.
- Текущие незавершённые задачи 4–7 mdm-core продолжаются этим планом. Завершённые foundations повторно не реализовывать.

## Review Focus

- Частичный Android inventory не удаляет выбранные пакеты: задача 3.
- Локальная конфигурация меняется после WEB Apply, до доставки: конфликт на телефоне, задача 4.
- Пауза сразу после commit, до ACK: очистка external и повтор ACK без повторного apply, задача 4.
- Потерян ответ сервера на команду/Apply: идемпотентный request ID, задача 2/6.
- Удалён VPN-пользователь с MDM-устройствами: MDM и история сохраняются, задача 1.

## Проверки и commits

Перед запуском Linux тестов синхронизировать checkout и проверить SHA/dirty diff. GO означает `go test -race ./internal/mdm ./internal/admin ./mobile` на Linux; при узкой задаче использовать `-run TestName`, затем затронутые пакеты целиком. PASS — exit 0 и ok, инфраструктурная ошибка не считается RED.
BUILD: `android/gradlew.bat -p android assembleDebug assembleDebugAndroidTest lintDebug --console=plain` с установленными JDK17/SDK. AT(Class): установить APK и test APK, `adb -s dc69eb2c shell am instrument -w -e class ru.vpnc.quiclab.Class ru.vpnc.quiclab.test/androidx.test.runner.AndroidJUnitRunner`; PASS только `OK (N tests)` без crash/failures.
Каждый завершённый пункт фиксировать в этом файле. Каждый task завершать отдельным commit только затронутых файлов; чужие изменения не включать.

### Task 1 — Организационная связь и вход в MDM

Files: modify internal/mdm/{store,enrollment}.go, internal/admin/{mdm,web}.go, internal/admin/{web,mdm}.html, internal/admin/ui.js; create internal/mdm/ownership.go, ownership_test.go; extend internal/admin/mdm_test.go.
Interfaces: `CreateUserInvitation(now time.Time, rights Rights, userID string) (Invitation,error)`; `AssignUser(bindingID,userID string,now time.Time) error`; `UnassignUser(userID string,now time.Time) error`. Пустой userID означает независимый MDM. Проверка существования пользователя — admin handler; ID хранится в invitationState/bindingState, не принимается из device redeem.

- [ ] RED: TestOwnershipEnrollmentAndMigration: старый snapshot открывается, владелец наследуется только с сервера, повтор redeem не создаёт запись. TestUserDeletionKeepsMDM: после удаления пользователя binding/история доступны без владельца.
- [ ] Выполнить эти тесты на Linux, зафиксировать ожидаемый FAIL.
- [ ] Реализовать ownership и миграцию, audit, проверку назначения и очистку связи при удалении пользователя. Добавить MDM:N и таблицу MDM в карточку, ссылку на устройство; создание invitation с config/vpn/telemetry правами как запросом, без автоматического согласия.
- [ ] GREEN: ownership/admin тесты, включая CSRF/неизвестный пользователь/чужой binding и фильтр по владельцу. Проверить карточку визуально без нарушения фиксированной высоты вкладок.
- [ ] Commit `feat: link MDM devices to admin users`.

### Task 2 — Наблюдаемое состояние, CAS и совместимость

Files: create internal/mdm/report.go, report_test.go; modify internal/mdm/{types,store,http,control}.go; create internal/admin/mdm_control.go, mdm_control_test.go.
Interfaces: `Report(id string, epoch int64, report DeviceReport, now time.Time) error`; `DeviceReport{Version int, Sequence int64, ConfigGeneration int64, Capabilities []string, Name,AppVersion string, VPNState string, AppliedRevision int64, Configuration json.RawMessage, Inventory *AppInventory}`. Inventory: Entries(packageID,label), hash, complete. Optional sections bound to rights. Persist ReceivedAt separately from client measurement time. Protocol feature token `web-control-v1` gates UI/runtime.
`SetDesiredChecked(id string, expectedRevision,expectedGeneration int64, requestID,mode string,document json.RawMessage)(ConfigRevision,error)` wraps existing validation/transaction; desired carries expectedGeneration. Idempotency same requestID+payload returns same revision, different payload conflicts. Equivalent `QueueVPNOnce(id,kind,requestID string,now time.Time)` prevents duplicate clicks/retries. Existing APIs stay compatible.

- [ ] RED: TestReportsRightsAndOrdering checks stale sequence rejection, absent rights, unsupported version, redaction and 1 MiB body cap; inventory max 2048 entries, labels max 256 Unicode characters, duplicate package IDs rejected. TestDesiredCASRetry checks two editors, changed observed generation and same request retry.
- [ ] Run narrow Linux tests, observe FAIL.
- [ ] Implement authenticated report operation, persist last successful sync even without telemetry, capability gates, statuses and sanitized admin DTO. Existing Sync reports grant changes before accepting related sections; reject secret-bearing report fields rather than persisting them. Limit state growth; report replaces previous report.
- [ ] GREEN: Linux tests plus old-client telemetry regression, escaped labels, no secret in GET/error/audit; queue expiry 5 min and pause epoch checks.
- [ ] Commit `feat: report MDM device state and guard remote revisions`.

### Task 3 — Android отчёт, inventory и прямой QR

Files: create android/app/src/main/java/ru/vpnc/quiclab/MdmDeviceReport.kt; modify MdmConfiguration.kt, MdmController.kt, MdmRuntime.kt, MdmModels.kt, MdmEnrollActivity.kt, ProfileScanActivity.kt; create android/app/src/androidTest/java/ru/vpnc/quiclab/MdmDeviceReportTest.kt; extend MdmConsentTest.kt.
Interfaces: `MdmDeviceReport.snapshot(context:Context,state:MdmState):JSONObject`; `MdmConfiguration.exportObserved(context:Context):JSONObject`. Экспорт строится из разрешённых полей schema, содержит наличие identity без её значения. Не передавать произвольные preferences. Capability web-control-v1 включить только после task 4/5.

- [ ] RED: snapshot без config не содержит configuration/inventory; с config содержит label/package и generation без ключей/tokens/URI. Неполный inventory не меняет apps. Consent test: QR и текст с активным VPN открывают согласие, не регистрируют до подтверждения и не заменяют binding.
- [ ] Run BUILD/AT соответствующих классов, observe FAIL.
- [ ] Implement report + persisted sequence/hash retry, inventory через тот же источник package visibility, что локальный выбор приложений. Первая выдача права/изменение списка отправляет полный актуальный inventory; между изменениями — hash. Ошибка чтения не публикует пустой список как полный. Добавить две прямые кнопки MDM, без проверки «VPN остановлен».
- [ ] GREEN BUILD/AT; проверить отсутствие config-данных после отзыва права между сбором и отправкой; прежний профиль и работающий VPN неизменны после отмены сканирования.
- [ ] Commit `feat: expose MDM enrollment and consented device inventory`.

### Task 4 — Транзакционное применение при работающем VPN

Files: create android/app/src/main/java/ru/vpnc/quiclab/MdmApplyCoordinator.kt; modify MdmConfiguration.kt, MdmConfigurationStore.kt, MdmController.kt, MdmRuntime.kt, MdmStore.kt, LabVpnService.kt; create android/app/src/androidTest/java/ru/vpnc/quiclab/MdmApplyCoordinatorTest.kt.
Interfaces: `MdmApplyCoordinator.apply(context:Context,state:MdmState,desired:MdmConfigRevision):ApplyResult`; `recover(context:Context)`; `detach(context:Context)`. ApplyResult: revision, configurationApplied:Boolean, vpnState:String, errorCode:String?. Persist operation(binding,epoch,generation,revision,phase,previousVpnIntent), phase prepared/committed/completed. Recovery resolves durable configuration first; no ACK before commit. Lock ordering follows existing controller then configuration lock; no network I/O or blocking service stop while holding either lock.

- [ ] RED: tests current/external, invalid document, write failure, changed config generation before delivery, pause/delete/right withdrawal during apply; crash injection before/after commit and before ACK. Assert old settings on failed commit, current retained/external removed on detach, retry no second application.
- [ ] Run AT, observe FAIL.
- [ ] Implement serialize/validate/stop/commit/restart with authorization recheck at commit and restart. Waiting for VPN stop has bounded timeout and explicit error. Track user stop generation so local Stop prevents scheduled restart. Persist cleanup before pause/delete returns; app/service restoration resolves cleanup before starting managed VPN. ACK revision comes from durable config store, not volatile event memory.
- [ ] GREEN BUILD/AT plus MdmConfigurationTest, MdmConfigurationStoreTest, MdmLifecycleTest, existing budget/recovery tests. VPN start failure keeps applied settings, reports distinct error. A local change after Apply conflicts rather than overwrites.
- [ ] Commit `feat: apply MDM configurations with durable VPN coordination`.

### Task 5 — Команды VPN и аудит действий

Files: create android/app/src/main/java/ru/vpnc/quiclab/MdmVpnControl.kt; modify LabVpnService.kt, VpnActivity.kt, MdmRuntime.kt, MdmModels.kt, MdmStore.kt; create android/app/src/androidTest/java/ru/vpnc/quiclab/MdmVpnControlTest.kt; extend internal/mdm/report_test.go.
Interfaces: `MdmVpnControl.execute(context:Context,state:MdmState,command:MdmCommand):String` returns stable result code; `recordUserAction(context:Context,kind:String)`. Использует координатор задачи 4 и durable command-ID ledger до expiry+24h. События source user/device, request/command ID; success означает проверенное состояние службы, не отправленный Intent.

- [ ] RED: unauthorized/paused/wrong epoch/expired command cannot start VPN; duplicate ID cannot repeat start; user Stop after queued start remains stopped; missing VpnService.prepare consent returns needs_android_consent. Test process death before result delivery.
- [ ] Run AT, observe FAIL.
- [ ] Implement commands and durable event retry, wire poll consumers/cleanup and applied revision, advertise web-control-v1. Config first, fresh commands afterwards; apply failure cannot accidentally start rejected profile. Keep control channel alive after VPN Stop.
- [ ] GREEN BUILD/AT + Linux dedupe/result tests; real start/stop over independent MDM channel on test phone, with user action events.
- [ ] Commit `feat: execute and acknowledge voluntary MDM VPN commands`.

### Task 6 — WEB редактор и результаты применения

Files: create internal/admin/mdm_editor.html, mdm_editor.js; modify internal/admin/{mdm,mdm_control,web}.go, mdm.html; extend mdm_control_test.go.
Routes: GET mdm/device?id=; GET mdm/state?id=; POST mdm/config (id, expectedRevision, expectedGeneration,requestID,mode,document); POST mdm/command (id,kind,requestID); POST mdm/assign (id,userID). Все под существующим admin auth/CSRF, ответы JSON для действий без полной перезагрузки. GET никогда не возвращает identity; отдельное подтверждение явной замены credentials.

- [ ] RED: handlers enforce rights/capability/CAS, unsupported client receives update-required, repeated Apply returns same revision; GET contains observed+desired status without credentials. Sanitized desired uses presence markers; save without replacement preserves pending credentials server-side as well as existing device credentials.
- [ ] Run Linux tests, observe FAIL.
- [ ] Implement Обзор/Конфигурация/Приложения/Телеметрия/События. Form edits full document preserving untouched fields; transports, profile choices, routes, reserve, budget, DNS, diagnostics. Inventory searchable, missing selections retained. Preview diff masks secrets; Apply only explicit click; polling never resets draft. Conflict offers reload, not auto-overwrite. Show receivedAt, measurement time, rights/stale data and pending/applying/applied/error separately from VPN state. Existing telemetry links continue working.
- [ ] GREEN Linux admin tests; browser acceptance at Full HD and narrow viewport: two tabs conflict, offline device, dropped POST response/retry, duplicate clicks, malicious labels, update-required client, pending new credential edit without data loss.
- [ ] Commit `feat: manage phone configuration from the MDM web console`.

### Task 7 — Интеграционная приёмка и публикация

Files: update docs/mdm-core-progress.md, docs/mdm-configuration-schema.md; create docs/mdm-web-management-acceptance.md; update android/app/build.gradle.kts version after inspecting actual published version.

- [ ] Run complete affected Linux race suites and vet; BUILD/lint and MDM Android suite on Redmi. Verify source SHA on Linux matches tested checkout. Record commands, versions, serial and results, including any unsupported Android versions.
- [ ] E2E: enroll assigned and independent device; report inventory; edit real DNS/apps/routes/reserve/budget/profile; verify actual tunneled/direct traffic. Test current/external, offline latest config, local Stop, pause/delete during apply, service/process death, stale client and telemetry/update regression. No automatic consent changes.
- [ ] Review full diff against accepted spec with a fresh reviewer; resolve critical findings and rerun impacted tests. Do not count prior foundation tests as runtime evidence.
- [ ] Back up production binary/config/APK, deploy server before client with old-client gates; publish APK/version/hash, validate public download. Keep rollback binary/APK and explain downgrade constraints if durable schema changes.
- [ ] Install on available test phone; verify from WEB and record Note12Pro acceptance when available without silently granting new rights. Update completed/pending status and commit `docs: record MDM web management acceptance`.
