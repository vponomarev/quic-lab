# Развёртывание на Linux: QUIC + HTTPS + Let's Encrypt

Пример для готового Debian/Ubuntu с systemd (поддержка LoadCredential), nginx и
установленным Go 1.26+. Используйте собственный домен вместо `quic.example.org`.
A-запись должна указывать на этот сервер; если есть AAAA, IPv6 тоже должен работать.
Пример unit слушает QUIC по IPv4. Для Android предпочтителен доступный A-адрес.

## 1. Пакеты, порты и бинарник

```sh
sudo apt-get update
sudo apt-get install -y nginx certbot git ca-certificates
git clone https://github.com/vponomarev/quic-lab.git
cd quic-lab
go build -trimpath -o bin/quic-lab-server ./cmd/server
sudo install -d -m 755 /opt/quic-lab /var/www/quic-demo
sudo install -m 755 bin/quic-lab-server /opt/quic-lab/quic-lab-server
DOMAIN=quic.example.org  # замените своим доменом
```

Разрешите входящие TCP/80, TCP/443, UDP/4433 в firewall сервера и правилах облака.
Не открывайте TCP/8081 наружу. При использовании UFW: `sudo ufw allow 80/tcp`,
`sudo ufw allow 443/tcp`, `sudo ufw allow 4433/udp`. Это не включает UFW и не меняет
правила SSH. Существующие сайты nginx сохраняйте, добавляя отдельный server block.

## 2. HTTP для проверки домена

Команды ниже выполняются из корня репозитория в одной shell-сессии с DOMAIN.

```sh
sed "s/quic.example.org/$DOMAIN/g" deploy/nginx-quic-demo-http.conf > /tmp/quic-lab-http.conf
sudo install -m 644 /tmp/quic-lab-http.conf /etc/nginx/sites-available/quic-lab
sudo ln -s /etc/nginx/sites-available/quic-lab /etc/nginx/sites-enabled/quic-lab
sudo nginx -t
sudo systemctl reload nginx
curl "http://$DOMAIN/"
```

Если ссылка уже существует, повторять `ln -s` не нужно. Убедитесь, что домен
не обслуживает другой server block. Нужен доступ к HTTP/80 из интернета.

## 3. Сертификат Let's Encrypt

```sh
sudo certbot certonly --webroot -w /var/www/quic-demo \
  --cert-name "$DOMAIN" -d "$DOMAIN"
```

Certbot запросит контактный email и принятие условий. `fullchain.pem` и
`privkey.pem` появятся в `/etc/letsencrypt/live/$DOMAIN/`.
Метод webroot оставляет работающие сайты nginx доступными.
[Документация Certbot](https://eff-certbot.readthedocs.io/en/stable/using.html#webroot).

## 4. HTTPS и QUIC-служба

```sh
sed "s/quic.example.org/$DOMAIN/g" deploy/nginx-quic-demo.conf > /tmp/quic-lab-https.conf
sudo install -m 644 /tmp/quic-lab-https.conf /etc/nginx/sites-available/quic-lab
sed "s/quic.example.org/$DOMAIN/g" deploy/quic-lab-public.service > /tmp/quic-lab.service
sudo install -m 644 /tmp/quic-lab.service /etc/systemd/system/quic-lab.service
sudo systemctl daemon-reload
sudo systemctl enable --now quic-lab
sudo nginx -t
sudo systemctl reload nginx
```

nginx завершает TLS для WSS и проксирует `/echo` на loopback. QUIC завершается
на Go-сервере UDP/4433. Unit использует DynamicUser и LoadCredential: systemd
передаёт процессу доступные только ему копии TLS-файлов. Делать `privkey.pem`
доступным всем пользователям не требуется.
[systemd credentials](https://systemd.io/CREDENTIALS/).

## 5. Автоматическое продление

```sh
sed "s/quic.example.org/$DOMAIN/g" deploy/renew-quic-lab.sh > /tmp/renew-quic-lab.sh
sudo install -m 755 /tmp/renew-quic-lab.sh /etc/letsencrypt/renewal-hooks/deploy/quic-lab
sudo systemctl enable --now certbot.timer
sudo certbot renew --dry-run
systemctl list-timers certbot.timer
```

Hook срабатывает только для этого certificate lineage, проверяет nginx,
перезагружает его конфигурацию и перезапускает работающую QUIC-службу.
Перезапуск разрывает текущие учебные QUIC-сеансы. `--dry-run` проверяет продление;
по умолчанию deploy hooks в dry-run не выполняются. При другой установке Certbot
используйте её механизм планирования вместо второго таймера.

## 6. Проверка и приложение

```sh
systemctl status quic-lab --no-pager
sudo journalctl -u quic-lab -n 30 --no-pager
sudo ss -lntup | grep -E ':4433|:8081|:443'
curl "https://$DOMAIN/"
```

В Android задайте QUIC endpoint `$DOMAIN:4433` и TLS/HTTPS hostname `$DOMAIN`.
Поле Pin оставьте пустым. HTTPS использует порт 443 и `/echo`.
При блокировке UDP HTTPS может работать независимо от QUIC.

CLI-проверка: `go run ./cmd/client -h` показывает параметры запуска.
Локальный QUIC без публичного сертификата можно запустить с `-ephemeral-cert`;
сервер напечатает fingerprint, который требуется явно задать клиенту.
Такой сертификат не подходит для публичного HTTPS-сравнения.

## Обновление

Соберите бинарник из выбранного тега. Остановите `quic-lab`, сохраните предыдущий
бинарник, установите новый и запустите службу. Обновляйте Android из того же релиза.
Проверьте echo и миграцию. Учебный сервер не имеет аутентификации: используйте
его для контролируемого опыта, а не как production VPN или общедоступный сервис.
