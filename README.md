# QUIC Lab

Android-клиент и Go-сервер для лабораторных работ по QUIC и самостоятельного
использования как IPv4 VPN. Android 11+ (API 30), ARM64 и x86_64.
Интерфейс на русском языке.

**Веха v0.7.0:** Echo и VPN сосуществуют в одном приложении и на одном сервере.
[Состав вехи и проверки](docs/milestone-v0.7.0.md) ·
[Планы развития](docs/vpn-plan.md).

## Возможности

- Echo: сравнение QUIC, HTTPS/WebSocket и AmneziaWG, публичный стенд либо
  аутентифицированный VPN-профиль. Публичный AWG включается на сервере отдельно.
- RTT до шлюза и через транзит до удалённого gateway, jitter, максимальная пауза,
  график, события смены сети и сведения WiFi/сотовой сети.
- VPN: QUIC, HTTPS/WebSocket и AmneziaWG; TCP и IPv4 UDP. Для HTTPS UDP
  переносится поверх TCP и подвержен head-of-line blocking.
- Сохранённые профили; all apps, only selected apps, exclude selected apps,
  маршрутизация по IPv4-подсетям; общий или индивидуальный список приложений.
- Multiple: несколько подключений через один Android TUN, приоритеты правил,
  независимая остановка профилей и блокировка их маршрутов без скрытого fallback.
- Веб-админка: управление пользователями и протоколами, QR/конфиги, отзыв доступа,
  живая статистика, состояние транзита и скачивание APK.
- mTLS для QUIC/HTTPS VPN, управляемый AWG-сервер, общий AWG-транзит,
  установщик Linux с поддержкой существующего nginx.

IPv6 не поддерживается. Multiple имеет один DNS-профиль; split DNS и Fake IP
отложены. Смена транспорта не обещает сохранения существующих TCP-соединений.
Границы проверки multiple описаны [отдельно](docs/multiple-vpn.md).

[VPN и сертификаты](docs/vpn-gateway.md) · [AmneziaWG](docs/amneziawg.md) ·
[Три транспорта Echo](docs/echo-transports.md)

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
