# Research Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Добавить добровольное исследование приложений в собственных туннелях: потоки/DNS, таблицы и JSONL, затем маркированный туннельный PCAPNG.

**Architecture:** Android определяет владельца потока и передаёт ограниченный справочник отдельно от данных. Версионированные управляющие сообщения связывают разрешение, приложение и логический поток; сервер асинхронно записывает наблюдения и экспортирует их. Полный захват изолирован по устройству и использует тот же справочник.

**Tech Stack:** существующие Go/gomobile, Kotlin Android, серверная HTML/JS-админка, JSONL, PCAPNG и Lua/Wireshark; новые зависимости только после проверки имеющихся.

**Spec:** [2026-10-08-research-mode-design.md](../specs/2026-10-08-research-mode-design.md), утверждена владельцем 2026-10-08.

## Global Constraints

- Только трафик собственных QUIC/HTTPS, включая standalone и bond; прямые маршруты, AWG/VLESS исключены.
- Клиентская настройка «Режим исследования» выключена по умолчанию; независима от технической диагностики.
- Серверная политика глобальная/пользовательская; разрешение телефона всегда необходимо.
- Включение/выключение на лету не пересоздаёт VPN и прикладные соединения.
- Потоки/DNS: 60 секунд между промежуточными записями, 7 дней хранения по умолчанию.
- Полный захват: 10 минут или 100 МиБ; лимиты изменяются в процессе; хранение 24 часа.
- Первая версия полного захвата — туннель устройства целиком. Приложение фильтрует анализ, не содержимое сырого файла.
- Приоритет трафика: запись, DNS-парсер, заполнение диска и очередей не блокируют VPN.
- Старый клиент без протокола согласия не даёт разрешение на исследование.
- Экспорт LLM только вручную, JSONL с описанием схемы и сводкой.
- Go-тесты и серверные тестовые бинарники — Linux 192.168.5.214. Android build/ADB — Windows, выделенный Redmi; основной телефон не прерывать.
- Никакой публикации на production до отдельного этапа приёмки/выпуска. Этот план не закрывает оставшиеся VPN release gates.

## Review Focus

1. Отзыв по одному пути и старые сообщения по другому: поколение не откатывается, старая запись не возобновляется (R3/R10).
2. Повторное использование UID/ID после нового процесса или переустановки: приложения/потоки разных сессий не объединяются (R2/R4).
3. DNS через системный резолвер/шифрованный DNS: нет выдуманного исходного приложения или декодированного имени (R6).
4. Исследование на общем серверном порту: чужие пакеты и TLS secrets не попадают в файл устройства (R1/R9).
5. Долгая запись, медленное скачивание и полная файловая система: ограничения памяти действуют, VPN работает, потери явно отмечены (R7/R9/R10).

## Карта файлов и зависимости

Новые файлы ниже — целевая структура, существующие точки интеграции проверены при составлении плана.
- `internal/research/{model,codec,consent,flows,dns,store,export,capture}.go`: отдельные обязанности исследовательской подсистемы, соответствующие `*_test.go`.
- `mobile/research.go`, `android/.../ResearchSettings.kt`, `ResearchController.kt`: клиентский адаптер и разрешение.
- `internal/gateway/research.go`, `internal/bond/research.go`: транспортные адаптеры, не хранилище/UI.
- `internal/admin/research.go`, `research.html`, `research.js`: админка и авторизованный API.
- `internal/debugcapture/research.go`, `research_pcapng.go`, `cmd/capture/quiclab.lua`: изолированный туннельный захват и экспорт.
- `docs/research-mode.md`, `docs/research-jsonl.md`, `docs/research-acceptance.md`: эксплуатация, схема, доказательства.
- Android prefix в задачах: `android/app/src/main/java/ru/vpnc/quiclab/`; device tests: `android/app/src/androidTest/java/ru/vpnc/quiclab/`.

Порядок: R1 → R2 → R3 → R4 → R5 → R6 → R7 → R8 → R9 → R10.
Один связный план: все этапы зависят от общей идентичности и разрешения. В конце R8 возможен отдельный проверяемый checkpoint потоков/DNS без заявления готовности полного захвата.

## Проверки и общие шаги каждой задачи

