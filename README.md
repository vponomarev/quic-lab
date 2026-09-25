# QUIC Lab

Учебный Android-клиент и Go-сервер для сравнения **QUIC stream** и
**WebSocket поверх HTTPS / HTTP/1.1 / TLS / TCP** при смене Wi-Fi и мобильной сети.
Android 11+ (API 30), ARM64 и x86_64. Интерфейс на русском языке.

## VPN gateway (разработка)

Ветка 0.4 добавляет VPN как отдельный режим; существующий echo-стенд сохраняется.
Один сервер поддерживает echo и mTLS exit node, клиент — QUIC / HTTPS и четыре режима маршрутизации.
QUIC VPN поддерживает TCP и произвольный IPv4 UDP через QUIC DATAGRAM; HTTPS — TCP и DNS. AmneziaWG поддерживает TCP/UDP. IPv6 не поддерживается.
[Настройка, сертификаты и ограничения](docs/vpn-gateway.md).

## Возможности

- Одновременный echo через оба транспорта: по 20 сообщений/с.
- QUIC connection migration, предварительная проверка резерва, обнаружение
  отсутствия ответов и выдержка стабильности перед возвратом на Wi-Fi.
- HTTPS восстанавливается и возвращается на Wi-Fi независимо от QUIC.
- RTT, максимальная пауза, максимум изменения RTT за последние 15 секунд,
  число настоящих серверных сеансов, график и отметки обнаруженных переходов.
- SSID/BSSID и параметры Wi-Fi, доступные сведения об обслуживающей соте.
- Одна кнопка запуска, ручной выбор канала, лента событий и диагностика.

QUIC не переподключается автоматически после окончательного закрытия сеанса:
новый опыт начинается по кнопке. Это позволяет отличать миграцию от reconnect.
Полное отсутствие покрытия и блокировка UDP могут прервать соединение.

## APK и первый запуск

