# Маршрутизация, DNS и общий бюджет LTE Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Обеспечить отсутствие незапланированного direct fallback и единый LTE-бюджет всех выходов.

**Architecture:** Маршрутизатор продолжает владеть TUN и правилами выходов. Отдельный клиентский ledger учитывает сетевой обмен на одном согласованном уровне, а Kotlin предоставляет сетевую привязку и разрешение разовых служебных операций.

**Tech Stack:** Go 1.26.8, существующий fork quic-go 0.63.0, coder/websocket, AmneziaWG, Kotlin, Android API 30+, JDK 17, Linux/systemd.
**Spec:** [Утверждённая спецификация](../specs/2026-09-30-vpn-product-design.md), утверждена владельцем 2026-09-30.
**Status:** План предложен для проверки; реализация не начата.

## Global Constraints

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

A1–A3; C1/C2 используются B4. C3 включает методы D4 только после их реализации, но проверяется с локальным HTTPS-стендом.

## Review Focus

- Одновременные выходы расходуют последний остаток бюджета (C1).
- Входящий трафик уже находится в сети после исчерпания лимита (C2).
- DNS меняется вместе с физической сетью без случайного fallback (C4).
- Redirect загрузки APK уводит служебное исключение на иной endpoint (C3).
- Остановка одного выхода ошибочно закрывает общий TUN (C4).

## Карта файлов и задач

Пути ниже относительно корня репозитория quic-lab. Новые файлы выделены по ответственности; существующие крупные файлы не переписывать целиком. Общие команды Android и правила baseline — в [плане 00](2026-09-30-phase1-00-roadmap.md).

### Task C1: Общий бюджет и период работы

**Files:**
- Create: `internal/trafficbudget/ledger.go`, `internal/trafficbudget/ledger_test.go`; `mobile/traffic_budget.go`.
- Modify: `android/app/src/main/java/ru/vpnc/quiclab/LabVpnService.kt`; `mobile/gateway_bond.go`.

**Interfaces:** type Class string: user/control/copy/config/apk; type Snapshot struct { Epoch string; Limit, Used, Reserved uint64; Blocked bool }; type Ledger struct; func New(epoch string, limit uint64) *Ledger; func (l *Ledger) Reserve(n uint64, class Class) (ticket uint64, ok bool); func (l *Ledger) Commit(ticket, actual uint64); func (l *Ledger) ObserveReceived(n uint64, class Class); func (l *Ledger) Snapshot() Snapshot. gomobile: type TrafficBudget; NewTrafficBudget(epoch string, limit int64) *TrafficBudget; Snapshot() string. Один экземпляр на LabVpnService.

- [ ] **Step 1 — регрессионный тест:** добавить TestConcurrentReservation; TestSharedEpoch; TestNoOverflowOrDoubleCommit. Минимальные обязательные проверки:

```text
assert reserveConcurrently(2,remaining=100,each=80).successCount == 1
assert reconnectExit.doesNotChangeEpochOrUsed
assert explicitStopStart.changesEpoch
assert duplicateCommit.doesNotDoubleCharge
assert counterOverflow.doesNotUnblock
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Бюджет 0 — без пользовательского ограничения, но счётчики растут. Резервирование защищено mutex; возвращать неотправленный остаток, учитывать фактически переданные байты один раз. Входящие байты наблюдать и прекращать дальнейшее использование LTE на пороге. Общий snapshot для UI, транспортов и загрузчика; не сохранять отдельные независимые лимиты на профилях. Epoch создаёт только запуск всего VPN.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/trafficbudget ./mobile -count=1 -timeout=120s
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: share LTE budget across all VPN exits"`. Не включать соседние незавершённые задачи.

### Task C2: Единица учёта и закрытие LTE-путей

**Files:**
- Create: `mobile/metered_socket.go`, `mobile/metered_socket_test.go`.
- Modify: `mobile/path_socket.go`, `mobile/gateway.go`, `mobile/gateway_bond.go`, `mobile/websocket.go`; `internal/awg/engine.go`; `android/app/src/main/java/ru/vpnc/quiclab/VpnSession.kt`.
- Create: `android/app/src/androidTest/java/ru/vpnc/quiclab/TrafficBudgetTest.kt`.

**Interfaces:** Внутренние func meterConn(c net.Conn, network string, l *trafficbudget.Ledger, class trafficbudget.Class) net.Conn; func meterPacketConn(c net.PacketConn, network string, l *trafficbudget.Ledger, class trafficbudget.Class) net.PacketConn. transport callback onBudgetBlocked(epoch string) закрывает все LTE пути и запрещает новые. user/control/copy статистика отдельно от total wire ledger.

- [ ] **Step 1 — регрессионный тест:** добавить TestSocketAccountingOnce; TestBudgetClosesEveryCellPath; TrafficBudgetTest. Минимальные обязательные проверки:

```text
assert totalCountsReadAndWrittenSocketPayload
assert overlayAndSocketCountersNotAddedTogether
assert QUICAndHTTPSAndAWG.shareOneLedger
assert budgetBlocked.noNewCellDialsOrProbes
assert wifiContinuesAfterCellBudgetExhaustion
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Единый уровень общего счётчика — байты payload внешних socket read/write до/после TLS, включая QUIC/AWG datagrams. Это учитывает QUIC повторы в UDP payload, но не IP/TCP headers и kernel TCP retransmits. Транспортные метрики overlay не складывать с ним. Для AWG подключить meter к внешнему bind, а не inner netstack; HTTPS оборачивать TCP до TLS. Входящий overshoot возможен для уже летящих данных: после порога закрыть LTE, величину сверх порога показать, не заявлять операторский hard cap. Согласовать отказ от старого split-budget bond с общим ledger; server BlockCell посылается best effort, а клиент закрывает путь независимо.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/trafficbudget ./mobile ./internal/awg -count=1 -timeout=120s; Android: TrafficBudgetTest
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: meter and stop cellular traffic across transports"`. Не включать соседние незавершённые задачи.