Каждая задача выполняется циклом:
- [ ] Написать перечисленные тесты и наблюдать их падение на отсутствующем/неверном поведении.
- [ ] Реализовать только указанные интерфейсы и интеграции.
- [ ] Запустить указанную проверку: PASS обязателен; skipped opt-in тест не считается выполненным.
- [ ] Проверить diff на соответствие задаче и сделать отдельный commit указанной темы.

Для Go команды ниже выполняются из актуального снимка репозитория на Linux. Перед тестом сверять переданные файлы с текущим commit/diff; не тестировать случайно старую серверную копию.
Для Android: после изменения mobile API пересобрать AAR через `scripts/build-android.ps1`, затем `android/gradlew.bat -p android assembleDebug assembleDebugAndroidTest lintDebug --console=plain`. Устанавливать app/test APK на выделенный Redmi; запускать указанные instrumented test classes через AndroidJUnitRunner. PASS требует фактического количества выполненных тестов и восстановления рабочего VPN после проверки.

### R1. Технические доказательства до изменения production-протокола

**Files:** создать `internal/debugcapture/research_probe_test.go`, `mobile/research_probe_test.go`, `docs/research-feasibility.md`; читать `internal/gateway/{wire,server,bond,bond_https}.go`, `internal/bond/{session,mux}.go`, `mobile/{gateway_open,multiple,gateway_tun_android}.go`, `cmd/capture/quiclab.lua`.

**Interfaces:** результат — отчёт с картой действительных точек наблюдения и PCAPNG fixtures, не новый публичный API. Fixture manifest перечисляет SHA256, согласия, устройства, время начала захвата и ожидаемые потоки.

- [ ] `TestResearchProbeOwnerCoverage`: провести TCP/UDP через standalone QUIC/HTTPS и bond, определить места получения UID и логического flow ID. Отдельно показать неизвестный UID и активный поток при позднем включении.
- [ ] `TestResearchProbeLateCapture`: начать QUIC и HTTPS до записи, затем включить захват без reconnect, сменить путь; открыть файл tshark с Lua и проверить видимость меток/путей/повторов.
- [ ] `TestResearchProbeDeviceIsolation`: два устройства на одном слушателе, согласие только у одного; assert exportedDeviceIDs == [allowed], foreignTLSSecrets == 0.
- [ ] Запустить `go test ./internal/debugcapture ./mobile -run ResearchProbe -count=1 -v` и tshark на fixtures; сохранить команды/версии и фактические результаты.
- [ ] В отчёте определить уровень счётчиков (payload/IP/туннельные записи) и способ корреляции внешних пакетов после NAT/migration.
- [ ] **Gate:** если поздний разбор либо изоляция не доказаны, не обещать полный трейс и не подменять его синтетической записью незаметно. Описать необходимую поправку спецификации и согласовать её до R2/R9. Не хранить досогласительный полный трафик и не разрывать VPN.
- [ ] Commit: `test: establish research attribution and capture feasibility`.

### R2. Модель, схема и идентичности

**Files:** создать `internal/research/model.go`, `codec.go`, `model_test.go`, `codec_test.go`, `docs/research-jsonl.md`.

**Produces:**
- `FlowKey struct { DeviceID, SessionID string; FlowID uint64 }`.
- `App struct { ID uint64; UID int64; Packages, Labels []string; Attribution string }`; attribution = known/shared_uid/unknown.
- `Record struct { SchemaVersion uint32; Type string; ID string; At time.Time; Generation uint64; Flow FlowKey; Data json.RawMessage }`.
- `EncodeRecord(w io.Writer, r Record) error`, `DecodeRecord(r io.Reader) (Record, error)`.
- Схема v1 типов consent/app/flow_open/flow_delta/flow_close/dns/gap/capture_manifest; внутренние структуры payload определяются здесь и повторно используются далее.

- [ ] `TestRecordRoundTrip`: Unicode labels, shared UID, unknown и максимальные uint64 без потери точности. В JSON ID и большие счётчики передавать десятичными строками, описать это в схеме.
- [ ] `TestSessionIdentity`: одинаковые flow ID/UID при разных SessionID не равны.
- [ ] `TestRecordBounds`: отвергнуть запись >64 КиБ, неизвестную версию, некорректные длины и типы; необязательные неизвестные поля допускаются.
- [ ] Реализовать bounded codec и пример JSONL. Имена/домены — строки данных; HTML/LLM-инструкции внутри них не исполняются.
- [ ] Проверка: `go test -race ./internal/research -count=1`. Commit: `feat: define versioned research records`.

### R3. Разрешение и управляющий канал

