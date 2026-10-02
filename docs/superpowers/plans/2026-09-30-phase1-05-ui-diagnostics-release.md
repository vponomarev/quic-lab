# Интерфейс, диагностика и приёмка Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Представить единый Android VPN-интерфейс и подтвердить первую фазу воспроизводимыми проверками.

**Architecture:** UI читает snapshots выходов и бюджета, не принимает транспортные решения. Диагностика использует выбранный выход либо отдельный standalone Echo; серверная упаковка сохраняет данные и явные границы версии.

**Tech Stack:** Go 1.26.8, существующий fork quic-go 0.63.0, coder/websocket, AmneziaWG, Kotlin, Android API 30+, JDK 17, Linux/systemd.
**Spec:** [Утверждённая спецификация](../specs/2026-09-30-vpn-product-design.md), утверждена владельцем 2026-09-30.
**Status:** План предложен для проверки; реализация не начата.

## Global Constraints

- Все Go/Python-тесты, race/vet и отдельные тестовые бинарники выполняются на Linux-хосте root@192.168.5.214; Windows — существующая Android-сборка и USB/ADB, instrumentation — на телефоне. Подготовка и ограничения хоста описаны в плане 00.

- Android 11+ — единственный обязательный клиент; Linux/systemd — сервер. Не добавлять Docker, автозапуск или IPv6.
- Максимальная доступность: до 1 секунды от отказа при готовом исправном резерве; экономия: до 10 секунд при доступном LTE. Сессия без путей: 120 секунд по умолчанию.
- Карусель: 2–5 соединений всего на профиль. Резерв не прогревается без разрешённой проверки. Лимит LTE общий на один запуск всего VPN.
- VPN-трафик при отказе блокируется; явный Stop всего VPN снимает блокировки. Остановка одного выхода сохраняет блокировку его правил.
- QR: 24 часа / 5 новых устройств по умолчанию. Одновременная нагрузка: 15 устройств, предел 30. 100 Мбит/с — внешний канал, не обещанная полезная скорость.
- Минимум два независимых выхода. В первой фазе нет VLESS, подписок, доменных правил, системной постоянной блокировки после завершения службы.
- Секреты и реальные клиентские конфиги не включать в тестовые fixtures, логи или git. Новые зависимости не нужны без отдельного обоснования.
- Исходная рабочая копия содержит незакоммиченные разработки. Выполнять только после сохранения согласованного baseline в изолированную рабочую копию; порядок — в плане 00.
- Новые интерфейсы ниже — проектируемые, не уже существующие API. Go API с context/структурами остаются internal; gomobile получает отдельные простые методы с JSON.
- Каждый шаг red/green выполняется на существующем коде и свежем baseline. Если сценарий уже реализован, сначала подтвердить тестом и не переписывать его ради плана.
- Коммит задачи включает только перечисленные изменённые файлы и тесты. Не применять git add . или публикацию/деплой.
- Блоки assert ниже — компактные контракты тестов (псевдокод), не готовый исполняемый тестовый файл. Реализовать именованные Go/Android-тесты стандартными средствами проекта; fixture строится в файле теста задачи.

## Зависимости

A–D для итоговой приёмки. E1 можно писать на fixtures snapshot, но завершать только на реальных событиях B4/C1.

## Review Focus

- Поздний результат exit-IP отображается для нового выхода (E1).
- Отказ permissions/отключённый RTT рисует старые метрики как свежие (E1).
- Диагностический Echo обходит выбранный VPN при отказе (E2).
- Захват случайно включает активные bond-пути (E3).
- Обновление systemd стирает устройства или ломает прежний nginx (E4).

## Карта файлов и задач

Пути ниже относительно корня репозитория quic-lab. Новые файлы выделены по ответственности; существующие крупные файлы не переписывать целиком. Общие команды Android и правила baseline — в [плане 00](2026-09-30-phase1-00-roadmap.md).

### Task E1: Главный экран VPN и метрики выходов

