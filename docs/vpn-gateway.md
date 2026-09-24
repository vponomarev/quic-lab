# VPN gateway (ветка разработки 0.4)

VPN дополняет echo. Это первая реализация для проверки на устройстве, не стабильный релиз.
Исходный экран, метрики и адреса echo не меняются. Один Go-процесс обслуживает оба режима.

## Что реализовано

- Android 12+, IPv4, отдельный VPN-экран и foreground service.
- QUIC: независимые streams, сохранение туннеля при миграции пути.
- HTTPS: WebSocket поверх TLS/TCP, smux v2 с ограничением буферов; новый туннель после разрыва, старые TCP-соединения закрываются.
- TCP и DNS (UDP/53). Произвольный UDP пока **не поддерживается**, приложения внутри VPN должны использовать TCP; браузер может перейти с HTTP/3 на HTTP/2 или HTTP/1.1.
- All apps / Only selected apps / Exclude selected apps / Network routing (IPv4 CIDR).
- mTLS для обоих gateway listeners. Echo не требует клиентского сертификата.
- Импорт одного ключа и цепочки сертификатов PKCS#12. На устройстве данные зашифрованы ключом Android Keystore, резервное копирование приложения отключено.
- Не более 128 каналов в туннеле и 1024 каналов на сервере. Обязательный серверный список разрешённых подсетей.

## Сервер

