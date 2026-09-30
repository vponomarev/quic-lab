# Модель выходов и жизненный цикл Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ввести стабильные идентификаторы выходов и профилей, сохранив single/multiple и самостоятельный AWG.

**Architecture:** Чистая Go-модель описывает выходы и профили, Android хранит её с миграцией прежних настроек. Один контроллер владеет жизненным циклом всего VPN; выходы перезапускаются независимо.

**Tech Stack:** Go 1.26.8, существующий fork quic-go 0.63.0, coder/websocket, AmneziaWG, Kotlin, Android API 30+, JDK 17, Linux/systemd.
**Spec:** [Утверждённая спецификация](../specs/2026-09-30-vpn-product-design.md), утверждена владельцем 2026-09-30.
**Status:** A1–A3 выполнены. Результаты проверок: docs/phase1-progress.md.

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

После baseline 00. Задачи A1–A3 предшествуют интеграции транспортов и политик.

## Review Focus

- Повторная миграция настроек не создаёт дубликаты (A1).
- Потерянный callback старой сессии не меняет новую (A2).
- Неизвестный UID не приводит к direct (A3).
- Остановка одного выхода не снимает правила остальных (A3).
- Сбой записи не уничтожает старые настройки и ключи (A1).

## Карта файлов и задач

Пути ниже относительно корня репозитория quic-lab. Новые файлы выделены по ответственности; существующие крупные файлы не переписывать целиком. Общие команды Android и правила baseline — в [плане 00](2026-09-30-phase1-00-roadmap.md).

### Task A1: Модель и атомарная миграция профилей

**Files:**
- Create: `internal/vpnmodel/config.go`, `internal/vpnmodel/config_test.go`; `android/app/src/main/java/ru/vpnc/quiclab/VpnConfiguration.kt`; `android/app/src/androidTest/java/ru/vpnc/quiclab/VpnConfigurationTest.kt`.
- Modify: `android/app/src/main/java/ru/vpnc/quiclab/VpnProfiles.kt`, `android/app/src/main/java/ru/vpnc/quiclab/ProfileImport.kt` (в том же каталоге).

**Interfaces:** vpnmodel: type Config struct { Version int; Exits []Exit; Profiles []Profile }; type Exit struct { ID, Name, Kind, DemuxID string }; type Profile struct { ID, ExitID, Transport, Mode, Endpoint string; Priority, PoolSize int; CheckReserve bool }; func Parse(raw []byte) (Config,error); func Validate(c Config) error. Kind: demux/standalone; Mode: auto/reserve/disabled. VpnConfiguration.migrate(context: Context): JSONObject; load(context: Context): JSONObject.

- [x] **Step 1 — регрессионный тест:** добавить TestConfigOwnership; VpnConfigurationTest.migrationIsIdempotentAndAtomic. Минимальные обязательные проверки:

```text
assert migrate(migrate(oldConfig)) == migrate(oldConfig)
assert legacyAWG.exit.kind == 'standalone'
assert legacyProfileIDsAndIdentityFilesPreserved
assert reject(duplicateID, missingExit, poolSize=6)
assert standaloneAWG.profileCount == 1
assert failedWriteLeavesOldConfigAndKeysIntact
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** Старый профиль становится отдельным выходом с прежним ID для маршрутов; существующий max_availability — demux, остальные сохраняют фактический backend. Не группировать разные серверы автоматически. Profile.PoolSize=1 означает выключенную карусель; при включении 2–5. Версионированный JSON плюс атомарная замена, backup прежних настроек. Секреты остаются в VpnIdentity, в JSON только ссылки. Перенести выбор DNS без изменения поведения.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/vpnmodel -count=1; Android: VpnConfigurationTest
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: introduce exit and connection profile model"`. Не включать соседние незавершённые задачи.

### Task A2: Контроллер выхода и защита от устаревших событий

