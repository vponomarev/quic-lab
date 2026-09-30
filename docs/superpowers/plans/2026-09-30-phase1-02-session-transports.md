# Общая сессия, QUIC/HTTPS и карусель Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сохранить пользовательские потоки при замене QUIC/HTTPS-соединений и сетей с ограниченными ресурсами.

**Architecture:** Существующие bond.Mux и bond.Session сохраняют семантику потоков. Адаптеры QUIC и HTTPS реализуют bond.Path; идентичность соединения отделяется от типа физической сети и политики профиля.

**Tech Stack:** Go 1.26.8, существующий fork quic-go 0.63.0, coder/websocket, AmneziaWG, Kotlin, Android API 30+, JDK 17, Linux/systemd.
**Spec:** [Утверждённая спецификация](../specs/2026-09-30-vpn-product-design.md), утверждена владельцем 2026-09-30.
**Status:** B1 выполнен с переходным ограничением конечного LTE-бюджета до C1/C2; B2–B4 не начаты.

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

A1–A3. B1/B2 можно проверить на Go-стенде до Android. B4 интегрируется после C1/C2; окончательная авторизация и лимит устройств — D1/D2.

## Review Focus

- Данные ждут 120 с, но старый recordTimeout=30 с закрывает поток (B1).
- Старый OPEN после ротации ID повторно открывает конечный сокет (B1).
- Маленькие ACK проходят при зависших больших данных (B3).
- Заблокированная запись HTTPS останавливает QUIC-путь (B2).
- Суммарный пул Wi-Fi/LTE превышает 5 после гонки reconnect (B4).

## Карта файлов и задач

Пути ниже относительно корня репозитория quic-lab. Новые файлы выделены по ответственности; существующие крупные файлы не переписывать целиком. Общие команды Android и правила baseline — в [плане 00](2026-09-30-phase1-00-roadmap.md).

### Task B1: Идентичность пути, удержание сессии и ограничение ресурсов

**Files:**
- Modify: `internal/bond/session.go`, `internal/bond/mux.go`, `internal/bond/session_test.go`; `internal/gateway/bond.go`; `mobile/gateway_bond.go`; `cmd/demux/main.go`, `cmd/server/config.go`.
- Create: `internal/bond/options.go`, `internal/bond/retention_test.go`.

**Interfaces:** type Options struct { DisconnectGrace time.Duration; MaxPendingSession, MaxPendingFlow int }; func NewWithOptions(ctx context.Context, deliver func(Record) bool, opts Options) *Session; type PathInfo struct { ID, ProfileID, Network string; Generation uint64 }; func (s *Session) AddNamedPath(info PathInfo, p Path) error; сохранить New/AddPath как внутренние совместимые обёртки. BondHello получает path_id/profile_id/network/generation, не ограничивает path_id строками wifi/cell.

- [x] **Step 1 — регрессионный тест:** добавить TestRetentionPreservesPendingUntilGrace; TestPathGenerationReplacement; TestFlowIDExhaustionDrains. Минимальные обязательные проверки:

```text
assert disconnected(119*second).sessionAlive
assert disconnected(120*second).sessionClosed
assert pendingReliableBytesRemainUntilGrace
assert pendingSession <= 1024 && pendingPerFlow <= 64
assert lateOpenFromPreviousSession.doesNotCreateSocket
assert exhaustion.rejectsNewFlowWithoutKillingExisting
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** По умолчанию DisconnectGrace=120s, max pending 1024/64. При полном отсутствии путей отделить срок удержания от 30s таймаута медленного читателя; заморозить таймер прогресса потока на время полной потери путей, но не срок сессии. Сохранить bounded receive queues. Перед исчерпанием ID перевести старую сессию в draining, новые потоки — в новую; старые остаются до завершения/таймаута. Секрет новой сессии новый; токен старой не возобновляет её после закрытия. Закрытие и удаление пути проверяют поколение.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/bond ./internal/gateway ./mobile ./cmd/server ./cmd/demux -count=1 -timeout=120s
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: retain bounded bond sessions across path replacement"`. Не включать соседние незавершённые задачи.

