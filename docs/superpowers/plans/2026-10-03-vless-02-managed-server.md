# VLESS V2 — управляемый Linux-сервер

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Поставить отдельный VLESS-сервис с TLS/REALITY, регистрацией устройств, экспортом URI/QR и подтверждённым отзывом всех соединений устройства.

**Architecture:** Отдельный Linux-процесс использует закреплённый Xray-core и типизированную конфигурацию. Администраторский backend хранит желаемое состояние; приватный локальный канал управления передаёт его worker и получает подтверждение применения. Разрыв при изменении параметров входа допустим; адресный отзыв устройства не прерывает другие устройства. До расширения backend сначала доказать серверный TCP/UDP и границу авторизованного диспетчера.

**Tech Stack:** Go 1.26.8, Xray-core v1.260327.0 без патчей, существующие admin/device admission, Android Kotlin, Linux/systemd.

**Spec:** ../specs/2026-10-03-android-vless-design.md

Статус: пошаговый план подготовлен 2026-10-03 после V0/V1 и pre.5; план утверждён владельцем; задачи 1–3 выполнены, задача 4 в работе. Метод из предыдущих договорённостей: основную интеграцию выполняет основной агент; изолированную задачу можно передать GPT-6.1-sol после фиксации её контракта, без повторных полных обзоров каждого шага.

## Global Constraints

- Android 11+, IPv4, Linux/systemd; Windows, подписки, WS/XHTTP и интеграция VLESS в общую клиентскую сессию остаются V3–V5.
- TCP/RAW + TLS или REALITY, Vision по выбранному профилю. Нативный Xray mux выключен; обработка XUDP не считается разрешением произвольного серверного mux.
- Устройство имеет отдельный случайный VLESS UUID: публичный device ID не становится паролем. Один UUID допускает несколько одновременных соединений с одинаковыми SRC IP/DEST IP/DEST Port и разными SRC port.
- Демультиплексор может находиться на том же сервере. Demux-only разрешает только заданное администратором TCP-назначение; внутренняя TLS-проверка и идентичность устройства сохраняются.
- Отзыв запрещает новые соединения, закрывает все действующие TCP/UDP устройства, отзывает его demux-сессии и обновление профиля. Ошибка/rollback не возвращают отозванный доступ.
- Сохраняется общий admission: по умолчанию 15 устройств, максимум 30. Несколько соединений одного устройства расходуют одно место; VLESS не создаёт отдельный независимый лимит.
- Go/Python и серверные бинарники исполнять на 192.168.5.214. Android build/ADB — Windows. quic-demo использовать только как изолированный стенд с отдельными портами, не менять действующий Xray/3x-ui/VPN.
- Секреты — только защищённые файлы/зашифрованное хранилище; нет произвольного Xray JSON, публичного SOCKS/API, дампов конфигурации в ошибках. Без автоматического production deploy.

## Review Focus

1. UDP/XUDP проходит иным путём Dispatch: аутентификация и отзыв должны действовать на каждом потоке (задача 2).
2. Отзыв во время подключения/rollback/рестарта: запрет сохраняется, включая запоздалый dial (задачи 2–3).
3. Одинаковые UUID/endpoint в параллельных соединениях: второй вход не вытесняет первый, закрытие одного не закрывает соседний (задача 2).
4. Имя demux меняет адрес или XUDP пытается сменить назначение: ограниченный вход не становится прокси (задачи 1–2).
5. Старый Android, прежние QUIC/HTTPS/AWG профили и частичное сохранение админки: совместимость и старый рабочий набор не теряются; новый экспорт не предлагается до применения (задачи 3–5).

## 1. Конфигурация и экспорт одного подключения

**Files:** create `internal/vlessserver/config.go`, `config_test.go`, `export.go`, `export_test.go`.

**Interfaces:** `Config.Validate() error`, `Config.ClientURI(uuid, name string) (string, error)`; Config содержит Listen, Endpoint, Security, ServerName, Fingerprint, Flow, TLSCertificateFile/TLSKeyFile, RealityTarget/RealityServerNames/RealityPrivateKey/RealityShortIDs и Mode/DemuxEndpoint. JSON-имена snake_case. Конфиг worker приватный, не DTO админки. Mode — standalone или demux-only; Endpoint — публичный host:port; Listen — IPv4:port. Структура не исполняет внешние Xray-конфиги.

- [x] RED: `TestConfigRejectsInvalidSecurityAndDestination`: некорректный порт/IPv6, смешанные TLS/REALITY поля, неизвестный mode, demux-only без назначения и неподдерживаемый flow отклоняются. `TestClientURIRoundTrips`: URI TLS/REALITY с пустым flow и Vision принимается `vless.ParseImport`, UUID/SNI/порт/публичный ключ совпадают; приватные ключи отсутствуют.
- [x] Run Linux `go test ./internal/vlessserver -count=1`, получить ожидаемый FAIL до реализации.
- [x] Реализовать валидацию и URI через net/url; публичный REALITY-ключ вывести из приватного X25519, spx=/ соответствует текущей клиентской реализации. Экспорт выбирать с одним допустимым serverName и short ID; не экспортировать неоднозначный выбор.
- [x] GREEN: те же тесты + `go test ./internal/vless ./mobile -run 'Import|VLESS' -count=1`; отдельный коммит контрактов.

