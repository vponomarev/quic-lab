# Client Research Scope Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Делегирование — только после выбора пользователем такого способа исполнения.

**Goal:** Три режима клиентского исследования QUIC/HTTPS с отдельным согласием на прямой трафик и единым выходом direct.

**Architecture:** Расширяем существующий MultiRouter: правила VPN определяют выход, независимая политика исследования определяет сбор. Direct использует защищённые локальные сокеты и общий контракт наблюдения. Доставка исследований привязана к разрешённому активному QUIC/HTTPS-контексту, а не к наличию удалённого сервера у direct.

**Tech Stack:** Kotlin/Android VpnService, Go/gomobile, Go HTTP/templates, JSONL.

**Spec:** `docs/superpowers/specs/2026-10-11-client-research-scope-design.md`.

## Состояние на 12.10.2026

Задачи 1–5 реализованы в f52e3c9, 147e8fd и 01b4dec, независимое ревью выполнено. Task 6: Note9 прошёл внешнюю матрицу QUIC/HTTPS × три охвата по LTE, восемь переключений Wi-Fi/LTE, единый WEB→MDM→телефон на тестовой привязке, 25-минутное сравнение, аварийный restart и отказ тестового VPN. Кандидат 0.9.4-pre.3 (59) сохранён для Note12; production не обновлён. Открыты ручная приёмка, отдельный тестовый доступ (ожидает разрешения), многочасовой прогон и восстановление VPN Note9 после ошибки cleanup в instrumentation. Полный orchestrator не обозначается PASS. Точные результаты и ограничения: [отчёт](../../testing/client-research-scope.md). Исходные детальные gates ниже не отмечены массово: полный release gate остаётся открытым.

## Global Constraints

- По умолчанию «Только VPN», новое согласие false; старое согласие не расширяется.
- Анализ работает только при включённом VPN. DoH/DoT не расшифровываются.
- Первый выпуск QUIC/HTTPS; AWG/VLESS и смешанные неподдерживаемые конфигурации явно ограничены.
- Отзыв согласия немедленно прекращает соответствующий сбор без разрыва VPN; история сохраняется.
- VPN-правила не меняются от выбора анализа; отказ VPN не включает direct.
- Серверный анализ только VPN. Источник, маршрут, выход и уровень измерения независимы.
- Go-тесты и сборки только на Linux. Тестовое устройство сначала Note9.
- Не включать в коммиты существующие посторонние untracked-файлы. Production не используется для экспериментов с маршрутизацией.

## Review Focus

1. Старые настройки, повторный MDM apply и перезапуск процесса не расширяют разрешение — задачи 2/5.
2. Неизвестный UID/shared UID и удалённое приложение не вызывают прямой обход VPN — задачи 3/4.
3. Системный DNS принадлежит резолверу, а не обязательно приложению; не выдумывать привязку — задачи 3/6.
4. Смена физической сети и отказ выхода не смешивают счётчики/маршруты — задачи 3/6.
5. Отзыв во время накопления и отправки останавливает новые измерения, сохраняя законно собранную историю — задачи 2/4/6.

## Task 1: Модель выхода в исследованиях

**Files:** modify `internal/research/model.go`, `store.go`, `summary.go`; tests `provenance_test.go`, `summary_test.go`; create `mobile/research_exit.go`, `mobile/research_exit_test.go`.

**Interfaces:** поля JSON `route` (vpn/direct/unknown), `exit_id`, `exit_type` (quic/https/direct/unknown) в данных потока и DNS. Go `research.ExitRef { Route string; ID string; Type string }` с JSON-тегами выше; встраивается в FlowData. Отсутствующее значение читается как unknown для отображения, без переписывания старых файлов. Не менять глобальную идентичность потока при позднем обогащении.

- [ ] Написать `TestResearchExitCompatibility`: старый JSON читается, direct отличается от VPN, одинаковые flow ID разных сессий не объединяются. `TestSummarySeparatesExits`: два выхода одного приложения дают две группы, клиент/сервер не суммируются.
- [ ] Запустить на Linux `go test ./internal/research ./mobile -run 'ResearchExit|SummarySeparatesExits'`; подтвердить RED.
- [ ] Реализовать поля и нормализацию, в summary включить маршрут/выход в ключ группы. Добавить поля фильтра в Query/matches; отсутствующие ID не придумывать.
- [ ] Повторить тесты, затем `go test -race ./internal/research ./mobile`; PASS.
- [ ] Коммит `feat: identify research flows by route and exit`.

## Task 2: Политика охвата и отдельное согласие

**Files:** create `android/app/src/main/java/ru/vpnc/quiclab/ResearchScope.kt`; modify `ResearchSettings.kt`, `MdmModels.kt`, `MdmStore.kt`; create `android/app/src/androidTest/java/ru/vpnc/quiclab/ResearchScopeTest.kt`.