**Files:**
- Create: `android/app/src/main/java/ru/vpnc/quiclab/VpnDashboardModel.kt`; `android/app/src/androidTest/java/ru/vpnc/quiclab/VpnDashboardModelTest.kt`.
- Modify: `android/app/src/main/java/ru/vpnc/quiclab/MainActivity.kt`, `android/app/src/main/java/ru/vpnc/quiclab/VpnActivity.kt`, `android/app/src/main/java/ru/vpnc/quiclab/TransportStats.kt`, `android/app/src/main/java/ru/vpnc/quiclab/RadioMonitor.kt`, `android/app/src/main/java/ru/vpnc/quiclab/LatencyChart.kt`; `mobile/gateway_exit.go`, `mobile/gateway_traffic.go`, `mobile/gateway_exit_test.go`.

**Interfaces:** Kotlin data class ExitSnapshot(val exitId: String, val generation: Long, val state: String, val activePathId: String?, val paths: List<PathSnapshot>, val rxBytes: Long, val txBytes: Long, val rxBps: Double, val txBps: Double, val exitIpv4: String?); PathSnapshot: pathId, profileId, network, state, rttMs?, jitterMs?, measuredAtMs. VpnDashboardModel.accept(snapshot: ExitSnapshot): Boolean. Существующий Gateway.CheckExitIP() вызывается на собственном gateway выхода; событие помечено generation.

- [ ] **Step 1 — регрессионный тест:** добавить VpnDashboardModelTest.staleMetricsAndExitIp; TestExitIPBoundToGateway; VpnDashboardTest. Минимальные обязательные проверки:

```text
assert staleGenerationEvent.ignored
assert disabledRTT.label == 'RTT выключен'
assert homeWithoutInternet.exitIPv4 == 'Недоступен'
assert internetAndHome.haveSeparateTotals
assert deniedLocationPermission.doesNotBreakVPN
assert reconnectTransport.doesNotResetRunTotals
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** VPN сделать основным экраном. Карточка выхода: RX/TX скорость и итог за запуск, активный профиль/транспорт/путь, RTT активного пути и доступных альтернатив, jitter при свежих samples, exitIPv4, краткие radio данные. Не смешивать jitter с QUIC variance: использовать существующее определение jitter в TransportStats и подписать окно. Счётчики переживают reconnect профиля и сбрасываются при Stop/Start всего VPN. Устаревание RTT после max(3*configuredInterval,5s); отсутствие разрешения — 'Недоступно', не нули. Exit IP запрос при подключении/смене выхода; отменить старый и проверить поколение результата. Сверху общий бюджет C1.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./mobile -run 'Exit|Traffic' -count=1; Android: VpnDashboardModelTest, VpnDashboardTest
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: show per-exit VPN status traffic and network metrics"`. Не включать соседние незавершённые задачи.

### Task E2: Два режима Echo и изоляция диагностики

**Files:**
- Create: `android/app/src/main/java/ru/vpnc/quiclab/EchoActivity.kt`; `mobile/gateway_echo.go`, `mobile/gateway_echo_test.go`; `android/app/src/androidTest/java/ru/vpnc/quiclab/VpnEchoTest.kt`.
- Modify: `android/app/src/main/java/ru/vpnc/quiclab/MainActivity.kt`, `android/app/src/main/java/ru/vpnc/quiclab/ProfileEchoSession.kt`, `android/app/src/main/java/ru/vpnc/quiclab/LabVpnService.kt`; `android/app/src/main/AndroidManifest.xml`.

**Interfaces:** gomobile: func (g *Gateway) StartExitEcho(target string, intervalMillis int64) error; func (g *Gateway) StopExitEcho(); emit exit_echo с exit_id/generation/rtt_ms/error. Kotlin EchoActivity выбирает standalone или exit ID. target — разрешённый тестовый адресат; выполнять обмен через g.dialStream/его UDP backend, не стандартный direct HTTP client.

- [ ] **Step 1 — регрессионный тест:** добавить TestExitEchoCannotFallback; TestExitEchoDoesNotSwitchPolicy; VpnEchoTest; ComparisonTest. Минимальные обязательные проверки:

```text
assert runningVPNAndStandaloneEcho.mutuallyExclusive
assert runningVPNAndExitEcho.allowed
assert unavailableExitEcho.neverUsesDirect
assert echoTargetFailure.doesNotPromoteAnotherProfile
assert echoTraffic.countedInSharedBudget
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Перенести существующий самостоятельный Echo UI на дополнительный экран без потери QUIC/WSS/AWG сравнения и радиографиков. Новый echo проходит через выбранный выход до тестового responder (управляемый локальный/публичный стенд, адрес не зашит в APK). RTT полного пути отдельный от RTT шлюза. Echo ошибки диагностические, не feed для автомиграции. Цикл ограничен, отменяется вместе с выходом и при закрытии опыта. Не активировать отдельный VpnService для Echo.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./mobile -run 'Echo|Exit' -count=1 -timeout=90s; Android: VpnEchoTest, ComparisonTest
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: keep lab Echo separate and add Echo through VPN exits"`. Не включать соседние незавершённые задачи.

### Task E3: Серверный захват и ограничения режима

**Files:**
- Modify: `internal/admin/user_capture.go`, `internal/admin/user_capture_test.go`, `internal/admin/capture.go`, `internal/admin/capture_settings.go`; `internal/gateway/capture.go`; `cmd/server/capture_existing_test.go`, `cmd/server/capture_http_test.go`; `docs/wireshark-capture.md`.

**Interfaces:** func (s *Store) CaptureEligible(deviceID string) error; error при наличии активной multiplexed сессии цели. Существующие capture handlers проверяют eligibility до начала и при смене режима; транспорт публикует признак multiplexed из B/D. Не делать исключение лишь по имени QUIC или HTTPS.

- [x] **Step 1 — регрессионный тест:** добавить TestCaptureRejectsBond; TestCaptureStopsWhenTargetEntersBond; existing capture tests. Минимальные обязательные проверки:

```text
assert activeBond.captureRequestRejectedClearly
assert captureRequest.doesNotDisableBond
assert standaloneCapture.canStartWithoutClientConfirmation
assert innerHTTPSApplicationPayload.remainsEncrypted
assert TLSSecretsNotWrittenToOrdinaryLogs
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** Сохранить существующий внешний/внутренний capture и выдачу серверных TLS secrets администратору. При переходе захватываемой цели в bond остановить capture с явной причиной, не VPN. Общий захват не должен обходить запрет через другой UI endpoint; если невозможно отделить bond в общем захвате, отказать общему захвату до его остановки. Не добавлять захват карусели в эту фазу.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/admin ./internal/gateway ./cmd/server -run 'Capture' -count=1 -timeout=120s
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "fix: enforce capture scope for non-multiplexed connections"`. Не включать соседние незавершённые задачи.

### Task E4: Systemd, обновления и согласованная документация

**Files:**
- Modify: `scripts/install-server.py`, `scripts/test-install-server.py`, `scripts/build-server-release.py`; `deploy/quic-lab.service`, `deploy/quic-lab-public.service`; `docs/install-server.md`, `docs/server-config.md`, `docs/vpn-plan.md`, `docs/max-availability.md`, `README.md`.
- Create: `docs/phase1-upgrade.md`.

**Interfaces:** Расширить существующий JSON config: bond_disconnect_grace_seconds=120, max_active_devices=30, update_base_url, accepted_vpn_sni; явная валидация port/SNI conflicts. Installer upgrade сохраняет identities/config и делает backup до миграции, не меняет известные режимы nginx/direct самовольно.

- [ ] **Step 1 — регрессионный тест:** добавить Existing installer tests + test_upgrade_keeps_device_state + test_nginx_mode_preserved. Минимальные обязательные проверки:

```text
assert upgrade.preservesUsersDevicesAndKeys
assert failedValidation.doesNotReplaceWorkingConfig
assert existingNginxModeAndPortsPreserved
assert serviceRestartMayDisconnectButClientRecovers
assert noAutomaticPublishOrDeploy
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Поддержать установку и обновление существующим systemd-путём, credentials и LE renewal. Миграция админки выполняется при старте с backup; если rollback требует старого формата, документировать восстановление backup, не читать новую схему старым бинарником. Installer проверяет конфиг перед рестартом; бинарник rollback сохраняется. Описать APK обязательность, максимальные устройства, DNS, SNI ограничения, семантику счётчиков. Обновить устаревшие roadmap-абзацы только по реально завершённым задачам.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
python scripts/test-install-server.py; go test ./cmd/server ./cmd/demux ./internal/admin -count=1 -timeout=120s; Linux disposable VM: systemd start/restart/upgrade checks
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: package phase one server with safe systemd upgrades"`. Не включать соседние незавершённые задачи.

### Task E5: Воспроизводимая приёмка и полевой прогон

**Files:**
- Create: `internal/bond/acceptance_test.go`; `scripts/phase1-acceptance.py`; `docs/phase1-acceptance.md`; `android/app/src/androidTest/java/ru/vpnc/quiclab/PhaseOneSoakTest.kt`.
- Modify: `android/app/src/androidTest/java/ru/vpnc/quiclab/BondDeviceTest.kt`, `android/app/src/androidTest/java/ru/vpnc/quiclab/CarouselDeviceTest.kt`, `android/app/src/androidTest/java/ru/vpnc/quiclab/MultipleLiveTest.kt`.

**Interfaces:** python scripts/phase1-acceptance.py --scenario <fault|load|soak> --config <local-untracked.json> --output <local-dir>. Report JSON: build hashes, device/API, server CPU/RAM, link cap, scenario, fault_at_monotonic, useful_resume_at_monotonic, rx/tx/overhead, result. Скрипт не содержит credentials; автоматические remote mutations требуют выбранного тестового стенда.

- [ ] **Step 1 — регрессионный тест:** добавить TestAcceptanceExactBytesAcrossTransports; PhaseOneSoakTest; 15/30/31 device load scenarios. Минимальные обязательные проверки:

```text
assert preparedBackupResume-faultAt <= 1*second
assert coldAllowedLTEResume-faultAt <= 10*second
assert singleTCPSocketAndExactBytes
assert eightHourRun.noUnboundedMemoryGrowthOrUnexpectedStop
assert active15.devicesAllMakeProgress
assert active30.allowed && active31.rejected
assert obsoleteSmallProbeDoesNotHideLargeTransferBlackhole
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Проверять Wi-Fi off/on, silent drops, размерные drops и остановку после 1/2MiB; крупную непрерывную передачу, idle, half-close, UDP duplicates, loss/reorder и медленного читателя. QUIC↔HTTPS, два реальных независимых выхода, AWG standalone, отзыв/обновление, exhausted budget, DNS/IPv6 bypass, screen-off. Soak минимум 8h с TCP/UDP и рядами RSS/heap/sockets; отдельные замеры батареи обоих режимов, не вводить неподтверждённый порог. Согласовать VPS CPU/RAM/100Mbit shaping в отчёте. Один телефон и локальный loopback не доказывают нагрузку 15 устройств. Если устройство/стенд недоступны, пометить конкретные проверки not run и не объявлять MVP принятым. Выпуск после всей матрицы, не после одной сборки.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./... -count=1 -timeout=180s; go vet ./...; Linux: go test -race ./internal/... ./mobile/... -count=1 -timeout=300s; Android build/lint + PhaseOneSoakTest; python scripts/phase1-acceptance.py --scenario load --config <local-untracked.json> --output <local-dir>
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "test: add reproducible phase one VPN acceptance suite"`. Не включать соседние незавершённые задачи.

## Проверка плана

- Сопоставить результат задач с матрицей покрытия в плане 00.
- Review Focus распределён по указанным тестам; транспортные тесты не заменяют испытания на Android.
- Никакой шаг этого плана не является разрешением на публикацию или изменение действующего сервера.
