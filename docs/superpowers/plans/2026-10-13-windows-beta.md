# Windows VPN beta Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task in the current session. Do not launch parallel agents unless the owner chooses that execution method.

**Goal:** Выпустить устанавливаемый Windows 11 x64 VPN beta с QUIC/HTTPS/AWG/VLESS, GUI, профилями и проверенным восстановлением сети, без выбора приложений.

**Architecture:** Общее Go-ядро с платформенно независимым controller и платформенный пакет Windows; привилегированная служба владеет сетью и секретами, непривилегированный WPF UI работает через защищённый named pipe. Android сохраняет адаптер файлового дескриптора; Windows получает Wintun. Системные изменения выполняются транзакционно и имеют журнал восстановления.

**Tech Stack:** Go согласно go.mod (toolchain go1.26.8), существующие gateway/bond/AWG/Xray, Wintun, WPF/.NET; Windows SDK и .NET SDK фиксируются в отдельном global.json после инвентаризации VM.

**Spec:** [Windows design](../specs/2026-10-03-windows-client-design.md), дополнения владельца 2026-10-13.

## Global Constraints

- Windows 11 x64, IPv4. IPv6 не должен давать обход назначенного VPN-трафика.
- Beta включает QUIC/HTTPS, AWG, VLESS TLS/REALITY, два выхода, весь IPv4/подсети, DNS, бюджет и импорт/обновление профиля.
- Выбор приложений, поиск процессов и Roblox — после beta, перед первым стабильным релизом. Сейчас только сохранение возможности расширить модель.
- MDM — фаза 2–3. Исследовательская диагностика, отправка аналитики и расширенный Echo не входят в beta. Эксплуатационные журналы и базовые метрики обязательны.
- Никакого неявного direct fallback для назначенного VPN-трафика; никакого молчаливого отключения проверки TLS.
- Общие Go-тесты/race/vet — Linux 192.168.5.214. Windows cross-build может выполняться там; Windows-runtime тесты — на основном Windows 10 x64 компьютере, разрешённом владельцем 2026-10-14, по этапам и с точечным откатом из дополнения спецификации. Windows 11 остаётся отдельной матрицей.
- Не менять production или телефоны. Не смешивать существующие незакоммиченные изменения Android/research/admin с Windows-задачами.
- VM недоступна. Разрешение владельца 2026-10-14 снимает блокировку W2/W3: допускается основной компьютер. Сначала fake backend/IPC/UI, затем служба и тестовый маршрут с проверенным откатом. Требования snapshot и аварийной перезагрузки в последующих шагах адаптируются по дополнению спецификации; непроведённые проверки остаются открытыми.

## Review Focus

1. Авария между изменением DNS/маршрута и записью результата: восстановление только собственных изменений (W3).
2. Другой пользователь или поддельный pipe: клиент не получает чужие секреты/управление, UI проверяет принадлежность endpoint службе (W2).
3. Изменения сети другим VPN или пользователем: Stop не восстанавливает устаревший полный снимок системы (W3).
4. Отказ одного из выходов, сон, смена IP: нет скрытого direct fallback или сброса бюджета (W4/W6).
5. Вредоносный импорт, несовместимый сервер, downgrade профиля: отказ без потери работающей конфигурации и утечки секретов в журналы (W5).

## Состояние исходников перед исполнением

Существующий linked worktree `.worktrees/phase1-vpn`, ветка `codex/mdm-core`, содержит незавершённые изменения предыдущих задач. Проверены чтением `mobile/gateway_tun.go` (linux tag, unix.Dup, fdbased.Open), `mobile/gateway_tun_other.go` (ошибка на !linux), `cmd/gateway-client/main.go` (только тестовый SOCKS), go.mod (существующие Wintun/Xray/AWG зависимости). Наличие зависимости не доказывает Windows-совместимость. Windows сборка и runtime ещё не проверены.

## W0 — проверка переносимости и минимальное отделение TUN

**Files:** create `scripts/check-windows-build.sh`, `docs/testing/windows-portability.md`, `mobile/gateway_tun_link.go`, `mobile/gateway_tun_link_test.go`; modify `mobile/gateway_tun.go`, `mobile/gateway_tun_other.go` только в необходимой части.

**Interfaces:** сохранить `Gateway.Attach(fd int) error` для Android. Общий внутренний вход `attachLink(link stack.LinkEndpoint, router *MultiRouter) error` владеет принятым endpoint; Android только создаёт endpoint из дубликата FD. Windows-адаптер не подделывает Unix FD. Внешний Windows API определяется по результату сборки, до W3.