**Interfaces:** `ResearchScope(mode: String, packages: Set<String>)`, modes `vpn_only`, `vpn_plus_apps`, `all_apps`; право `researchDirect` независимо от `research`. `ResearchSettings.effectiveScope(context: Context): ResearchScope`. Хранить отдельные желаемое и эффективное состояния; при отзыве устанавливать локальный барьер поколения политики, чтобы старый MDM desired не включился повторно после выдачи права.

- [ ] Написать `ResearchScopeTest`: default vpn_only/false; список сохраняется; без основного или дополнительного согласия direct не собирается; повторное согласие не оживляет прежнюю политику; MDM pause/remove сохраняет прежнюю семантику полномочий.
- [ ] Выполнить существующий Android instrumentation workflow на тестовом устройстве, зафиксировать RED.
- [ ] Добавить устойчивое хранение и миграцию, атомарный пересчёт эффективного охвата, уведомление наблюдателя вне главного потока. Сохранять список даже если установленных пакетов временно нет.
- [ ] Повторить ResearchScopeTest и существующий ResearchConsentTest; PASS. Отдельно проверить отзыв одновременно с очередью событий.
- [ ] Коммит `feat: add opt-in direct traffic research scope`.

## Task 3: Выход direct и безопасная маршрутизация

**Files:** modify `mobile/multiple.go`, `mobile/research.go`, `mobile/research_stream.go`, `internal/routing/policy.go`, `internal/routing/dns.go`; create `mobile/research_scope.go`, `mobile/research_scope_test.go`; extend `mobile/multiple_test.go`, `mobile/multiple_dns_test.go`.

**Interfaces:** `MultiRouter.SetResearchScope(raw string) error` публикует immutable snapshot охвата; `MultiRouter.SetResearch(observer ResearchObserver)` задаёт единый клиентский наблюдатель. Существующий зарезервированный direct остаётся встроенным выходом, без самостоятельного внешнего сервера. Внутренний адаптер исследований получает ExitRef и идентификатор потока один раз при выборе выхода; поток остаётся привязан к этому выходу.

- [ ] Написать table-driven `TestResearchScopeRouteMatrix`: режимы VPN all/apps/exclude/subnets × три охвата; исследование не меняет routing decision. Добавить unknown UID, shared UID, отсутствующий профиль, отказ VPN, одинаковые серверные адреса у разных профилей.
- [ ] Написать `TestDirectResearchConsentAndDelivery`: прямой TCP/UDP наблюдается только при двух правах; отзыв останавливает новые дельты, не сокет; поток не дублируется старым Gateway observer; отсутствие research receiver не останавливает передачу.
- [ ] Написать `TestResearchScopeDNSRoutes`: явная DNS-политика сохраняется, direct DNS не маршрутизируется в VPN автоматически, неизвестное приложение не получает вымышленную метку. Проверить системный DNS отдельно от app-owned DNS. Если ОС не позволяет разделение системных запросов, зафиксировать ограничение до включения режима в UI; не скрывать его ложной атрибуцией.
- [ ] Запустить Linux `go test ./mobile ./internal/routing -run 'ResearchScope|DirectResearch'`; RED.
- [ ] Расширить существующие выходы/наблюдатель. Разделить выбор маршрута и сбор; ограниченная общая очередь, согласие проверяется на каждом событии. Доставка direct через исследовательский контекст устройства; не создавать фиктивный server_gateway поток и не отправлять payload на сервер.
- [ ] Выполнить `go test -race ./mobile ./internal/routing ./internal/research ./internal/gateway`; PASS. Проверить бюджеты direct, tunnel и служебной отправки раздельно.
- [ ] Коммит `feat: observe direct exit alongside quic and https`.

## Task 4: Android TUN и интерфейс исследования

**Files:** modify `LabVpnService.kt`, `MultipleVpnController.kt`, `MultipleVpnPlan.kt`, `ResearchBridge.kt`, `AppSelectionActivity.kt`; create `ResearchScopeActivity.kt`, register in `android/app/src/main/AndroidManifest.xml`; connect from current diagnostic settings; extend `ResearchScopeTest.kt`.

**Interfaces:** `ResearchCapturePlan` в `ResearchScope.kt`: результат компиляции исходного VPN-охвата и охвата исследования в TUN selection + неизменённые router rules. Дополнительный picker использует отдельный storage key и не пишет VPN apps. Одна общая owner lookup служит маршрутизации и наблюдению, с явным unknown.