Сначала выполните [базовую установку с Let's Encrypt](deploy-linux.md).
Один сертификат сервера можно использовать для echo и gateway.
Дополнительные параметры существующего `cmd/server`:

```sh
./quic-lab-server \
  -listen 0.0.0.0:4433 -web-listen 127.0.0.1:8081 \
  -cert /path/to/fullchain.pem -key /path/to/privkey.pem \
  -gateway-quic 0.0.0.0:4434 -gateway-https 0.0.0.0:8443 \
  -client-ca /path/to/client-ca.pem \
  -gateway-allow 192.168.50.0/24,1.1.1.1/32 \
  -demo-listen 127.0.0.1:8082
```

Откройте UDP/4434 и TCP/8443. Echo остаётся на прежних портах.
HTTPS gateway сам завершает TLS и проверяет клиентский сертификат: nginx не терминирует этот listener.
Это позволяет не передавать доверенную личность клиента через HTTP-заголовки.
Системный unit можно дополнить drop-in: передать CA через `LoadCredential`, заменить `ExecStart` на команду с gateway-параметрами, сохранив прежние echo-параметры и `${CREDENTIALS_DIRECTORY}` для файлов.

`gateway-allow` — IPv4 CIDR через запятую, обязательно заданный администратором.
`0.0.0.0/0` явно разрешает все IPv4-направления, включая внутренние адреса сервера.
Для домашнего шлюза задайте только свои подсети. Список на телефоне не заменяет серверную ACL.
DNS для режимов приложений тоже должен быть разрешён: по умолчанию клиент использует `1.1.1.1`.
Все доверенные клиентские сертификаты пока получают одну общую ACL; отдельных правил по личности и CRL/OCSP пока нет.
Для отключения потерянного устройства отзовите доверие организационно: замените CA/доверенную группу и перезапустите gateway, либо выдавайте короткоживущие сертификаты.

## Клиентский сертификат

На административной машине с OpenSSL; секретный ключ CA не переносите на exit node или телефон.
Команды запрашивают пароли интерактивно, не помещайте их в историю shell.

```sh
umask 077
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -aes-256-cbc -out client-ca.key
openssl req -new -x509 -key client-ca.key -sha256 -days 365 \
  -subj '/CN=QUIC Lab Client CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' -out client-ca.pem
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out phone.key
openssl req -new -key phone.key -subj '/CN=student-phone' -out phone.csr
printf '%s\n' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature' 'extendedKeyUsage=clientAuth' > client.ext
openssl x509 -req -in phone.csr -CA client-ca.pem -CAkey client-ca.key \
  -CAcreateserial -days 30 -sha256 -extfile client.ext -out phone.pem
openssl pkcs12 -export -inkey phone.key -in phone.pem -certfile client-ca.pem -out phone.p12
```

На сервер передаётся только `client-ca.pem`. На телефон — защищённый паролем `phone.p12`.
После импорта удалите ненужную копию P12 из Downloads; приложение сохраняет собственную зашифрованную копию.
Поле «CA сервера» оставьте пустым при Let's Encrypt. Оно нужно только при частном CA сервера и не является CA клиентов.
Проверка имени и цепочки сертификата сервера всегда включена; insecure-режима в gateway нет.

## Android

1. Откройте «VPN / Exit node» из главного экрана. Echo перед этим остановите.
2. Выберите QUIC и адрес `ваш-домен:4434` либо HTTPS и `ваш-домен:8443`.
3. Укажите TLS hostname без порта, импортируйте P12.
4. Выберите режим маршрутизации, приложения или подсети. Разрешите системный запрос VPN.
5. Начните загрузку в выбранном браузере и переключайте Wi-Fi/LTE.

В Only selected apps пустой список запрещён. В Exclude selected apps пустой список означает все приложения.
Само приложение QUIC Lab исключается из захвата; сокеты туннеля дополнительно защищены через `VpnService.protect`.
Для захваченных приложений IPv6 блокируется, чтобы он не обходил IPv4 VPN.
Network routing сохраняет DNS физической сети; частный DNS и совпадающие подсети не обрабатываются.
Переключение конфигурации требует остановки и нового запуска VPN. Always-on/lockdown в этой версии не поддерживается.

## Демонстрационная загрузка

`-demo-listen` включает отдельный HTTP listener. Проксируйте его через обычный HTTPS-сайт nginx:

```nginx
location /vpn-demo/ {
    proxy_pass http://127.0.0.1:8082/;
    proxy_buffering off;
    proxy_read_timeout 300s;
}
```

Страница `/vpn-demo/` выдаёт файл 128 MiB со скоростью около 512 KiB/s.
Отключите HTTP/3 на этом тестовом сайте. Range-запросы отвергаются; каждый новый запрос имеет новый ID.
В журналах: `download_started`, `download_finished` с `completed`, `gateway_open/closed`, `flow_open/closed`.
Успех миграции — один gateway-сеанс, один исходящий TCP flow и один полностью завершённый запрос.
Сам по себе продолжающийся прогресс браузера не доказывает сохранение TCP.

## Desktop SOCKS для диагностики

```sh
go run ./cmd/gateway-client -transport quic -server your.domain:4434 \
  -cert phone.pem -key phone.key -socks 127.0.0.1:1080
curl --socks5 127.0.0.1:1080 https://your-download-site/vpn-demo/download -o result.bin
```

SOCKS5 CONNECT поддерживает IPv4; DNS разрешает curl локально (`--socks5`, не `--socks5-hostname`).
В HTTPS-режиме замените транспорт и порт. SOCKS слушает только loopback.
Клиентские ключи/сертификаты, P12, журналы и адреса частных стендов не добавляйте в Git.

## Проверки

`go test ./...` и `go vet ./...`; Linux: `go test -race ./mobile ./internal/gateway`.
Тесты создают временный CA в памяти/TempDir, проверяют оба транспорта, отказ mTLS и ACL, TCP half-close, миграцию QUIC и закрытие потоков.
Linux-тест TUN использует Unix socketpair и локальный DNS на IPv4-адресе тестовой машины, порт 53; если порт недоступен, тест явно пропускается.
Android: `scripts/build-android.ps1 -BuildApk`; instrumentation включает `VpnRoutesTest`.
Системное разрешение VPN, split tunnel и реальные Wi-Fi/LTE-переходы требуют отдельного прогона на телефоне.
