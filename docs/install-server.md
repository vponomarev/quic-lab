# Установка сервера одним скриптом

Поддерживаемые системы: Debian 12/13 и Ubuntu 22.04/24.04 с systemd.
Архивы linux-amd64 и linux-arm64 содержат статический Go-бинарник, установщик
и скрипт публикации APK. Go, Git и сборка на целевом сервере не нужны.
Нужен Python 3: на минимальном образе `sudo apt-get update && sudo apt-get install -y python3`.

## Первая установка

Скачайте архив своей архитектуры и SHA256SUMS из поставки сервера.
Проверьте `sha256sum --ignore-missing -c SHA256SUMS`, затем распакуйте архив:

```sh
tar xzf quic-lab-server-0.4.2-dev-linux-amd64.tar.gz
cd quic-lab-server-0.4.2-dev-linux-amd64
sudo ./install-server.py quic.example.org
```

Замените `quic.example.org` своим DNS-именем. A-запись должна указывать на сервер;
если есть AAAA, она тоже должна вести на этот сервер. HTTP/80 должен быть доступен
из интернета для ACME. TCP/443 и TCP/8443, UDP/4433 и UDP/4434 нужны клиентам.
Установщик не меняет firewall и не затрагивает правила SSH.

Установщик ставит nginx, certbot, CA и OpenSSL из репозиториев дистрибутива,
добавляет отдельный сайт, получает сертификат Let's Encrypt через webroot и
включает продление. Запуск означает принятие [условий Let's Encrypt](https://letsencrypt.org/repository/).
По умолчанию учётная запись ACME создаётся без email; можно указать
`--email admin@example.org`. [Метод webroot Certbot](https://eff-certbot.readthedocs.io/en/stable/using.html#webroot)
не требует остановки существующих сайтов.

Если `/etc/quic-lab/admin.json` отсутствует, генерируются случайные логин и пароль.
Они сразу выводятся в терминал и сохраняются в `/etc/quic-lab/admin-credentials.txt`
с правами root:root 0600. Конфиг также имеет права 0600. При ошибке получения
сертификата данные уже сохранены: исправьте DNS/доступность порта и повторите команду.
При повторной установке пароль не меняется и повторно не печатается.

После установки откройте `https://quic.example.org/lab/`: публичный QR настраивает
Echo; вход в админку позволяет выпускать mTLS-профили VPN.

По умолчанию аутентифицированным VPN-клиентам разрешены все IPv4-направления
(`0.0.0.0/0`), включая внутреннюю сеть сервера. Для ограниченного шлюза при первой
установке задайте, например, `--gateway-allow 192.168.50.0/24,1.1.1.1/32`.
В дальнейшем редактируйте `/etc/quic-lab/server.env` и перезапускайте службу.
Android-маршруты и выбор приложений задаются отдельно в клиенте.

## Уже имеющийся сертификат

```sh
sudo ./install-server.py quic.example.org \
  --cert /etc/ssl/quic/fullchain.pem --key /etc/ssl/quic/privkey.pem
```

Скрипт проверяет имя, срок сертификата и соответствие ключу. В этом режиме
сертификат не запрашивается и его продление остаётся за вашим инструментом.
После обновления PEM выполните `sudo nginx -t && sudo systemctl reload nginx`
и `sudo systemctl restart quic-lab`. Режим и пути сохраняются для следующих запусков.

## Файлы и обновление

- `/opt/quic-lab/quic-lab-server` — бинарник; `.previous` — предыдущая версия.
- `/etc/quic-lab/admin.json` — настройки сайта/профилей и текущие учётные данные.
- `/etc/quic-lab/admin-credentials.txt` — первоначальные учётные данные;
  после ручной смены пароля актуальным источником является `admin.json`.
- `/etc/quic-lab/server.env` — серверная ACL `GATEWAY_ALLOW`.
- `/etc/quic-lab/install.json` — DNS-имя и пути TLS для установщика.
- `/var/lib/quic-lab` — пользователи, CA и ключи. systemd управляет владельцем
  через DynamicUser/StateDirectory; не меняйте права вручную.
- `/etc/nginx/sites-available/quic-lab`, `sites-enabled/quic-lab` — отдельный сайт.
- `/etc/systemd/system/quic-lab.service` — echo, VPN, админка и demo одним процессом.

Распакуйте новый архив и повторите `sudo ./install-server.py quic.example.org`.
Конфиг, пароль, ACL и пользователи сохраняются. Управляемые unit и nginx-файл
обновляются: свои systemd-настройки храните в drop-in (`systemctl edit quic-lab`).
Не меняйте порты backend в drop-in без согласованного изменения nginx.
Если новая служба не проходит проверку запуска, предыдущие бинарник и unit
восстанавливаются. Обновления/продление сертификата перезапускают службу и разрывают
текущие сеансы. Смена DNS-имени — отдельная ручная миграция.

Сохраните `/etc/quic-lab` и `/var/lib/quic-lab` в защищённую резервную копию:
в них находятся пароли, CA и закрытые ключи клиентов.

## Сосуществование с ручной установкой

Скрипт не перезаписывает чужой `quic-lab.service`, сайт nginx или renewal hook,
а также отказывается добавлять второй nginx-сайт с тем же DNS-именем.
Для уже установленного вручную стенда используйте ручное обновление бинарника
или заранее перенесите старую установку: сохраните unit/nginx/hook и данные,
освободите указанные имена файлов и проверьте порты 8081–8083, 4433/4434 и 8443.
Существующий `admin.json` принимается только с тем же public_url, loopback 8083
и data_dir `/var/lib/quic-lab`; остальные настройки он не перезаписывает.
Переезд не выполняется автоматически. Другие сайты nginx остаются на месте.

## APK и проверка

Можно сразу добавить `--apk ./quic-lab.apk` при установке. Позже:

```sh
sudo ./publish-apk.sh ./quic-lab.apk /var/lib/quic-lab/downloads/quic-lab.apk
systemctl status quic-lab --no-pager
sudo journalctl -u quic-lab -n 30 --no-pager
sudo nginx -t
curl https://quic.example.org/lab/
sudo certbot renew --dry-run
```

APK появляется под QR на `/lab/`. Порты 8081–8083 доступны только через loopback.
Для VPN не нужны TUN, ip_forward или NAT на сервере: это TCP/DNS proxy gateway.

## Сборка поставки из репозитория

На машине сборки с Go из go.mod и Python 3:

```sh
python3 scripts/build-server-release.py --version 0.4.2-dev
```

Архивы обеих архитектур и SHA256SUMS появляются в `artifacts/server-release/`.
Можно ограничить сборку `--arch amd64` или `--arch arm64`. Для запуска прямо
из исходников: `sudo python3 scripts/install-server.py quic.example.org --binary ./bin/quic-lab-server`.

## Проверки поставки

Установка и повторный запуск проверены в образах Debian 12/13 и Ubuntu 22.04/24.04
с реальными apt-пакетами, nginx, OpenSSL и сервером. Проверены права файлов,
сохранность учётных данных и CA, отдельный HTTPS-сайт и откат при ошибке запуска.
В контейнерных тестах systemctl имитируется; unit проверяется systemd-analyze.
Дополнительно на Debian 13 выполнен запуск с настоящим systemd, DynamicUser,
LoadCredential и StateDirectory. ARM64-архив собран, запуск на ARM64 не проверялся.
ACME в этих тестах не запрашивает публичные сертификаты: используется тестовый PEM.

Модульные проверки: `python3 scripts/test-install-server.py` на Linux.
`scripts/test-install-container.py` предназначен только для одноразовых Docker-контейнеров:
в `/test` положите установщик и бинарник; установите python3, openssl, nginx, curl,
systemd, затем запустите тест. На обычном сервере этот тест запускать нельзя.