### Task C3: Ограниченные служебные исключения и разрешение превышения

**Files:**
- Create: `mobile/service_download.go`, `mobile/service_download_test.go`; `android/app/src/main/java/ru/vpnc/quiclab/ServiceTransfer.kt`; `android/app/src/androidTest/java/ru/vpnc/quiclab/ServiceTransferTest.kt`.
- Modify: `android/app/src/main/java/ru/vpnc/quiclab/ProfileImport.kt`.

**Interfaces:** gomobile: func (b *TrafficBudget) GrantTransfer(operationID string) error; RevokeTransfer(operationID string); тип ServiceTransfer; NewServiceTransfer(budget *TrafficBudget, sink EventSink) *ServiceTransfer; Fetch(requestJSON string, binder SocketBinder) error; Cancel(operationID string). requestJSON: id, kind(config/apk), https_url, output_file, device_auth_ref; секреты разрешать через существующее защищённое хранилище, не EventSink.

- [ ] **Step 1 — регрессионный тест:** добавить TestGrantAppliesToSingleOperation; TestNoCrossOriginRedirect; ServiceTransferTest. Минимальные обязательные проверки:

```text
assert noConsentAndExhaustedBudget.downloadBlocked
assert grant('apk-1').doesNotEnableVpnOr('apk-2')
assert redirectToDifferentOrigin.rejected
assert cancelRevokesGrant
assert completedBytes.countInGlobalLedger
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Не предоставлять общий произвольный bypass API UI-потокам. Допуск только конфигу и APK из проверенного профиля. HTTPS identity проверяется, cleartext и cross-origin redirects запрещены; новые адреса требуют проверенной конфигурации. Физическую сеть bind/protect отдельно от VPN. Если лимит достигнут посередине загрузки — приостановить/отменить, запросить согласие и безопасно перезапустить или продолжить только с проверенным validator. Согласие относится к operationID и прекращается при завершении/отмене, а не снимает бюджет.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./mobile -run 'Service|Transfer|Grant|Redirect' -count=1; Android: ServiceTransferTest
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: gate direct service downloads with per-operation consent"`. Не включать соседние незавершённые задачи.

### Task C4: DNS, IPv6 и независимые блокировки

**Files:**
- Create: `internal/routing/dns.go`, `internal/routing/dns_test.go`; `android/app/src/main/java/ru/vpnc/quiclab/VpnDnsPolicy.kt`; `android/app/src/androidTest/java/ru/vpnc/quiclab/DnsRoutingTest.kt`.
- Modify: `internal/routing/policy.go`, `internal/routing/policy_test.go`; `mobile/multiple.go`, `mobile/multiple_test.go`; `android/app/src/main/java/ru/vpnc/quiclab/LabVpnService.kt`, `android/app/src/main/java/ru/vpnc/quiclab/MultipleVpnPlan.kt`.

**Interfaces:** type DNSPolicy struct { Mode, ExitID string; Servers []netip.Addr }; func ValidateDNS(p DNSPolicy, exits []string) error; Mode=tunnel/system. Kotlin VpnDnsPolicy.snapshot(network: Network?): JSONObject. MultiRouter NewMultiRouter сохраняет shim, новый NewMultiRouterWithDNS(raw, dnsJSON string, owner FlowOwner, directBinder SocketBinder, sink EventSink) (*MultiRouter,error).

- [ ] **Step 1 — регрессионный тест:** добавить TestTunnelDNSNoFallback; TestSystemDNSNetworkChange; TestUnknownUIDBlocked; DnsRoutingTest. Минимальные обязательные проверки:

```text
assert defaultDNS.mode == 'tunnel'
assert unavailableDNSExit.requestsNeverReachDirect
assert selectedSystemDNS.followsPhysicalNetwork
assert capturedIPv6.doesNotEscape
assert stopOneExit.keepsTunAndBlocksItsRules
assert stopAll.restoresNormalNetwork
```

- [ ] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [ ] **Step 3 — реализация:** Tunnel DNS TCP/UDP53 направлять в один выбранный выход, без переписывания запросов на direct при ошибке. System DNS берётся из LinkProperties выбранной физической сети и обновляется при смене сети; bootstrap разрешение собственных VPN/update endpoints выделить как служебное, иначе невозможен старт при сломанном туннеле. Bootstrap не выдаётся приложениям как fallback. DoH/DoT — обычные потоки. Заблокировать захваченный IPv6 правилами TUN; не включать поддержку IPv6. Сохранить системное нормальное поведение после явного Stop всего VPN.

- [ ] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
Linux: go test ./internal/routing ./mobile -count=1 -timeout=120s; Android: DnsRoutingTest, MultipleLiveTest
```

- [ ] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: enforce explicit DNS policy and exit fail-closed routing"`. Не включать соседние незавершённые задачи.

## Проверка плана

- Сопоставить результат задач с матрицей покрытия в плане 00.
- Review Focus распределён по указанным тестам; транспортные тесты не заменяют испытания на Android.
- Никакой шаг этого плана не является разрешением на публикацию или изменение действующего сервера.
