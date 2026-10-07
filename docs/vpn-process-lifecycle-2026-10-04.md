# VPN: завершение процесса Xiaomi и фоновая работа

Дата: 2026-10-04. Android pre.11/versionCode31, сервер без изменений.

## Причина и границы исправления

На Xiaomi22101316UG/API31 21:39:56 МСК система завершила ru.vpnc.quiclab с reason13 OTHER KILLS BY SYSTEM, description OneKeyClean. Повторный запуск приложения — 22:11:37. Второе заявленное отключение по имеющимся журналам не подтверждено. Сохранённые приватные данные: `%TEMP%/quic-vpn-stop-20261004-230008`.

Код возвращал START_NOT_STICKY при запуске и всех вспомогательных командах. Исправлено: работающая служба возвращает START_STICKY, stop/revoke/failed start остаются остановленными; неактивные вспомогательные команды не оставляют запущенную службу. Добавлено событие system_restart для null Intent. Нет boot receiver, always-on/lockdown не включены, явное выключение сохраняется.

[Контракт Android Service](https://developer.android.com/reference/android/app/Service#START_STICKY) запрашивает восстановление службой Android; это не гарантия против ограничений прошивки или force-stop. Приложение само не получает привилегию изменять системные настройки. В общих настройках добавлены объяснение и ссылка на системную карточку приложения.

## Проверки

- RED на старом APK: VpnServiceRestartTest получил stopIfKilled=true у реально запущенной службы.
- GREEN на pre.11: VpnServiceRestartTest, VpnBudgetRunTest, VpnExitLifecycleTest — OK(6 tests), 6.402s на Redmi Note9Pro/API30. Проверяются старт, exit-ip/move/toggle-profile/повторный start и явный stop. Последний удаляет startRequested и VPN не появляется снова.
- Финальная Android assembleDebug/assembleDebugAndroidTest/lintDebug PASS.
- На старом Redmi с запрещённым автозапуском OneKeyClean 23:11:51 убил исправленный процесс26390; sticky сам по себе не помог.
- После выдачи автозапуска в системном UI повтор OneKeyClean 23:13:39 не убил процесс29692. На старом Redmi дополнительно снято ограничение батареи (ранее «Умный режим»). Следующий повтор очистки на pre.11 сохранил процесс32660 и активную VPN-службу, exit IP проверен.
- Отдельный shell-induced crash (`am crash`) с разрешённым автозапуском не привёл к автоматическому восстановлению на старом Redmi, в том числе при снятом ограничении батареи. Это открытый E5 lifecycle gate; нельзя объявлять универсальное восстановление после смерти процесса проверенным. Попытки run-as kill были отклонены ОС и не считаются выполненным тестом.
- На втором Xiaomi установлен pre.11, автозапуск включён в системном списке и подтверждён checked=true; после запуска23:21:46 QUIC подключён, exit IP подтверждён23:21:47, TCP/UDP потоки проходят. Настройки профиля и выбранных приложений не изменялись. Массовую очистку других приложений на основном телефоне не запускали.

Оба телефона оставлены с работающим VPN. Pre.11 установлен локально, публикация на сайте/GitHub в этой задаче ещё не выполнена.

## Остаток

Найденный сценарий OneKeyClean предотвращён системным разрешением на проверенном Redmi; долгосрочное подтверждение на основном Xiaomi ещё требуется. Гарантия восстановления после аварийного завершения процесса на MIUI не достигнута: исследовать отдельно, сохраняя ручное отключение и отсутствие запуска VPN после загрузки телефона. Автоматический перезапуск создаёт новый период счётчика LTE, как полный запуск службы; перенос бюджета через гибель процесса не реализован и должен быть оценён в этом lifecycle gate.
Независимое узкое ревью: важных регрессий не найдено. Тест проверяет системную restart policy и stop, а не реальное восстановление/revoke/failed-start; эти различия сохранены в отчёте. SHA256 pre.11 APK: f3698ef920fb4bccee58db4bdd9bcbe0ea8ed724a1ea33f01fe86520159b3234.

## Update 2026-10-07

The historical LTE-reset limitation above is superseded by durable checkpoints. Actual SIGKILL + activity reopening, preserved nonzero usage and HTTPS/UDP probes are verified; MIUI unattended restart remains limited. See [release reliability evidence](superpowers/plans/2026-10-07-release-reliability.md).
