# Linux/Windows transport parity и Android-регрессия — 2026-11-04

База `390ce1b`, ветка `codex/windows-client-beta`, существующий worktree `.worktrees/phase1-vpn`. [План](../superpowers/plans/2026-11-04-desktop-transport-parity.md), [сводный roadmap](../client-roadmap.md). Пакет продвигает все три клиента; сервер не развёрнут, установщик Windows не выпускался.

## Изменения

- Linux CLI поддерживает standalone AWG и VLESS TCP TLS/REALITY, WebSocket TLS через существующие движки. `awg_config` / `vless_uri` — явные поля приватного JSON; чужие credentials отклоняются. Числовой IPv4 endpoint, 1..256 подсетей, запрет захвата endpoint/зарезервированных адресов, конфликтов маршрутов и policy rules сохранены. VLESS CA и AWG AllowedIPs проверяются до TUN. Проекция engine JSON содержит только данные выбранного транспорта.
- Linux AWG получил ограниченный IPv4 UDP Bind: upstream StdNetBind не предоставлял Linux-доступ к дескриптору для защиты сокета. Новый адаптер выполняет SO_BINDTODEVICE через существующий Binder внутри RawConn.Control; старые receive callbacks остаются привязанными к закрытому сокету после нового Open. Android/Windows используют прежние реализации.
- Windows принимает подсеть, совместно покрываемую соседними AWG AllowedIPs. Общая проверка интервалов поддерживает границу 2^32 и отклоняет неполное покрытие. Сообщение импорта явно учитывает WebSocket.
- Общий VLESS gateway больше не преобразует сохранённый WebSocket в TCP: сохраняет transport/path/host/SNI и проверяет недопустимые комбинации до преобразования. Legacy TCP с пустым transport сохраняется. Исправление вошло в Android AAR/APK, а также в Windows/Linux binaries.
- Desktop startup использует общий вход четырёх транспортов с TLS roots и проверкой отмены, без HA/underlay. Linux `running/readiness=engine_ready` обозначает готовность локального движка, а не проверенную связь с сервером. AWG/VLESS server-loss detection/reconnect остаются открытыми задачами.

## Проверки и воспроизведения

Журналы и бинарники: `artifacts/desktop-transport-parity-20261104/` (игнорируются Git). Linux source-only каталог `/tmp/quic-android-stability-20261104` на разрешённом `root@192.168.5.214`. 21 изменённый исходник/скрипт совпадает с локальным по SHA256 (`local-source-hashes.txt`, `linux-source-hashes.txt`). Пользовательские профили и ключи не передавались; сетевые fixtures создают временные синтетические credentials.

| Проверка | Результат |
| --- | --- |
| `go test ./... -count=1 -timeout=180s` | PASS всех пакетов после исправления fixture, mobile 80.152s, `final-linux.log` |
| race AWG/Linux/Windows engine | PASS, `full-linux.log`; 1.077s / 1.026s / 1.766s |
| race VLESS без REALITY, затронутые mobile-тесты | PASS 1.658s / 1.048s, `race-qualified.log` |
| vet AWG/Linux/Windows engine/mobile | PASS, `race-qualified.log` |
| Windows amd64 cross-build службы и test binaries | PASS; актуальные бинарники скопированы для native запуска |
| Windows engine native | 33 PASS, 3 явно opt-in live SKIP, `windows-engine-final.log` |
| Windows client native, выбранные DPAPI/IPC/store/settings tests | 10 PASS, `windows-client-final.log` |
| Android AAR arm64+amd64, API30, 16KiB alignment | PASS, `aar-final.log`, процесс exit=0 и создан AAR |
| Android assembleDebug/assembleDebugAndroidTest/lintDebug offline | PASS 1m2s, `android-build.log` |
| Note9, 27 instrumentation классов | **167/167 PASS**, 65.619s, `android-device.log`, без пропусков |

Широкий `go test -race ./internal/vless` аварийно завершился в стороннем `github.com/xtls/reality` из-за checkptr/unsafe pointer arithmetic (`full-linux.log`). Это не зелёный стандартный race-прогон. Отдельно весь VLESS/REALITY race прошёл с `-gcflags=all=-d=checkptr=0` за 1.738s (`reality-race-qualified.log`); race-инструментация включена, checkptr отключён. Обычный Go-прогон всех REALITY-тестов прошёл. Исправление зависимости и стандартная checkptr-приёмка остаются открытыми.

Реальные сетевые проверки выполнялись только внутри двух временных netns:

- QUIC/HTTPS: TCP+UDP, ожидание дольше прежней 30-секундной startup границы, Stop/TUN cleanup, автоматическая очистка после остановки сервера — PASS (`linux-netns-green.log`).
- AWG: TCP+UDP и Stop/TUN cleanup — PASS в том же журнале. Первое выполнение до Linux socket adapter завершилось `VPN transport startup failed`; unit `TestLinuxBoundAWGSocketLifecycle` подтвердил причину `socket protection unavailable` (`socket-red.log`) и затем прошёл race.
- VLESS TLS TCP и WS: TCP+UDP и Stop/TUN cleanup — PASS (`vless-netns-green.log`). Это отдельный повтор обеих VLESS-мод после исправления fixture. Host management route unchanged — PASS.
- Первоначальный VLESS timeout был **UDP-дефектом стенда**: TCP давал ответ, а сервер создавался с клиентской `net.Dialer` factory. Общее ядро разрешает UDP только `serverSockets`. Fixture переведён на серверную factory; защита продукта не ослаблялась. RED: `vless-diagnostic.log`; GREEN: обе VLESS-моды выше.
- REALITY, публичные серверы, Linux systemd/reconnect и физические Windows/Android сетевые переходы этой матрицей не проверены.

RED→GREEN кода: `shared-red.log` воспроизводит потерю WS/принятие неверных полей и отказ Windows split AllowedIPs; `shared-green.log` проходит. `coverage-red.log` и `linux-red.log` фиксируют отсутствие новых APIs, `linux-green.log` — целевые GREEN. В тестовом AWG fixture исправлено отсутствие обязательного DNS. Первый Windows запуск неверно передал некавыченные `-test.v` через PowerShell; он не считается проверкой, успешные финальные запуски используют корректные флаги.

## Артефакты и состояние устройств

AAR собран из локального снимка committed HEAD с наложенными scoped Go-файлами; посторонние dirty go.mod/server/admin/cell изменения исключены. Прежний AAR сохранён как `baseline.aar`. Сборки локальные, без публикации:

| Файл | SHA256 |
| --- | --- |
| `android/app/build/outputs/apk/debug/app-debug.apk` | `1aad257dd385d30c37d8c2b288a3cdf34456c23b615db0102f8ce7e36bbe0695` |
| `android/app/libs/quiclab.aar` | `2ef25d8306f02c8156342c9ecf177d2988035d97d0cc346bc74ef9e8fccbdc70` |
| `artifacts/desktop-transport-parity-20261104/quic-lab-client` | `57d5411c1ec64bb96ced8052b20a45df8573c0f441e0965b241bcd79c742efb6` |
| `artifacts/desktop-transport-parity-20261104/quic-windows-service.exe` | `83f19e84e57f277757437190b37e0a324fd7a45d6a4a43494913ce1f459d8bf4` |

APK установлен через install -r на Redmi Note9 Pro Android11/API30, `dc69eb2c`. После тестов открыта MainActivity, активных служб приложения нет, временное JobScheduler-задание 1364018430 отсутствует. Реальные VPN/MDM/радио не включались. Windows-служба не устанавливалась/не заменялась, host routes/DNS не менялись. Бинарник службы — результат разработки, не обновлённый Setup.exe.

## Ревью, решения и оставшееся

Один свежий read-only reviewer: подтверждённых production Critical/Important дефектов не найдено. Important acceptance blocker — VLESS timeout — устранён исправлением fixture с последующим настоящим TCP/UDP GREEN обеих мод. Повторное ревью не проводилось.

- Решение: автономное inline-выполнение в существующем worktree с PowerShell-журналами, локальный scope commit. Цена — интеграция/публикация остаются отдельными действиями.
- Решение: собственный узкий Linux IPv4 Bind вместо обхода защиты или изменения зависимости. Цена — производительность/GSO пока не измерены; Android/Windows bind не заменяются.
- Решение по declined-to-judge: физические Windows/Android сети, Linux throughput и явно исключённые server-loss/full-tunnel/failover/kill-switch не считаются завершёнными. Цена — дальнейшая системная приёмка и реализация по roadmap. Посторонние файлы не входят в пакет; документацию проверил основной исполнитель.
- Решение по REALITY: не менять стороннюю unsafe-реализацию в этом пакете; сохранить стандартный checkptr FAIL и отдельно qualified race PASS. Цена — полная стандартная race/checkptr-приёмка этой зависимости не закрыта.
- Minor, отложено: добавить явные coverage-тесты однoадресного разрыва и IPv4-mapped IPv6. Текущая реализация отклоняет такие входы, но отдельного воспроизведения этих границ пока нет.
- Minor, отложено: проверить старый receive callback после reopen и доставку новому callback. Текущий тест проверяет close до reopen, успешный reopen и защиту от failed-start утечки; прямой regression generation-isolation ещё не добавлен.
