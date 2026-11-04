# Linux client preview

CLI: QUIC/HTTPS, standalone AWG и VLESS TCP + TLS/REALITY или WebSocket + TLS; IPv4, 1–256 выбранных подсетей. Нет GUI, MDM, полного маршрута /0 и автоматического переключения физической сети. Системные DNS и IPv6-маршруты не меняются. Kill switch отсутствует: после остановки VPN трафик снова использует обычную маршрутизацию.

## Запуск

Нужны Linux amd64, /dev/net/tun, iproute2, root и профиль выбранного транспорта. Для QUIC/HTTPS скопируйте client.example.json, замените endpoint на IPv4:port сервера, hostname на TLS-имя, certificate/key/ca на PEM-строки профиля (не пути к файлам). Файл содержит секреты. Пример намеренно не содержит рабочих ключей. Формат AWG/VLESS описан ниже.

```sh
chmod 600 client.json
./quic-lab-client -validate -config client.json
sudo ./quic-lab-client -config client.json
```

Ctrl+C удаляет собственный временный TUN и связанные маршруты. При обнаруженной потере QUIC/HTTPS клиент очищает ресурсы и завершается с кодом 1; обнаружение зависит от таймаутов транспорта. AWG/VLESS публикуют готовность локального движка (`readiness: engine_ready`), которая не доказывает доступность сервера; автоматическое завершение при потере этих серверов пока не реализовано. Журнал выводит состояние и счётчики. `-validate` проверяет структуру, AWG/VLESS-параметры и заданный VLESS CA, но не выполняет сетевую аутентификацию; mTLS-сертификаты QUIC/HTTPS проверяются при подключении.

## Standalone AWG/VLESS

Для AWG используйте `transport: "awg"`, явный внешний IPv4 `endpoint`, `targets` и поле `awg_config` с полным текстом AWG-конфигурации. `AllowedIPs` должны в совокупности покрывать каждую выбранную подсеть. DNS внутри AWG-профиля обязателен для существующей диагностики, системный resolver он не меняет. Поддерживается прежний формат AWG Jc/Jmin/Jmax/S1/S2/H1–H4; произвольные PostUp/скрипты не исполняются.

Для VLESS используйте `transport: "vless"`, явный внешний IPv4 `endpoint`, `targets` и `vless_uri` с импортируемой ссылкой. TLS SNI, WebSocket path/host, REALITY key/short ID берутся из ссылки. Для частного TLS CA допускается PEM в `ca`; REALITY не принимает CA. Пример структуры с заведомо тестовым UUID:

```json
{"transport":"vless","endpoint":"198.18.0.2:443","vless_uri":"vless://11111111-1111-4111-8111-111111111111@vpn.example:443?security=tls&sni=vpn.example&type=ws&path=%2Ftunnel","targets":["192.0.2.2/32"]}
```

Это не рабочий профиль. Числовой endpoint задаёт фактический внешний адрес; hostname ссылки сохраняется для профиля, TLS SNI задаётся отдельно. Избранные подсети не должны захватывать endpoint. Материал другого транспорта (`key`, `certificate`, `awg_config` или `vless_uri`) отклоняется при несовместимой комбинации, а не молча игнорируется. Храните весь JSON с правами 0600.

Клиент отклоняет перекрывающиеся подсети, захват адреса VPN-сервера, конфликты с существующими маршрутами и нестандартные policy rules. Физический интерфейс определяется по маршруту до сервера; сокеты привязываются к нему. После смены сети требуется перезапуск. TUN не получает фиксированного IPv4-адреса.

## Необязательный systemd

Установите бинарник в /usr/local/bin/quic-lab-client, приватный профиль в /etc/quic-lab/client.json (root:root, 0600), приложенный unit в /etc/systemd/system/quic-lab-client.service. После проверки профиля выполните systemctl daemon-reload и systemctl enable --now quic-lab-client. Unit перезапускает процесс при ошибке; он не установлен автоматически на тестовый хост.

## Проверка

scripts/test-linux-client.sh запускается root только на тестовом хосте: две временные network namespace, отдельные сертификаты/ключи, реальные TCP/UDP через QUIC/HTTPS/AWG/VLESS TLS (TCP и WS), очистка TUN/маршрутов, сравнение SSH-маршрута хоста. Пользовательская сеть хоста не захватывается. REALITY не входит в эту локальную TLS-матрицу. Сводный дальнейший план: [все клиенты](client-roadmap.md).