## 2. Настоящий сервер и адресный отзыв — технический gate

**Files:** create `internal/vless/server.go`, `server_test.go`, `server_policy.go`, `server_policy_test.go`; modify `internal/vless/revoke.go`, `revoke_test.go`, `integration_test.go`; create `internal/vlessserver/runtime.go`, `runtime_test.go`.

**Boundary:** серверная часть остаётся за отдельным API пакета vless: `StartServer(ctx context.Context, options ServerOptions) (*Server, error)`, `(*Server).Revoke(uuid string)`, `(*Server).Close() error`. ServerOptions — только типизированные настройки входа, клиенты и политика назначений; vlessserver преобразует Config в него. Server не импортирует admin/vlessserver. Сначала проверить API закреплённого Xray для производственной регистрации dispatcher; тестовую регистрацию `emptypb.Empty` не переносить в продукт.

- [x] RED: реальные TLS TCP/UDP round trip, неправильный UUID; `TestServerConcurrentDeviceConnections` создаёт 2 и 5 соединений одного устройства и другое устройство. Закрытие одного слота оставляет соседние; Revoke закрывает все слоты первого устройства и отклоняет новые, второй продолжает передачу.
- [x] RED: `TestServerUDPRevoke` и XUDP-вариант проверяют именно сеть до независимого UDP echo endpoint, а не текущий fixtureUDPDispatcher. Потеря контекста пользователя в Dispatch/DispatchLink должна приводить к отказу, не обходу gate.
- [x] RED: `TestDemuxOnlyPolicy` проверяет разрешённый TCP endpoint, запрет иных IP/портов/UDP/XUDP назначения и отсутствие обхода через имя. Разрешённый IP фиксируется при применении конфигурации; новое DNS-разрешение требует новой ревизии. Проверить закрытие во время dial, лимиты очередей и сохранение последнего ответа.
- [x] Запустить Linux targeted tests, подтвердить FAIL; реализовать Server, авторизованный dispatcher и контроль назначений. Не оборачивать исходный inbound TLS/REALITY Conn: Vision зависит от его типа. Admission привязать к устройству, не к числу транспортных соединений.
- [x] GREEN: `go test -race ./internal/vless ./internal/vlessserver -count=1`; добавить REALITY/Vision на изолированном стенде. Зафиксировать ограничения. Если UDP/XUDP не сохраняет доказуемую идентичность, не переходить к продуктовой выдаче профилей и не объявлять V2 готовым.

**Gate 2026-10-03:** TCP, обычный UDP и XUDP до независимого echo, REALITY/Vision на изолированных loopback-портах Linux, 2/5 соединений, чужой UUID, отзыв и сохранение другого устройства, half-close, ограничение очереди 64 KiB и фиксация demux IP проверены. Контракт позднего dial покрыт существующими `TestSocketRouterClosesEstablishedAndLateConnections`/`TestEngineCloseCancelsRealCoreDial`; общий admission подключается в задаче 4.

**Ограничение XUDP:** адаптер удаляет необязательный GlobalID возобновления перед upstream mux. UDP работает, но ассоциация не переносится между внешними соединениями. Без адаптера тест одинакового GlobalID у разных устройств воспроизводимо ломается; с адаптером проходит.

**Оговорка проверки Vision:** обычный `-race` падает на upstream `unsafe.Pointer`/`checkptr` в Xray VLESS. Остальная матрица проходит без исключений (`-skip TestServerRealityVision`). Полная матрица проходит с `-race -gcflags=github.com/xtls/xray-core/proxy/vless/...=-d=checkptr=0`: race detector остаётся включён; checkptr наших пакетов не отключён. Это не полный чистый checkptr gate. Исходники Xray не изменены.

## 3. Worker, применение ревизий и восстановление

**Files:** create `cmd/vless-server/main.go`, `internal/vlessserver/worker.go`, `control.go`, `worker_test.go`, `control_test.go`.

**Interfaces:** `Snapshot{Revision uint64, Config Config, Devices []Device}`; `Device{ID, UUID string; Expires time.Time; Disabled bool}`. `Worker.Apply(ctx context.Context, snapshot Snapshot) error`, `Worker.Close() error`; `ControlClient.Apply(ctx context.Context, snapshot Snapshot) error`. Админка не получает успех до ответа worker с применённой ревизией.

- [x] RED: новая/повторная/устаревшая ревизия, конфликт одинаковой ревизии, невалидная конфигурация, отказ bind и ошибка записи. Повторные запросы идемпотентны; текущие ключи и конфиг не попадают в ответ.
- [x] RED: revoke → частичный отказ → restart/rollback не воскрешает доступ; отзыв одного устройства не обрывает другие. Повторный запуск не читает несохранённое или устаревшее разрешение как действующее.
- [x] Реализовать приватный Unix control socket и ограниченные сообщения с timeout; доступ только service user/root, без TCP API. Атомарное сохранение желаемой ревизии предшествует применению. Изменение listener/TLS допускает restart; изменения списка устройств используют адресный gate и поддерживаемый upstream user manager.
- [x] Реализовать fail-closed при потере административного канала и ограниченное admission lease по образцу AWG. Успешный отзыв требует подтверждённого закрытия соединений: при недоступном worker возвращать явное «применение не подтверждено», не успех. Проверить раздельно отказ backend и worker.
- [x] GREEN: `go test -race ./internal/vlessserver ./internal/vless -count=1`; Linux процесс SIGTERM освобождает порт, не пишет секреты; отдельный коммит.