- [ ] Записать исходный commit, dirty-file manifest, версии toolchain. Подготовить отдельный каталог исходников на Linux; не собирать поверх занятого стенда.
- [ ] Выполнить Linux baseline `go test ./internal/vpnmodel ./internal/routing ./internal/pathpolicy ./internal/trafficbudget ./internal/gateway ./internal/awg ./internal/vless ./mobile`; при несуществующем имени пакета сверить каталог, не заменять результат успешной проверкой другого пакета.
- [ ] Cross-build по отдельности: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./internal/gateway ./internal/awg ./internal/vless ./mobile ./cmd/gateway-client`. Сохранить реальные ошибки build tags, syscall, gVisor и QUIC fork в отчёте. Не выдавать compile за runtime PASS.
- [ ] RED: `TestAttachLinkClosesOnce`, `TestAttachLinkFailureReleasesEndpoint`, `TestAttachLinkStopWaitsWorkers`, `TestAttachLinkRejectsSecondAttach` с fake endpoint; старые Android TUN тесты остаются регрессией.
- [ ] Отделить создание Linux endpoint от жизненного цикла общей stack/handler. Не переписывать маршрутизацию и атрибуцию Android. Если Windows требует другой packet stack, остановить этот путь и представить конкретные результаты/варианты вместо скрытой замены архитектуры.
- [ ] GREEN: целевой mobile race и Linux baseline; повторить Windows cross-build. Скрипт сборки не исполняет Windows бинарники и не меняет системную сеть.
- [ ] Сохранить таблицу package/build/error/runtime-not-run. Отдельный commit только W0 после проверки diff.

## W1 — переносимая модель службы и транзакций

**Files:** create `internal/vpnclient/controller.go`, `controller_test.go`, `network_plan.go`, `network_plan_test.go`, `journal.go`, `journal_test.go`.

**Interfaces:** `Controller.Start(ctx context.Context, cfg Config) error`, `Stop(ctx context.Context) error`, `Snapshot() Snapshot`; интерфейс `Backend` с `Start(context.Context, Config) error`, `Stop(context.Context) error`. `Config` оборачивает текущую модель профилей, без копирования несовместимой схемы. `Snapshot` содержит state/reason/revision и безопасные метрики, без ключей. `NetworkPlan` — только принадлежащие приложению операции с исходным/ожидаемым значением и порядком отката.

- [ ] RED: повтор Start/Stop, конкурентные Start/Stop, отмена запуска, ошибка backend, закрытие UI без Stop, Snapshot без секретов. Fake backend не меняет сеть.
- [ ] Реализовать состояния stopped/starting/running/stopping/failed; изменения сериализованы, отмена освобождает принятые ресурсы.
- [ ] RED/GREEN транзакций: сбой на каждом шаге, рестарт после незавершённой операции, повторный rollback, чужое изменение после нашего — сохранить чужое значение и сообщить конфликт.
- [ ] `go test -race ./internal/vpnclient -count=1` на Linux. Commit проверенной модели; никакой установки службы на хост.

## L0 — минимальная проверка общего контракта на Linux (вспомогательный этап)

**Files:** create `cmd/vpn-platform-probe/main_linux.go`, `internal/vpnclienttest/backend_linux.go`, `backend_linux_test.go`, `scripts/test-vpn-platform-linux.sh`, `docs/testing/linux-platform-probe.md`.

**Interfaces:** headless probe использует Controller/Backend из `internal/vpnclient` (W1); Linux test adapter реализует тот же backend и packet endpoint W0. Продуктовые Windows-адаптеры остаются в `internal/windowsclient`; Linux не импортирует этот пакет. Модель состояния/счётчиков и отмены общая, GUI/IPC/права ОС — нет.

- [ ] RED/GREEN контрактов: два разных fake backend дают одинаковые переходы состояния, Stop идемпотентен, отменённый Start освобождает ресурс, platform error отображается безопасно. Общий пакет не зависит от Windows SCM/DPAPI/registry или Android FD.
- [ ] Создать headless probe с явными Start/Stop и выводом безопасного Snapshot, без постоянной службы, HTTP listener или desktop UI.
- [ ] На Linux-стенде подготовить отдельный netns/veth/TUN с ограниченными маршрутами; автоматический cleanup только созданных ресурсов. Проверить основной SSH-маршрут до/после. При отсутствии привилегий не выполнять сетевую часть на основном интерфейсе.
- [ ] Через общий packet handler проверить QUIC/HTTPS TCP/UDP, отмену и остановку; сравнить состояния со fake backend. Эти результаты не заменяют Wintun, Windows DNS/фильтры или GUI-приёмку.
- [ ] `go test -race ./internal/vpnclient ./internal/vpnclienttest` и отдельный live netns script на тестовом Linux; записать выполненные/пропущенные сценарии. Commit только общего контракта и стенда.

L0 выполняется после W0/W1, пока готовится Windows VM, если подтверждённо помогает переносу. При необходимости полноценного Linux network manager/installer работу остановить на проверке контракта и продолжить Windows. Linux-релиз не является условием Windows beta.

## W2 — Windows-служба, IPC и минимальное окно

**Files:** create `cmd/windows-service/main_windows.go`, `internal/windowsclient/service_windows.go`, `ipc_windows.go`, `ipc_windows_test.go`, `secrets_windows.go`, `secrets_windows_test.go`; `desktop/windows/QuicLab.Windows/` (WPF project, App, MainWindow, PipeClient), `desktop/windows/global.json`.

**Interfaces:** versioned JSON named-pipe messages `status`, `start`, `stop`, `validate_config`, `apply_config`; request ID, bounded frame 1 MiB, неизвестные команды/версии отклоняются. Pipe ACL — SYSTEM/администраторы и назначенный SID владельца; проверять caller identity. UI проверяет server PID/service identity перед отправкой секретов. Контракт fixture `desktop/windows/protocol-v1.json` проверяется Go и .NET.

- [ ] На VM определить SDK, создать snapshot и проверить SSH/консоль. Закрепить SDK; установка зависимостей только в VM.
- [ ] RED/GREEN: запрещённый SID, переполненный frame, неверная версия, disconnect клиента, отсутствие секретов в ошибках. Проверить реальный pipe под двумя локальными пользователями.
- [ ] Подключить Windows SCM к W1, сохранение секретов через Windows protection + ACL. Тест чужого доступа и повреждённого хранилища.
- [ ] WPF окно на fake backend: импорт/статус/Start/Stop, асинхронный IPC, закрытие/повторное открытие окна. UI не требует постоянного admin.
- [ ] `dotnet build` и тесты контракта на VM; SCM install/start/stop/uninstall в snapshot. Отдельный commit W2.

## W3 — один реальный QUIC/HTTPS VPN и восстановление сети

**Files:** create `internal/windowsclient/tun_windows.go`, `network_windows.go`, `socket_windows.go`, `filter_windows.go`, `backend.go`, соответствующие `_test.go`; modify общий TUN adapter W0; create `scripts/test-windows-network.ps1`.

**Interfaces:** Windows backend реализует W1; адаптер Wintun передаёт packet endpoint в общий обработчик. Сокеты выхода привязаны к выбранному физическому интерфейсу. NetworkPlan управляет только нашим adapter ID, routes/DNS/filter IDs; административный host-only канал стенда сохраняется явно.

- [ ] RED/GREEN fake API: partial failure adapter/routes/DNS/filters, чужие изменения, cleanup после аварии, повторный Stop.
- [ ] Реализовать Wintun lifecycle и transport socket binding без Unix FD; выбранный DNS и bootstrap endpoint не образуют рекурсивный маршрут через TUN.
- [ ] Реализовать блокировку назначенного трафика при отказе; IPv6 не становится обходом в режиме всего трафика. Системную фильтрацию проверить на VM, не называть kill switch готовым по unit-тестам.
- [ ] Live QUIC/HTTPS TCP/UDP/DNS, проверка IP выхода; отказ сервера; остановка/убийство службы на каждом системном шаге; восстановление после перезагрузки VM. Проверить прямой доступ после явного Stop.
- [ ] Зафиксировать таблицу до/после и данные контрольных соединений. Commit W3. Это первый рабочий туннель, ещё не beta.

## W4 — протокольный паритет и два выхода

**Files:** create `internal/windowsclient/profiles.go`, `profiles_test.go`, `networks_windows.go`, `budget.go`, `budget_test.go`; use `internal/vless`, `internal/awg`, существующие gateway/bond/pathpolicy/trafficbudget.

**Interfaces:** Config W1 содержит существующие transport settings, приоритеты выходов и IPv4 CIDR; backend адаптирует их без нового публичного протокола. Standalone AWG/VLESS не выдаются за продолжение QUIC/HTTPS-сессии.

- [ ] RED/GREEN маршрутов: два выхода, пересекающиеся CIDR, выбранный DNS, недоступный выход — блокировка, не direct. Переиспользовать установленные правила приоритетов.
- [ ] Реальные AWG и VLESS TLS/REALITY на стенде: TCP/UDP/DNS, импорт параметров, неверный SNI/сертификат/ключ, перезапуск. Нет незащищённого локального proxy listener для других приложений.
- [ ] Ethernet/Wi-Fi/доступный USB-интерфейс, режимы пула, reconnect, сохранение бюджета после сбоя. VM проверяет виртуальные интерфейсы; физические испытания маркируются отдельно.
- [ ] Go race на Linux + Windows live matrix по четырём протоколам и двум выходам. Commit W4.

## W5 — импорт, безопасное обновление профиля и пакет beta

**Files:** create `internal/windowsclient/import.go`, `import_test.go`; expand `desktop/windows/QuicLab.Windows/` (Profiles, Settings, Status); create `desktop/windows/installer/`, `scripts/build-windows.ps1`, `docs/testing/windows-beta.md`.

- [ ] RED/GREEN: файл/строка/QR-изображение, vless URI, повреждённый и чрезмерный ввод, несовместимая версия; секреты не выводятся в error/status.
- [ ] Обновление профиля: preview различий, подтверждение, сохранение identity; ошибка не разрушает рабочий профиль. Ссылку APK не предлагать Windows-клиенту.
- [ ] GUI: выбранные выходы, transport/network, RX/TX, RTT/jitter, IPv4, бюджет, понятные ошибки. Элементы выбора приложений/MDM/исследования не выдаются за доступные.
- [ ] Установщик: служба, официальный Wintun runtime и лицензии; проверка архитектуры/версии. Install/update/uninstall сохраняют либо удаляют данные явно; не удаляют чужие адаптеры. Проверить свежую VM и обновление установленной beta. Статус подписи приложения сообщается честно.
- [ ] Полные Linux проверки затронутых пакетов, Windows build/runtime/UI matrix и упаковка. Зафиксировать hashes/version/known limitations; номер beta определить по текущей релизной ветке перед публикацией. Commit W5.

## W6 — приёмка и выпуск beta

- [ ] На VM: Start/Stop, close/reopen UI, sleep/resume, отключение сети, restart службы/ОС, DNS/IPv6 isolation, два выхода, лимит бюджета, все четыре протокола.
- [ ] На доступной физической Windows: смена интерфейсов и sleep/resume. Если устройства пока нет — явно указать границу VM-проверки, не объявлять физическую матрицу пройденной.
- [ ] Проверить разрешённый прямой трафик в subnet mode и отсутствие прямого трафика для назначенных VPN-маршрутов при отказе.
- [ ] Финальное ревью, инструкции установки/отката, воспроизводимые артефакты. Выпуск beta не закрывает задачи списка приложений или стабильного релиза.

## После beta

Отдельный план: обнаружение приложений, атрибуция TCP/UDP, список выбранных приложений и контракт маркировки QUIC/HTTPS; затем приёмка Roblox и первый стабильный релиз. Не разрабатывать WFP-атрибуцию/драйвер этой подсистемы до beta под видом подготовительных работ.

## Проверка полноты плана

Требования спецификации распределены: общая платформа W0/W1 и вспомогательная Linux-проверка L0, служба/IPC W2, сеть/откат W3, протоколы/выходы/бюджет W4, импорт/UI/упаковка W5, runtime/release W6. Пять Review Focus имеют проверки в указанных задачах. Отсутствие VM блокирует только фактические Windows-runtime проверки, но не превращает cross-build в подтверждение работы. План ещё не исполнен; ни одна проверка не помечена PASS заранее.

## Дополнение владельца: Linux как архитектурная проверка

Разрешено ограниченное проектирование/реализация Linux-стенда ради правильных общих интерфейсов. Полноценный Linux-клиент значительно позже Windows. Это разрешение на вторую платформу не требует запуска параллельных агентов. Проверка пользовательского Windows-интерфейса остаётся в Windows VM.

## Checkpoint 2026-10-14 — компактное окно без UAC

По дополнительному запросу владельца выполнена часть W5 поверх W2: компактный WPF dashboard, подключение/отключение, TX/RX за соединение и текущая скорость, автообновление и сохранение работы службы при закрытии окна. Runtime-схема и счётчики подготовлены для реального MVP engine. Добавлен opt-in `-engine` службы; реальный SCM+GUI прогон ещё требует установки с UAC и не выполнен в этой итерации. Проверены обычный foreground fixture, IPC, native-тесты и WPF render. Подробные границы и свидетельства: `docs/testing/windows-service.md`, раздел compact dashboard. Это частичный checkpoint, не завершение W3/W5 и не выпуск beta.
