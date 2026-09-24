# VPN gateway: план реализации

VPN дополняет существующий echo-режим; его UI, метрики и сервер сохраняются.

- Android 12+, IPv4; отдельный foreground VpnService и экран.
- QUIC streams / HTTPS WebSocket + smux v2; общий протокол прокси.
- all apps / only selected / exclude selected / network routing (IPv4 CIDR).
- mTLS: CA клиентов на exit node; PKCS#12 импорт на Android; gateway в одном процессе с echo, на отдельных listeners.
- TCP и DNS в первом сквозном сценарии; UDP DATAGRAM требует отдельной проверки.
- Exit node разрешает только явно заданные подсети назначения; доступ к домашним сетям обеспечивает администратор.
- Домашний DNS, пересечение подсетей и IPv6 вне текущего объёма.
- QUIC мигрирует существующий сеанс; HTTPS восстанавливает туннель для новых соединений.
- После окончательного разрыва туннеля прежние прокси-соединения закрываются.
- Демонстрационная загрузка: ограниченная скорость, ID запроса, журнал завершения/обрыва.
- Тесты: обе транспортные реализации, mTLS rejection, ACL, TCP half-close, остановка, маршруты; сборка Android и регрессия echo.

## Реализовано и проверено

Ветка `codex/vpn-gateway`: общий сервер echo + mTLS gateway, QUIC streams и WSS/smux, Android VpnService, четыре режима IPv4-маршрутизации, импорт P12, TCP/DNS, SOCKS5 для desktop, тестовая загрузка.

Проверены: Go tests/vet на Windows, Linux race tests, TCP half-close, полный ответ после миграции QUIC, mTLS/ACL rejection, реальный TCP и DNS через тестовый TUN, завершение TUN. Android APK и lint собираются.

До стабильного релиза: системный VPN consent, split tunnel и route mode на телефоне; Wi-Fi/LTE и фоновые ограничения Android. Instrumentation-тест маршрутов подготовлен, но без подключённого устройства не запущен.

Произвольный UDP/DATAGRAM и возобновление прокси-сеансов после окончательного разрыва туннеля остаются следующим этапом. IPv6 и домашний DNS исключены по согласованному объёму.