### Task B2: HTTPS WebSocket-адаптер общей сессии

**Files:**
- Create: `internal/bondhttps/path.go`, `internal/bondhttps/path_test.go`; `internal/gateway/bond_https.go`, `internal/gateway/bond_https_test.go`; `mobile/gateway_bond_https.go`.
- Modify: `internal/gateway/bond.go`, `internal/gateway/server.go`; `mobile/gateway_bond.go`; `cmd/server/main.go`, `cmd/demux/main.go`.

**Interfaces:** func bondhttps.NewPath(c *websocket.Conn, cleanup func()) bond.Path; func (s *gateway.Server) ServeBondHTTPS(w http.ResponseWriter, r *http.Request); общий внутренний joinBond(ctx context.Context, identity tls.ConnectionState, hello BondHello, path bond.Path) error используется обоими входами. mobile dialBondPath(ctx context.Context, profileJSON string, binder SocketBinder) (bond.Path,error) — internal API.

- [ ] **Step 1 — регрессионный тест:** добавить TestHTTPSPathFramingAndCancel; TestQUICHTTPSShareOneExitSocket; TestBlockedHTTPSDoesNotBlockQUIC. Минимальные обязательные проверки:

```text
assert oneBinaryWebSocketMessage == oneBondRecord
assert oversizedRecordRejected
assert blockedHTTPSWrite.doesNotBlockQUIC
assert tcpSocketCountAfterQUICToHTTPS == 1
assert invalidClientCertificate.cannotJoin
assert sharedRegistryPreservesUDPMapping
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Переиспользовать coder/websocket, TLS/mTLS и общий реестр. Endpoint /tunnel/bond, subprotocol quic-lab-bond-v1, первоначальный JSON join с максимумом 4096 байт, затем бинарные записи до bond.Chunk+существующий header. Очередь 32 сообщения на путь; write deadline/cancel не должен блокировать другие пути. HTTPS сохраняет HoL внутри пути и не обещает датаграммную семантику сети. QUIC ALPN для изменённого handshake версионировать, старую несовместимую схему не принимать как новую. cmd/demux поддерживает опциональный HTTPS-listener; не добавлять публичный порт на реальный сервер в рамках задачи.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/bondhttps ./internal/bondquic ./internal/gateway ./mobile ./cmd/demux -count=1 -timeout=120s
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: carry bond sessions over HTTPS WebSocket"`. Не включать соседние незавершённые задачи.

### Task B3: Наблюдение прогресса и выбор профиля

**Files:**
- Create: `internal/pathpolicy/policy.go`, `internal/pathpolicy/health.go`, `internal/pathpolicy/policy_test.go`, `internal/pathpolicy/health_test.go`.
- Modify: `internal/bond/session.go`, `mobile/gateway_bond.go`; `docs/max-availability.md`.

**Interfaces:** type Observation struct { PathID string; At time.Time; PendingBytes, AckedBytes uint64; ProbeOK, DataProbeOK bool }; type Decision struct { PathID, ProfileID, Action, Reason string }; type Policy struct; func New(c vpnmodel.Config) *Policy; func (p *Policy) Observe(o Observation); func (p *Policy) Next(now time.Time) []Decision. Action: dial/close/promote/probe/recommend_carousel. Ввод доступности сетей отдельным SetNetwork(name string, allowed, available bool).

- [ ] **Step 1 — регрессионный тест:** добавить TestNoFalseFailureWhenIdle; TestSmallProbeCannotMaskStall; TestVolumeBlackhole; TestReserveAndDisabled; TestReturnHysteresis. Минимальные обязательные проверки:

```text
assert idleWithoutPending.doesNotFail
assert tinyProbeSuccessAndDataStall != 'healthy'
assert blackholeAfter(2*MiB).triggersReplacement
assert availableAutoOnLTE.precedesReserveOnWiFi
assert disabledProfile.probeCount == 0
assert reserveWithoutProbe.dialCountBeforeAutoFailure == 0
assert flappingPreferred.doesNotImmediatelyReplaceWorkingPath
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Начальные проектные параметры: data stall=max(3*smoothedRTT,300ms), cap 800ms для подготовленного пути; применим только к ожидающим данным/контролируемой размерной пробе, не к пустому каналу. Healthy return: минимум 8s и 3 успешные пробы. Backoff 1/2/4/8/16/30s с jitter ±20%, сброс после 30s полезного прогресса. Редкая проверка резерва каждые 60s ±20% только при включённой галочке. Малый ping не отменяет data-stall; bounded размерная проба до размера рабочей записи. После 3 восстановлений новым соединением за 10min рекомендовать карусель. Это параметры плана, не SLA для произвольной сети; подтвердить fault-тестами, менять без ослабления 1s/10s приёмки.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/pathpolicy ./internal/bond -count=1 -timeout=120s
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: select VPN profiles using actual data progress"`. Не включать соседние незавершённые задачи.

### Task B4: Пул соединений и интеграция с Android

**Files:**
- Create: `internal/pathpolicy/pool.go`, `internal/pathpolicy/pool_test.go`; `mobile/exit_runtime.go`, `mobile/exit_runtime_test.go`.
- Modify: `mobile/gateway_bond.go`; `android/app/src/main/java/ru/vpnc/quiclab/VpnExitController.kt`, `android/app/src/main/java/ru/vpnc/quiclab/VpnSession.kt`, `android/app/src/main/java/ru/vpnc/quiclab/LabVpnService.kt`, `android/app/src/main/java/ru/vpnc/quiclab/MultipleVpnController.kt`.
- Create: `android/app/src/androidTest/java/ru/vpnc/quiclab/CarouselDeviceTest.kt`.

**Interfaces:** type Pool struct; func NewPool(limit int) (*Pool,error); func (p *Pool) Reserve(profileID, network string) (connectionID string, err error); func (p *Pool) Release(connectionID string). gomobile: type ExitRuntime struct; func NewExitRuntime(sink EventSink) *ExitRuntime; Start(configJSON string, binder SocketBinder) error; UpdateNetwork(networkJSON string, binder SocketBinder) error; Stop(); Snapshot() string. Runtime использует A/B/C API, возвращает JSON события с exit_id/profile_id/path_id/generation.

- [ ] **Step 1 — регрессионный тест:** добавить TestPoolBoundAcrossNetworks; TestReconnectDoesNotResetCellBudget; CarouselDeviceTest; BondDeviceTest. Минимальные обязательные проверки:

```text
assert poolSizeAcrossWifiAndLTE <= configuredSize
assert economyAndWorkingWiFi.cellularRequestCount == 0
assert reserveChecksDoNotStartCarousel
assert readyBackup.recoveryMillis <= 1000
assert availableColdLTE.recoveryMillis <= 10000
assert tcpBytesExactlyEqualBeforeAfterSwitch
assert stoppedGenerationCannotReplenishPool
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Общий пул на профиль, не отдельный на сеть. Очередь connect/Reconnect сериализована; отмена освобождает слот ровно один раз. Native sockets защитить от TUN и bind до connect. Ограничения C1/C2 проверяются перед подключением и probes. Сохранить standalone AWG без пула и без обещания demux; самостоятельно менять физическую сеть существующим механизмом. Убрать запрет multiple+bond после успешного теста двух выходов и остановки одного. Нужна проверка отключения Wi-Fi и silent blackhole, не только модель callbacks.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/pathpolicy ./internal/bond ./mobile -count=1 -timeout=120s; Android: CarouselDeviceTest, BondDeviceTest, MultipleLiveTest
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: manage bounded carousel connections per VPN profile"`. Не включать соседние незавершённые задачи.

## Проверка плана

- Сопоставить результат задач с матрицей покрытия в плане 00.
- Review Focus распределён по указанным тестам; транспортные тесты не заменяют испытания на Android.
- Никакой шаг этого плана не является разрешением на публикацию или изменение действующего сервера.