**Files:** создать `internal/research/consent.go`, `consent_test.go`, `internal/gateway/research.go`, `internal/bond/research.go`; изменить точки диспетчеризации R1, `internal/gateway/capture.go`, `internal/admin/{capture,user_capture}.go`.

**Consumes:** R2 Record/FlowKey.
**Produces:** `ConsentState.Apply(generation uint64, enabled bool) bool` (принято ли новое состояние), `ConsentState.Allows(generation uint64) bool`; состояние привязано к аутентифицированной device/session, не к переданному клиентом чужому ID.

- [ ] `TestConsentOrder`: Apply(3,false), Apply(2,true) не включает сбор; повтор поколения идемпотентен, конфликт отвергается.
- [ ] `TestConsentReconnect`: разрешение новой сессии неизвестно до handshake, данные старой не включают новую.
- [ ] `TestOldCaptureCannotBypassConsent`: оба прежних пути захвата отказывают устройству без разрешения; Echo вне области этой политики.
- [ ] Согласовать capability, выделить управлению bounded очередь отдельно от пользовательских данных; описать точную границу серверного принятия отзыва и поведение reorder. Отсутствующая capability сохраняет обычный VPN, запрещает исследование.
- [ ] Проверка: `go test -race ./internal/research ./internal/gateway ./internal/bond ./internal/admin -count=1`.
- [ ] Commit: `feat: gate tunnel research by versioned device consent`.

### R4. Android: настройка и привязки приложений на лету

**Files:** создать `ResearchSettings.kt`, `ResearchController.kt`, device `ResearchConsentTest.kt`; изменить `AppSettingsActivity.kt`, `LabVpnService.kt`, `MultipleVpnController.kt`; создать `mobile/research.go`, `research_test.go`, интегрировать владельца в существующие single/multiple точки R1.

**Produces:** `ResearchSettings.enabled(context): Boolean`, `ResearchSettings.setEnabled(context, enabled: Boolean)`; gomobile `Gateway.SetResearchEnabled(enabled bool) error` и `Gateway.SetResearchApplication(flowID string, appJSON string) error`. Gomobile получает десятичную строку flowID; внутренний wire остаётся uint64.

- [ ] Binding test: flowID выше MaxInt64 проходит как строка без потери; некорректная строка отвергается.
- [ ] `defaultOffAndIndependent`: assertFalse(enabled(cleanInstall)); включённая Diagnostics не включает Research.
- [ ] `toggleKeepsConnections`: TCP/UDP продолжаются при off→on→off, TUN и логическая сессия не заменены; события появляются только в разрешённом интервале.
- [ ] `lateEnableAndSharedUID`: старые потоки получают метку неполного начала; shared UID сохраняет список пакетов; ошибка определения не блокирует поток.
- [ ] При off очистить очереди исследовательской метаинформации, послать revoke отдельно; не очищать обычную диагностику и не закрывать VPN. Не сканировать все установленные приложения.
- [ ] Проверить `go test -race ./mobile -run Research -count=1`, AAR/APK/lint и ResearchConsentTest на Redmi.
- [ ] Commit: `feat(android): add opt-in live research attribution`.

### R5. Учёт логических потоков

**Files:** создать `internal/research/flows.go`, `flows_test.go`; интеграция `internal/gateway/research.go`, `internal/bond/research.go`, `mobile/research.go`.

**Produces:** `FlowTracker.Observe(key FlowKey, generation uint64, direction string, bytes, packets uint64, at time.Time)`, `FlowTracker.Flush(at time.Time) []Record`, `FlowTracker.Close(key FlowKey, reason string, at time.Time) []Record`.

- [ ] `TestFlowMinuteDeltas`: 59s — нет периодической записи, 60s — delta, close — финальная delta без повторного суммирования.
- [ ] `TestBondCopiesNotDoubleCounted`: две транспортные копии одной логической записи учитываются один раз; настоящая новая логическая передача учитывается.
- [ ] `TestLateEnable`: старый поток имеет observed_from, unknown start и только новые счётчики.
- [ ] `TestAttributionArrivesLate`: неизвестный владелец обновляется по flow key; данные другой сессии не подмешиваются.
- [ ] Реализовать наблюдение на уровне, доказанном R1; отдельно задокументировать измеряемые bytes/packets и транспортные расходы.
- [ ] Проверка: `go test -race ./internal/research ./internal/gateway ./internal/bond -count=1`.
- [ ] Commit: `feat: record interval research flow accounting`.