- [ ] Добавить тесты union/exclude/subnets, пустого/удалённого пакета, нескольких пакетов одного UID; сохранение списка при отмене и смене режима; несовместимый AWG/VLESS/mixed сообщает причину.
- [ ] Запустить instrumentation и подтвердить RED.
- [ ] Реализовать план TUN, подключение scope и owner к MultiRouter. Не расширять перехват до публикации соответствующих правил. Отзыв меняет observer без пересоздания TUN; смена охвата через «Применить» предупреждает о переподключении.
- [ ] Добавить три режима, отдельный список и отдельное согласие с ясным объяснением прямого пути. Stop прекращает сбор, восстановление процесса восстанавливает только разрешённую конфигурацию. Проверить актуальность текста ResearchSettings.explanation.
- [ ] Выполнить ResearchScopeTest, ResearchConsentTest, ResearchOwnerLookupTest и тесты сохранения конфигурации; PASS. Проверить экран вручную на Note9.
- [ ] Коммит `feat: configure Android client research coverage`.

## Task 5: MDM, серверное хранение и WEB

**Files:** modify `internal/mdm/types.go`, `config.go`, `report.go`, `research_rights_test.go`; `internal/admin/mdm_editor.go`, `mdm_editor.js`, `mdm_editor.html`, `mdm.go`; `internal/admin/research.go`, `research.html`, `research_test.go`; Android `MdmConfiguration.kt`, `MdmApplyCoordinator.kt`, `MdmDeviceReport.kt`, `MdmEnrollActivity.kt`, `MdmTelemetryActivity.kt`.

**Interfaces:** capability `research_scope_v1` в отчёте; MDM configuration `researchScope: {mode, packages}`, право `researchDirect`. Report включает желаемый/эффективный режим и причину ограничения. Исследовательские фильтры `route`, `exit`; export и ссылки сохраняют их. Политика без capability не показывается применённой.

- [ ] Написать MDM-тесты absent right=false, несовместимый клиент, повторная policy revision после отзыва, новая разрешённая revision, конфиг/права независимо. Добавить WEB-тесты фильтра direct, двух VPN-выходов, старого unknown и escaping имён.
- [ ] Linux `go test ./internal/mdm ./internal/admin -run 'Research|Scope'`; RED.
- [ ] Реализовать wire-модель с проверками прав и возможностей, Android enrollment/edit и согласование поколения. Сервер проверяет право при приёме direct-записей; не доверяет заявленному origin. Гонку отзыва и последнего законно собранного batch обработать явно по времени/поколению, а не принимать произвольную клиентскую метку как доказательство.
- [ ] Добавить маршрут и выход в компактные таблицы, группировки, фильтры и JSONL; исключить суммирование разных источников. Показать server-only VPN отдельно от client direct.
- [ ] `go test -race ./internal/mdm ./internal/admin ./internal/research ./cmd/server`; Android MDM/consent regression; PASS. Визуальная проверка HTML на тестовом сервере.
- [ ] Коммит `feat: manage and inspect client research scope through MDM`.

## Task 6: Физическая приёмка и документация

**Files:** create `android/app/src/androidTest/java/ru/vpnc/quiclab/ResearchDirectLiveTest.kt`, `docs/testing/client-research-scope.md`; update `docs/research-jsonl.md` и данный план результатами.

**Interfaces:** instrumentation использует существующий ResearchLiveTest workflow, сохраняет и восстанавливает исходные профиль/согласия/сети. Использует два контролируемых приёмника и реальные счётчики; при отсутствии подходящей сети тест отмечается непроведённым, а не успешным.

- [ ] Подготовить на Linux тестовый сервер и TCP/UDP/DNS приёмники. Записать версии/конфигурацию без секретов; собирать только разрешённый тестовый трафик.
- [ ] Note9: QUIC и HTTPS × три режима; приложение VPN и приложение direct имеют ожидаемые egress IP; direct отсутствует в server_gateway; WEB/JSONL показывает оба клиентских потока с exit ID. Контроль объёмов и DNS вопрос/ответ.
- [ ] Проверить отзыв во время передачи, повторное разрешение без команды, Stop/Start, гибель процесса, недоступный сервер, заполненную очередь, Wi-Fi/LTE, IPv6 и системный DNS. Зафиксировать продолжительность задержек и память относительно режима vpn_only.
- [ ] Выполнить Linux race regression всех затронутых пакетов и Android instrumentation; просмотреть diff на обход VPN, расширение согласия и двойной учёт. Исправить найденные дефекты и повторить только затронутые проверки.
- [ ] Обновить документацию и результаты приёмки; коммит `test: verify direct research scope on Android`.
- [ ] Перед production подготовить совместимые сервер/APK, резервную копию и откат; публиковать только после успешной приёмки и согласованного выпуска. Личный телефон не прерывать в рамках ночных тестов Note9.

## Порядок и контрольные результаты

Последовательно 1 → 2 → 3 → 4 → 5 → 6. После 3 — доказанная маршрутизация и счётчики в тестах; после 4 — рабочий локальный режим на Note9; после 5 — управление и просмотр; после 6 — кандидат на выпуск. Реализацию предлагается вести в текущей сессии без постоянного размножения контекстов, с отдельным итоговым ревью после выбора способа исполнения.