**Gate 2026-10-03:** worker сохраняет желаемую ревизию и tombstones перед применением; старые/конфликтные ревизии отклоняются, повтор идентичен. Ошибка записи закрывает runtime и требует рестарта, ошибка bind не откатывает запреты. После рестарта сам файл не запускает listener: требуется свежий Apply backend и admission для каждого потока. Повторное включение отозванного UUID запрещено, требуется новый UUID. Отдельный Linux-процесс проверен: два устройства, адресный revoke, потеря admission, SIGTERM, освобождение порта; полные targeted race (с оговоркой Vision выше) и vet проходят.

## 4. Устройства, админка и получение профиля Android

**Files:** create `internal/admin/vless.go`, `vless_test.go`, `vless_web.go`, `vless_web_test.go`; modify `store.go`, `devices.go`, `protocols.go`, `protocols_web.go`, `device_config.go`, `config.go`, `enrollment.go`, `web.go`; modify Android `ProfileImport.kt`, `ProfileUpdate.kt`, `VpnIdentity.kt`, corresponding instrumentation tests; inspect `internal/protocol` compatibility DTO before adding VLESS.

**Interfaces:** `Store.ConfigureVLESS(*vlessserver.Config, *vlessserver.ControlClient) error`, `Store.VLESSProfile(deviceID string) (string, error)`; существующие выдача/обновление/отзыв являются единственным жизненным циклом устройства. QR VLESS содержит один URI, регистрационный QR продолжает создавать отдельные устройства.

- [ ] RED: два телефона по одному регистрационному QR получают разные UUID; повтор одной регистрации возвращает тот же профиль. Devices/Users metadata и ошибки не раскрывают UUID. Экспорт запрещён отключённому/просроченному устройству и пользователю без VLESS.
- [ ] RED: общий лимит 15/30 учитывает устройство один раз для QUIC/HTTPS/AWG/VLESS; несколько его VLESS-соединений разрешены. Отзыв закрывает VLESS и зарегистрированные demux-сессии, запрещает update; ошибка сохранения не публикует новый профиль.
- [ ] Реализовать хранение UUID, протокол vless, применение worker и выдачу URI/QR после подтверждения. Приватные ключи сервера не передавать клиенту. Админский UI: TLS/REALITY, назначения входа, состояние применения и ошибка без секретов; существующие CSRF/auth проверки обязательны.
- [ ] RED/GREEN Android: получение VLESS через существующее enrollment/update сохраняет каноническую конфигурацию в encrypted identity, сохраняет локальные приложения/маршруты/бюджет; preflight отклоняет несовместимый профиль атомарно. Импорт отдельного QR продолжает работать.
- [ ] GREEN Linux `go test ./internal/admin ./internal/vlessserver ./mobile -count=1`; Android build/lint + импорт/обновление/совместимость. Не объявлять fallback между VLESS и QUIC одного выхода готовым: это V3.

## 5. Поставка и приёмка V2

**Files:** modify `scripts/build-server-release.py`, `scripts/install-server.py`, `scripts/test-install-server.py`, `docs/install-server.md`, `docs/vless-compatibility.md`; create `docs/vless-server.md`. systemd unit генерируется штатным установщиком, а не альтернативным механизмом установки.

- [ ] RED/GREEN installer: отдельный бинарник/unit VLESS, service user, закрытые конфиги/Unix socket, проверка до reload; безопасный rollback и отсутствие восстановления отозванных устройств. Docker не нужен, миграции существующей установки проверяются в Linux-тестах.
- [ ] Изолированный quic-demo стенд: TLS и REALITY/Vision, импорт URI и QR на Android/совместимом стороннем клиенте, TCP/UDP, пять параллельных соединений одного устройства, revoke во время трафика, контроль второго устройства, restart. Действующие сервисы не менять.
- [ ] Полные Linux test/vet + targeted race; Android regression и live VLESS. Незакрытый LTE-сценарий внешнего профиля из pre.5 сохранять отдельно до объяснения причины.
- [ ] Удалить тестовые секреты/службы, записать результаты и ограничения, обновить roadmap. Один итоговый обзор безопасности/интеграции. Релиз/production deployment — отдельный checkpoint после приёмки V2.

## Самопроверка плана

Все требования V2 из спецификации распределены по пяти задачам. V3-пул на клиенте не смешан с поддержкой параллельных соединений сервера. Главная техническая неопределённость — UDP/XUDP и runtime admission; её gate стоит до UI. Начать с задачи 1, затем задача 2; задачи 3–5 зависят от результатов этого gate. Существующая pre.5 остаётся рабочим клиентским релизом на время разработки.
