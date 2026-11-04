# Windows per-app TUN/direct — 2026-11-04

База `bd0429a`, ветка `codex/windows-client-beta`, существующий worktree
`.worktrees/phase1-vpn`. [План](../superpowers/plans/2026-11-04-windows-per-app-routing.md),
[дизайн](../superpowers/specs/2026-10-14-windows-per-app-tun-design.md).
Это первый opt-in пакет, а не завершённая физическая приёмка per-app.
Установленная Windows-служба, пользовательские маршруты и профили не изменялись.

## Реализация

- Политика `app_routing` версии 2 отдельно от старой WFP-политики. Два независимых
  списка полных путей: только выбранные через VPN / все, кроме выбранных.
  Неизвестный или смешанный владелец: VPN по умолчанию, явно direct либо block.
  Общие владельцы с одинаковым решением используют согласованный выход.
- IPv4 TCP: полная пара endpoint; UDP: все подходящие локальные endpoints,
  включая wildcard. PID 0, недоступный процесс, неоднозначность и отсутствие записи
  не превращаются в угаданное имя. Каждый lookup проверяет время создания процесса
  относительно снимка таблицы и канонический путь; кэша PID/решений нет.
- Windows native lookup: максимум 4 одновременно на весь процесс, 150 мс ожидания,
  таблица до 4 МиБ, до 3 попыток чтения, до 16 PID-кандидатов. Таймаут не освобождает
  слот незавершённого системного вызова; переподключение не создаёт новый лимит.
  Очередь первых потоков — 64, перегрузка учитывается как unknown по явной политике.
- Первый поток выбирается вне packet callback. TCP/UDP сохраняют выход до закрытия;
  ошибка VPN не вызывает direct fallback. Direct использует существующий защищённый
  dialer с привязкой к физическому интерфейсу. Раздельные лимиты потоков и UDP idle
  timeout сохранены. DNS на порту 53 в захваченной области всегда через VPN.
- Runtime сообщает direct TX/RX, resolved/unknown/shared/blocked/overload/timeout/DNS.
  Основные TX/RX учитывают VPN, включая прежнюю ICMP-политику. Имена exe/PID на сервер
  не передаются. IPv6 и область маршрутов не расширяются.
- Служба объявляет `app-routing-tun-v2`, проверяет политику и пути до применения/
  активации. Старый сервис отклоняет новое поле, UI блокирует включение без capability.
  Старые настройки без per-app остаются совместимыми. Удалённый exe не блокирует
  запуск settings IPC: список можно исправить или выключить; активация stale-правил
  отклоняется. Статистика безопасно публикуется параллельно Start/Stop.
- WPF: вкладка приложений, существующий редактор двух списков, импорт старого черновика,
  unknown policy и описание области захвата. Сохранение работающего VPN использует
  прежнее явное подтверждение переподключения.

## Проверки

Артефакты: `artifacts/windows-per-app-20261104/`.

- Linux source-only стенд: `go test ./... -count=1 -timeout=180s` — PASS.
  Race: approuting/windowsengine/windowsclient/vpnclient и целевые desktop,
  MultiRouter, attach/TUN тесты — PASS. Linux/Windows targeted `go vet` — PASS.
- Реальные TCP и UDP payloads через QUIC/HTTPS и direct, UDP-ответ 30 КБ,
  независимые счётчики, binder, global DNS, отсутствие fallback, saturation/stop — PASS.
  Полный userspace TUN с TCP/UDP пакетами через оба выхода — PASS.
- Нативные Windows executables: owner/policy **15 PASS**, engine **35 PASS / 3 SKIP**
  (только opt-in live network), selected settings/IPC/storage **12 PASS**.
  TCP loopback и UDP wildcard действительно определены как текущий процесс.
- WPF build: 0 ошибок/предупреждений. Полный fake-service acceptance — PASS,
  включая capability gate, оба списка, сохранение/reload и описание захвата.
  Self-contained Release win-x64 опубликован локально в каталоге `ui/` артефактов;
  это dev-сборка интерфейса, не установщик и не обновление установленной службы.
- Android AAR arm64/amd64, APK/test APK, lint — PASS; Note9 `dc69eb2c`, API 30:
  **167/167 PASS**, 66.743 с. После тестов MainActivity открыт, активных VPN/MDM-служб
  нет, JobScheduler 1364018430 отсутствует (история выполненного теста сохраняется).
- Service exe SHA256: `0620e1e2ec1b07b37b6601f26091d687c537382814b1d5d4417e5cb68847f72a`.
  Установленный debug APK SHA256: `7510fd0971852cca738572f91b025c938daee15621b36242ab4c96a6cb5b0da8`.
  Все 27 изменённых Go-исходников сверены по SHA256 между worktree и Linux-стендом.

## Ревью и решения

Один fresh-context reviewer, без повторного ревью. Три Important исправлены за один
проход, каждый с наблюдаемым RED → GREEN:

1. Гонка status/Stop: перенесена та же схема публикации в проверяемое общее состояние;
   `TestAppRoutingStatusConcurrentRestart` сначала дал race FAIL, затем PASS с mutex.
2. Удалённый exe ломал запуск службы: `TestSettingsRemovedExecutableKeepsRepairIPC`
   сначала FAIL, затем PASS для включённой/выключенной политики и запрета активации.
3. Реконструкция resolver сбрасывала native bound:
   `TestWindowsOwnerLimitSurvivesResolverReconstruction` сначала FAIL, затем PASS.

Minor замечаний нет.

- Решение: первый пакет применяет настройки с явным переподключением, без hot apply.
  Цена — текущие VPN/direct-потоки прерываются; при автоматическом восстановлении
  транспорта direct также перезапускается. Независимый VPNExit остаётся в roadmap.
- Решение: не кэшировать PID/решения. Цена — больше работы на первый поток; нужны
  измерения перед масштабированием.
- Решение по declined-to-judge: физический захват Windows, два приложения с разными
  внешними выходами и Roblox не объявляются пройденными. Цена — отдельный системный прогон.
- Решение по declined-to-judge: production first-flow latency, CPU, память и устойчивое
  число сокетов ещё не измерены. Цена — нагрузочная приёмка остаётся открытой.
- Решение по declined-to-judge: физическая per-app матрица AWG/VLESS не закрыта
  прежними транспортными тестами. Цена — отдельная приёмка каждого транспорта.

Windows contracts: [TCP owner table](https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getextendedtcptable),
[UDP OWNER_PID](https://learn.microsoft.com/en-us/windows/win32/api/udpmib/ns-udpmib-mib_udprow_owner_pid),
[process creation time](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getprocesstimes).