Скачайте APK из [Releases](https://github.com/vponomarev/quic-lab/releases).
Это подписанная отладочная учебная сборка, устанавливаемая вручную вне Google Play.
Она не содержит предустановленного сервера, IP-адресов или certificate pin.

1. Разверните собственный сервер по [инструкции Linux + Let's Encrypt](docs/deploy-linux.md).
2. В «Настройки стенда и подробности» задайте `ваш-домен:4433` и TLS/HTTPS-домен.
   Для сертификата Let's Encrypt оставьте Pin пустым. После запуска настройки
   сохраняются только на устройстве.
3. Включите Wi-Fi и мобильные данные, нажмите «Начать опыт», дождитесь ответов
   и готовности резерва, затем выключайте/включайте Wi-Fi.
4. Для SSID/BSSID и соты разрешите точную геопозицию кнопкой в приложении и
   включите геолокацию Android. Без разрешения echo работает, данные сети скрыты.

[Памятка для опыта](docs/student-quickstart.md) · [Архитектура](docs/architecture.md)

## Сервер на готовом Linux

**Быстрая установка echo + VPN + админки:** [архив с установщиком](docs/install-server.md).
Debian 12/13, Ubuntu 22.04/24.04; DNS-имя на входе, случайные учётные данные
при первом запуске, бинарник в `/opt/quic-lab`. Без nginx приложение само
обслуживает публичный HTTPS и mTLS VPN на TCP/443; при установленном nginx
он обслуживает сайт/echo на 443, а VPN использует 8443. Режим и порты сохраняются.
Ниже — ручной вариант.

Нужны Linux с systemd, nginx, certbot, Git и Go 1.26+; модуль фиксирует toolchain.
Домен должен указывать на сервер. Откройте TCP/80 для ACME, TCP/443 для HTTPS,
UDP/4433 для QUIC. WebSocket backend слушает только `127.0.0.1:8081`.

```sh
git clone https://github.com/vponomarev/quic-lab.git
cd quic-lab
go build -trimpath -o bin/quic-lab-server ./cmd/server
```

Краткий порядок получения сертификата: сначала включите HTTP-only шаблон nginx
из `deploy/nginx-quic-demo-http.conf`, заменив `quic.example.org` своим доменом,
и создайте `/var/www/quic-demo`. Затем:

```sh
sudo certbot certonly --webroot -w /var/www/quic-demo \
  --cert-name your.domain -d your.domain
```

После выдачи сертификата установите HTTPS-шаблон и systemd unit из `deploy/`,
также заменив домен. Установите deploy hook обновления сертификата: nginx должен
перечитать файлы, а QUIC-процесс — перезапуститься для загрузки нового TLS-ключа.
Полные команды, проверки и обновление — в [deploy-linux.md](docs/deploy-linux.md).
Секретный ключ сертификата хранится на сервере; не добавляйте его в Git.

## Сборка Android

Установите Go 1.26+, JDK 17, Android SDK Platform 35 и NDK 27.2.12479018.
Gradle Wrapper и версии зависимостей находятся в репозитории.

Windows / PowerShell:

```powershell
$env:JAVA_HOME = 'C:/path/to/jdk-17'
$env:ANDROID_HOME = 'C:/path/to/Android/Sdk'
./scripts/build-android.ps1 -BuildApk
```

Linux / macOS с подходящим Android NDK:

```sh
export JAVA_HOME=/path/to/jdk-17
export ANDROID_HOME=/path/to/Android/Sdk
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/27.2.12479018"
export PATH="$JAVA_HOME/bin:$PATH"
mkdir -p bin android/app/libs
go build -trimpath -o bin/gomobile golang.org/x/mobile/cmd/gomobile
go build -trimpath -o bin/gobind golang.org/x/mobile/cmd/gobind
export PATH="$PWD/bin:$PATH"
gomobile bind -trimpath -target=android/arm64,android/amd64 -androidapi=30 \
  '-ldflags=-s -w -extldflags=-Wl,-z,max-page-size=16384,-z,common-page-size=16384' \
  -o android/app/libs/quiclab.aar ./mobile
sh android/gradlew -p android assembleDebug lintDebug
```

APK: `android/app/build/outputs/apk/debug/app-debug.apk`.
Для Android Studio сначала соберите AAR, затем откройте `android/` и выберите JDK 17.

## Проверки

```sh
go test ./...
go vet ./...
# На Linux с C toolchain:
go test -race ./...
(cd third_party/quic-go && go test -race .)
```

Android instrumentation APK: `android/gradlew -p android assembleDebugAndroidTest`.
Установите оба APK через `adb install -r`, затем:

```sh
adb shell am instrument -w -e host your.domain -e comparison true \
  -e class ru.vpnc.quiclab.HttpsReturnTest,ru.vpnc.quiclab.ComparisonTest \
  ru.vpnc.quiclab.test/androidx.test.runner.AndroidJUnitRunner
```

Эти тесты переключают Wi-Fi и требуют включённой мобильной сети. В finally
Wi-Fi включается обратно. `-e initialDwell 100 -e cycles 4 -e dwell 12` расширяют
ComparisonTest. Локальные тесты метрик: класс `ru.vpnc.quiclab.TransportStatsTest`.

## Структура

`android/` — UI и Android Network; `mobile/` — Go-клиенты; `cmd/server/` и
`internal/echo/` — сервер; `cmd/client/` — CLI; `deploy/` — шаблоны nginx/systemd.
`third_party/quic-go/` — экспериментальный локальный fork v0.63.0, используемый
через `replace`. [Описание патчей](third_party/README.md). Клиент и сервер следует
собирать из одной версии репозитория. Это не официальный выпуск quic-go.

Администрирование пользователей и импорт QR: [инструкция](docs/admin.md).

## AmneziaWG в Android

Дополнительный VPN-профиль: импорт `.conf` или QR, прежние режимы маршрутизации, TCP/UDP и ICMP RTT до endpoint через туннель. Echo и QUIC/HTTPS сохранены. [Настройка, совместимость и ограничения](docs/amneziawg.md).


## Мультипротокольный сервер

Админка управляет разрешениями QUIC / HTTPS / AmneziaWG для каждого пользователя,
включением и отключением доступа, выдачей общего профиля QUIC Lab и стандартного
AWG `.conf` / QR. Статистика обновляется через WebSocket каждые 5 секунд.
Опциональный AWG-процесс использует Linux TUN; ядро AWG устанавливать не нужно.
[Установка и сетевые настройки](docs/install-server.md#опциональный-сервер-amneziawg).

VPN поддерживает общий исходящий транзит через удалённый AWG gateway, NAT и блокировку прямого выхода при отказе uplink. Android показывает локальный / транзитный RTT; график использует локальный RTT. [Настройка транзита](docs/install-server.md#глобальный-транзит-vpn-через-удалённый-amneziawg).