### R6. DNS-события

**Files:** создать `internal/research/dns.go`, `dns_test.go`, fixtures в `internal/research/testdata/dns/`; подключить к R5.

**Produces:** `DNSObserver.Observe(key FlowKey, direction string, transport string, payload []byte, at time.Time) []Record`; вызывается только после consent gate.

- [ ] `TestDNSUDPAndTCP`: запрос/ответ, TTL, все секции, NXDOMAIN, несколько сообщений TCP, split/coalesced сообщения.
- [ ] `TestDNSBounds`: compression loop, обрыв/неправильная длина, TCP сообщение >65535, таймаут сборки; память освобождается, VPN не получает ошибку парсера.
- [ ] `TestDNSAttribution`: системный резолвер остаётся unknown/shared, не приписывать запрос приложению по времени; DoH/DoT не декодировать.
- [ ] Сборка TCP ограничена 128 КиБ на поток и 10s ожидания; глобальная очередь/память ограничены общим budget записи. Сопоставление DNS→IP содержит основание/TTL, не называется фактом назначения.
- [ ] Проверка: `go test -race ./internal/research -run DNS -count=1` и bounded fuzz прогон парсера на Linux.
- [ ] Commit: `feat: add bounded DNS research events`.

### R7. Хранилище, retention и JSONL

**Files:** создать `internal/research/store.go`, `export.go`, их tests; подключить конфигурацию сервера и `cmd/server` точку сборки; документировать настройки `docs/research-mode.md`.

**Produces:** `Store.TryAppend(r Record) bool` (не блокирует), `Store.Query(ctx context.Context, q Query) (Page, error)`, `Store.Export(ctx context.Context, q Query, w io.Writer) error`; Query включает все UI-фильтры, cursor/limit, диапазон UTC; Page — records, next cursor, фактическое покрытие/потери.

- [ ] `TestStoreBackpressure`: остановленный writer + заполненная очередь → TryAppend=false, bounded memory и gap при восстановлении.
- [ ] `TestRetention`: 7 дней потоков против 24 часов capture; квота удаляет старейшее завершённое, не активный чужой файл.
- [ ] `TestExportIdempotency`: duplicate record ID не удваивает данные; JSONL со справочником, schema, manifest/gaps; неполные границы минут не выдаются за точные.
- [ ] `TestStoreCrashAndSlowReader`: оборванная запись восстанавливается как gap, не теряется весь архив; медленный экспорт не держит ingest lock; отмена context завершает скачивание.
- [ ] Начать с append-only сегментов и ограниченного индекса, без загрузки всего периода в память. На приёмке проверить scan latency; если индекс не выдерживает — отдельное обоснование storage dependency.
- [ ] Требовать явные конечные дисковые квоты до включения; не угадывать доступный объём VPS. Администратор задаёт квоты через настройки R8.
- [ ] Проверка: `go test -race ./internal/research -count=1`; fixtures ENOSPC/permission denied, не заполнять production-диск.
- [ ] Commit: `feat: persist and export bounded research history`.

### R8. Админка потоков/DNS

**Files:** создать `internal/admin/research.go`, `research.html`, `research.js`, `research_test.go`; изменить существующие регистрацию маршрутов/навигацию/карточку пользователя.

**Consumes:** R7 Store/Query/Page, R3 политика.
**Produces:** авторизованные `/lab/research`, `/lab/research/records`, `/lab/research/export`, POST `/lab/research/settings`. API вне публичных download handlers; mutation использует существующую CSRF-защиту.

- [ ] `TestResearchPermissions`: без входа нет чтения/экспорта/изменения; чужой device ID не расширяет права; поддельные настройки не обходят client consent.
- [ ] `TestResearchFilters`: таблица и экспорт совпадают по пользователю, устройству, приложению, времени, протоколу, адресу, домену; unknown/shared видны отдельно.
- [ ] `TestResearchHTMLData`: названия/домены с HTML и управляющими символами отображаются как текст.
- [ ] Добавить две вкладки с пагинацией, состояния no consent/no capability/no data/gaps, наследование политики, квоты/retention и ссылку из пользователя.
- [ ] Проверка: `go test ./internal/admin ./internal/research -count=1`; визуально desktop + узкий экран на тестовой инсталляции.
- [ ] Commit: `feat(admin): expose research tables and filtered exports`.
- [ ] Checkpoint: выполнить end-to-end Android→сервер→таблица→JSONL; подтвердить, что полного трейсинга ещё нет, не публиковать его как готовый.