**Files:**
- Create: `internal/vpnmodel/lifecycle.go`, `internal/vpnmodel/lifecycle_test.go`; `android/app/src/main/java/ru/vpnc/quiclab/VpnExitController.kt`.
- Modify: `android/app/src/main/java/ru/vpnc/quiclab/MultipleVpnController.kt`, `android/app/src/main/java/ru/vpnc/quiclab/VpnSession.kt`, `android/app/src/main/java/ru/vpnc/quiclab/LabVpnService.kt`.
- Test: `android/app/src/androidTest/java/ru/vpnc/quiclab/VpnExitLifecycleTest.kt`.

**Interfaces:** type State string: stopped, connecting, active, recovering, blocked, incompatible; type Event struct { ExitID string; Generation uint64; Kind string }; type Runtime struct; func NewRuntime(c Config) *Runtime; func (r *Runtime) Start(id string) uint64; func (r *Runtime) Apply(e Event) bool; func (r *Runtime) Stop(id string); func (r *Runtime) State(id string) State. Kotlin VpnExitController.start(exitId: String), stop(exitId: String), stopAll().

- [x] **Step 1 — регрессионный тест:** добавить TestLifecycleRejectsOldGeneration; VpnExitLifecycleTest.independentRestart. Минимальные обязательные проверки:

```text
assert Apply(eventFromPreviousGeneration) == false
assert restart('internet').doesNotStop('home')
assert incompatibility('internet').state == 'incompatible'
assert stopAll().activeSessions == 0
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** Сериализовать команды службы; привязать callbacks к поколению выхода. Адаптер существующего VpnSession реализует один runtime выхода. Разделить остановку отдельного выхода и всей службы. Никаких BOOT_COMPLETED или включения Always-on. При ручном Stop всего VPN закрыть TUN/транспорты и убрать блокировки; уведомление первого Stop хранится как локальная настройка.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/vpnmodel -count=1; Android: VpnExitLifecycleTest
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: isolate VPN exit lifecycles"`. Не включать соседние незавершённые задачи.

### Task A3: Привязка маршрутизатора к выходам

**Files:**
- Modify: `mobile/multiple.go`, `mobile/multiple_test.go`; `internal/routing/policy.go`, `internal/routing/policy_test.go`; `android/app/src/main/java/ru/vpnc/quiclab/MultipleVpnPlan.kt`, `android/app/src/main/java/ru/vpnc/quiclab/MultipleVpnController.kt`.
- Test: `android/app/src/androidTest/java/ru/vpnc/quiclab/MultipleLiveTest.kt`.

**Interfaces:** Сохранить MultiRouter.SetGateway(id string, g *Gateway) error, но id теперь стабильный Exit.ID из A1; существующие операции pause/detach используют тот же ID. Новый профиль внутри выхода не вызывает SetGateway, пока общая сессия жива. Новое func (m *MultiRouter) BlockExit(id string) error оставляет правила и закрывает потоки только этого выхода.

- [x] **Step 1 — регрессионный тест:** добавить TestExitPausePreservesRules; TestTransportChangePreservesGateway; MultipleLiveTest. Минимальные обязательные проверки:

```text
assert firstMatchingRuleWins
assert noMatchIPv4 == 'direct'
assert unknownUIDAtAppRule == 'blocked'
assert pausedExitTraffic != 'direct'
assert pausedExitTraffic != otherExit
assert transportReplacementKeepsFlowDestination
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** Сохранить существующее определение UID и правила общего UID. Привязать tunHandler к выходу и общей сессии, а не физическому соединению. Снять текущий blanket-запрет multiple+bond только после B4; пока оставить явную ошибку. Правила фиксированы на запуске, применение новой конфигурации выхода — отдельная операция D4.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
Linux: go test ./internal/routing ./mobile -count=1 -timeout=120s; Android: MultipleLiveTest
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: route flows by stable VPN exit"`. Не включать соседние незавершённые задачи.

## Проверка плана

- Сопоставить результат задач с матрицей покрытия в плане 00.
- Review Focus распределён по указанным тестам; транспортные тесты не заменяют испытания на Android.
- Никакой шаг этого плана не является разрешением на публикацию или изменение действующего сервера.