### R9. PCAPNG и управление активным захватом

**Files:** создать `internal/debugcapture/research.go`, `research_pcapng.go`, соответствующие tests; изменить `cmd/capture/quiclab.lua`, добавить `internal/admin/research_capture.go`, tests и UI R8.

**Consumes:** R1 доказанный механизм изоляции/позднего старта, R2 справочники, R3 разрешение.
**Produces:** `CaptureManager.Start(deviceID string, limits CaptureLimits) (string,error)`, `UpdateLimits(id string, limits CaptureLimits) error`, `Stop(id, reason string) error`; CaptureLimits = MaxDuration time.Duration, MaxBytes int64. Экспорт через авторизованный handler, ID не bearer.

- [ ] `TestCaptureDynamicLimits`: defaults 10*time.Minute, 100<<20; увеличение без смены ID/потери данных, уменьшение ниже текущего значения сохраняет/завершает.
- [ ] `TestCaptureConsentAndIsolation`: off/revoke/server disabled/старый клиент запрещают запись, чужие TLS secrets отсутствуют, остановка не прерывает реальный TCP.
- [ ] `TestCaptureLateStartAndMigration`: tshark с Lua видит поток/приложение, пути, повторы; повторить R1 после production-интеграции. Не считать только успешное открытие PCAPNG достаточным.
- [ ] `TestCaptureDiskFailure`: запись завершена с причиной, VPN продолжает передачу, частичный файл имеет признак неполноты.
- [ ] Включить метаданные в сам PCAPNG; экспорт не зависит от БД. UI до старта сообщает «весь туннель устройства», фильтр приложения не обещает изоляции файла.
- [ ] Проверка: `go test -race ./internal/debugcapture ./internal/admin -count=1`; tshark assertions по сохранённым fixtures; QUIC/HTTPS standalone/bond live.
- [ ] Commit: `feat: export device-scoped annotated tunnel captures`.

### R10. Сквозная приёмка, нагрузка и документация

**Files:** создать device `ResearchLiveTest.kt`, `internal/research/acceptance_test.go`, `docs/research-acceptance.md`; обновить `docs/wireshark-capture.md`, `docs/research-mode.md`, `docs/research-jsonl.md`.

- [ ] `TestResearchLiveConsentRace`: revoke при delayed/reordered control, multiple paths, reconnect; old generation не включает запись; без поддерживаемого клиента VPN работает.
- [ ] `TestResearchLiveTrafficPriority`: параллельные TCP/UDP при переполнении очереди/slow disk/экспорте; нет зависания трафика из-за writer, память в заданных пределах.
- [ ] Прогнать матрицу QUIC/HTTPS × standalone/bond × research off/on/full, открыть активные потоки до on, выполнить Wi-Fi/LTE переходы на Redmi. Сохранить результаты фактических передач, а не только RTT.
- [ ] Измерить throughput/CPU/RAM/диск и объём служебных сообщений относительно baseline. Не заменять незакрытые VPN B4/E5/нагрузочные gates исследовательскими тестами.
- [ ] Проверить timeline/часовой пояс, длинный поток за границами часа, пропуски, период после retention; LLM-файл содержит ограничения анализа.
- [ ] Полные затронутые Go suites с race на Linux, Android сборка/lint/instrumentation, Wireshark fixtures; отдельное финальное ревью. Записать точные команды, версии, skips и ограничения.
- [ ] Commit: `test: certify research mode boundaries and exports`.
- [ ] Перед выпуском согласовать конкретный checkpoint/номер версии, включить сервер/APK/диссектор/схему/документы в единый релиз. План сам по себе не разрешает менять production.

## План самопроверен

Покрытие спецификации: разрешение/поколения R3–R4; атрибуция R1–R4; потоки R5; DNS R6; хранение и JSONL R7; UI R8; PCAPNG/лимиты R1/R9; отказоустойчивость и совместимость R3/R7/R10.
Все пять Review Focus имеют конкретные тесты. Подробный wire формат и late-capture механизм намеренно зависят от результатов R1; это gate с проверяемым результатом, не разрешение реализовать произвольный формат.
Исполнение ещё не начато. После проверки плана владельцем выбрать inline либо работу через субагентов.
