# Устройства, регистрация, конфигурация и SNI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Дать администратору отдельный отзыв устройств, общий QR регистрации и безопасное обновление профилей.

**Architecture:** Store хранит пользователя и устройства отдельно; авторизация протоколов привязана к device ID. Версионированный control endpoint остаётся доступным до data-plane handshake, а TLS-конфигурация отделяет отправляемый SNI от проверяемого имени.

**Tech Stack:** Go 1.26.8, существующий fork quic-go 0.63.0, coder/websocket, AmneziaWG, Kotlin, Android API 30+, JDK 17, Linux/systemd.
**Spec:** [Утверждённая спецификация](../specs/2026-09-30-vpn-product-design.md), утверждена владельцем 2026-09-30.
**Status:** D1–D3 выполнены и проверены; D4 — следующий этап.

## Global Constraints

- Все Go/Python-тесты, race/vet и отдельные тестовые бинарники выполняются на Linux-хосте root@192.168.5.214; Windows — существующая Android-сборка и USB/ADB, instrumentation — на телефоне. Подготовка и ограничения хоста описаны в плане 00.

- Android 11+ — единственный обязательный клиент; Linux/systemd — сервер. Не добавлять Docker, автозапуск или IPv6.
- Максимальная доступность: до 1 секунды от отказа при готовом исправном резерве; экономия: до 10 секунд при доступном LTE. Сессия без путей: 120 секунд по умолчанию.
- Карусель: 2–5 соединений всего на профиль. Резерв не прогревается без разрешённой проверки. Лимит LTE общий на один запуск всего VPN.
- VPN-трафик при отказе блокируется; явный Stop всего VPN снимает блокировки. Остановка одного выхода сохраняет блокировку его правил.
- QR: 24 часа / 5 новых устройств по умолчанию. Одновременная нагрузка: 15 устройств, предел 30. 100 Мбит/с — внешний канал, не обещанная полезная скорость.
- Минимум два независимых выхода. В первой фазе нет VLESS, подписок, доменных правил, системной постоянной блокировки после завершения службы.
- Секреты и реальные клиентские конфиги не включать в тестовые fixtures, логи или git. Новые зависимости не нужны без отдельного обоснования.
- Исходная рабочая копия содержит незакоммиченные разработки. Выполнять только после сохранения согласованного baseline в изолированную рабочую копию; порядок — в плане 00.
- Новые интерфейсы ниже — проектируемые, не уже существующие API. Go API с context/структурами остаются internal; gomobile получает отдельные простые методы с JSON.
- Каждый шаг red/green выполняется на существующем коде и свежем baseline. Если сценарий уже реализован, сначала подтвердить тестом и не переписывать его ради плана.
- Коммит задачи включает только перечисленные изменённые файлы и тесты. Не применять git add . или публикацию/деплой.
- Блоки assert ниже — компактные контракты тестов (псевдокод), не готовый исполняемый тестовый файл. Реализовать именованные Go/Android-тесты стандартными средствами проекта; fixture строится в файле теста задачи.

## Зависимости

Модель A1; D1/D2 перед финальной авторизацией B2/B4. C3 перед сетевым Android-обновлением D4. D3 — ранняя проверка технической осуществимости SNI, до настройки реального ingress.

## Review Focus

- Два телефона одновременно занимают последнее место QR (D2).
- Повтор запроса регистрации после потери HTTP-ответа создаёт лишнее устройство (D2).
- Общий ключ legacy-пользователя нельзя задним числом разделить на физические устройства (D1).
- Подмена update endpoint/сертификата и откат версии (D3/D4).
- AWG без логина обходит лимит активных устройств (D2).

## Карта файлов и задач

Пути ниже относительно корня репозитория quic-lab. Новые файлы выделены по ответственности; существующие крупные файлы не переписывать целиком. Общие команды Android и правила baseline — в [плане 00](2026-09-30-phase1-00-roadmap.md).

### Task D1: Постоянная модель устройств и отзыв

**Files:**
- Create: `internal/admin/devices.go`, `internal/admin/devices_test.go`.
- Modify: `internal/admin/store.go`, `internal/admin/protocols.go`, `internal/admin/stats.go`, `internal/admin/awg_stats.go`, `internal/admin/web.go`, `internal/admin/protocols_web.go` (все internal/admin); `internal/awgserver/worker.go`; `internal/gateway/bond.go`.

**Interfaces:** type Device struct { ID, UserID, Name, Certificate, Key string; Disabled bool; Created, Expires time.Time; AWG *awgserver.Peer }; func (s *Store) Devices(userID string) []Device; func (s *Store) DisableDevice(id string) error; func (s *Store) DeviceForTLS(cs tls.ConnectionState) (Device,error). Store.RegisterProtocol сохраняет подпись, учитывает deviceID, а агрегаты пользователя суммируют устройства.

- [x] **Step 1 — регрессионный тест:** добавить TestDeviceMigrationAndRevocation; TestLegacyIdentityPreserved; TestRevocationDuringJoin. Минимальные обязательные проверки:

```text
assert migratedLegacyUser.hasOneLegacyDevice
assert oldCertificateStillMapsToLegacyDevice
assert disable(deviceA).closesAllItsPathsAndAWG
assert deviceB.remainsAuthorized
assert joinRacingRevocation.cannotBecomeActive
assert failedStateSave.doesNotCorruptIdentityStore
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** Версионировать diskState, atomic save+backup, сохранять CA/ключи и существующие identities. Старую общую идентичность показать как legacy-устройство: несколько телефонов с тем же ключом не различимы до новой регистрации, не обещать обратного. Разнести AWG peer с пользователя на устройство и сохранить уникальные адреса. Отзыв сначала запрещает авторизацию в Store, затем закрывает все зарегистрированные callbacks/peer, не держа mutex во время close. Проверять каждый join, не только создание сессии.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/admin ./internal/gateway -count=1 -timeout=120s; Linux: go test ./internal/awgserver -count=1 -timeout=120s
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: provision and revoke identities per device"`. Не включать соседние незавершённые задачи.

### Task D2: Многоразовый QR и допуск 30 активных устройств

**Files:**
- Create: `internal/admin/enrollment.go`, `internal/admin/enrollment_test.go`, `internal/admin/device_admission.go`, `internal/admin/device_admission_test.go`.
- Modify: `internal/admin/web.go`, `internal/admin/ui.go`, `internal/admin/protocols.go`, `internal/admin/stats.go`; `internal/awgserver/worker.go`; `android/app/src/main/java/ru/vpnc/quiclab/ProfileImport.kt`, `android/app/src/main/java/ru/vpnc/quiclab/ProfileScanActivity.kt`.
- Test: `android/app/src/androidTest/java/ru/vpnc/quiclab/DeviceEnrollmentTest.kt`.

**Interfaces:** type Enrollment struct { ID, UserID string; Expires time.Time; MaxDevices, Used int }; func (s *Store) NewEnrollment(userID string, ttl time.Duration, maxDevices int) (Enrollment,string,error); func (s *Store) Enroll(token, requestID, deviceName string) (Device,error); func (s *Store) RevokeEnrollment(id string) error. type Admission struct; func NewAdmission(limit int) *Admission; Acquire(deviceID string) (release func(),err error); renew existing device lease without adding device count.

- [x] **Step 1 — регрессионный тест:** добавить TestConcurrentLastEnrollmentSlot; TestEnrollmentRetry; TestDeviceCap; TestAWGAdmission. Минимальные обязательные проверки:

```text
assert defaultTTL == 24*hour && defaultLimit == 5
assert concurrentLastSlot.successCount == 1
assert repeatSameRequestID.sameDeviceAndCredentials
assert revokedQR.doesNotRevokeExistingDevices
assert thirtyDevicesWithFivePathsEach.allowed
assert thirtyFirstDevice.rejected && existingDevicesStillWork
assert AWGAndQUICSameDevice.count == 1
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** QR несёт HTTPS registration URL и случайный 256-bit token во fragment; клиент отправляет token в POST body. Хранить hash token, срок и атомарный счётчик; requestID постоянен при retry конкретного импорта, ответ выдаётся повторно только с валидным enrollment token, без публичного lookup по requestID. В админке срок/лимит/отзыв. Допуск устройства общий для QUIC/HTTPS/AWG; AWG учитывать по аутентифицированному peer на сервере до разрешения data-plane, освобождать lease после явного отключения либо подтверждённого таймаута активности. Не считать registered peers активными устройствами. Существующий worker API проверить на возможность admission callback; если не позволяет, выделить техническое изменение worker до заявлений о cap=30, не исключать AWG молча.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/admin -count=1 -timeout=120s; Linux: go test ./internal/awgserver -count=1 -timeout=120s; Android: DeviceEnrollmentTest
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: enroll multiple devices and enforce shared admission limit"`. Не включать соседние незавершённые задачи.

### Task D3: SNI, независимая проверка identity и совместимость

**Files:**
- Create: `internal/servertls/client.go`, `internal/servertls/client_test.go`; `internal/protocol/capabilities.go`, `internal/protocol/capabilities_test.go`.
- Modify: `mobile/gateway.go`, `mobile/gateway_bond.go`, `mobile/gateway_bond_https.go`; `cmd/server/sni.go`, `cmd/server/sni_test.go`, `cmd/server/config.go`, `cmd/server/main.go`; `internal/admin/config.go`; `cmd/demux/main.go`.

**Interfaces:** type servertls.ClientOptions struct { ServerName, VerifyName string; Roots *x509.CertPool; Certificate *tls.Certificate }; func ClientConfig(o ClientOptions) (*tls.Config,error). type protocol.Capabilities struct { ControlVersion, DataVersion, MinAndroidVersionCode int; Features []string; APKURL, APKSHA256 string }; control endpoint GET /api/v1/capabilities; authentication device token из D4 при доступе к индивидуальным данным.

- [x] **Step 1 — регрессионный тест:** добавить TestDifferentSNIStillVerifiesServer; TestRejectWrongExpiredCertificate; TestQUICAndHTTPSCustomSNI; TestIncompatibleExit. Минимальные обязательные проверки:

```text
assert sentSNI == configuredCoverName
assert verifiedIdentity == realServerName
assert wrongCAOrNameOrExpiredCertificate.rejected
assert missingVerifyName.rejected
assert incompatibleInternetExit.doesNotStopHome
assert capabilitiesReadableBeforeDataHandshake
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** Отдельно протестировать Go tls.VerifyConnection с x509.Verify по VerifyName и реальной цепочке/сроку, включая resumption. Если для разделения SNI и VerifyName нужен InsecureSkipVerify, допускать его только внутри закрытого конструктора с обязательным VerifyConnection и отрицательными тестами; это не отключение проверки. Не выпускать сертификат чужого домена. Сервер принимает настроенные SNI для VPN без перехвата произвольных чужих имён; сохранить SNI fallback/nginx сценарии. Сертификаты и client auth QUIC/HTTPS прежние. Версии control/data раздельные, несовместимый data-plane возвращает определённый upgrade_required, control endpoint не ломается вместе с ним. Не обещать эффект обхода DPI по результату локального TLS-теста.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/servertls ./internal/protocol ./cmd/server ./internal/gateway ./mobile -count=1 -timeout=120s
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: separate VPN SNI from verified server identity"`. Не включать соседние незавершённые задачи.

### Task D4: Обновление профиля и APK по кнопке

**Files:**
- Create: `internal/admin/device_config.go`, `internal/admin/device_config_test.go`; `android/app/src/main/java/ru/vpnc/quiclab/ProfileUpdate.kt`; `android/app/src/androidTest/java/ru/vpnc/quiclab/ProfileUpdateTest.kt`.
- Modify: `internal/admin/web.go`, `internal/admin/download.go`; `android/app/src/main/java/ru/vpnc/quiclab/ProfileImport.kt`, `android/app/src/main/java/ru/vpnc/quiclab/VpnIdentity.kt`, `android/app/src/main/java/ru/vpnc/quiclab/VpnConfiguration.kt`, `android/app/src/main/java/ru/vpnc/quiclab/VpnExitController.kt`, `android/app/src/main/java/ru/vpnc/quiclab/VpnActivity.kt`.

**Interfaces:** func (w *Web) deviceConfig(rw http.ResponseWriter,r *http.Request) на GET /api/v1/devices/{id}/config; func (s *Store) AuthenticateUpdate(deviceID, token string) (Device,error). Kotlin ProfileUpdate.fetch(context: Context, exitId: String): JSONObject; diff(current: JSONObject, incoming: JSONObject): JSONObject; apply(context: Context, exitId: String, incoming: JSONObject). JSON envelope: schema_version, config_revision, device_id, exit_id, server_config, capabilities. Local preferences separate.

- [x] **Step 1 — регрессионный тест:** добавить TestUUIDAloneDenied; TestDisabledDeviceUpdateDenied; ProfileUpdateTest.atomicApplyAndLocalPreferences; ProfileUpdateTest.apkValidation. Минимальные обязательные проверки:

```text
assert UUIDWithoutBearer.status == 401
assert disabledDevice.status == 403
assert lowerRevision.doesNotOverwriteCurrent
assert failedFetchOrValidation.preservesProfile
assert apply.internetOnlyRestartsInternet
assert localRoutesAndEconomyPreserved
assert APKWrongHashOrPackageOrSigner.notInstalled
```

- [x] **Step 2 — RED:** выполнить целевые команды ниже. Убедиться, что отсутствующий контракт даёт ожидаемое падение проверки, а не ошибку окружения. Для уже работающего поведения сохранить зелёный регрессионный тест и выделить отсутствующую часть.

- [x] **Step 3 — реализация:** Предлагаемый механизм: отдельный случайный 256-bit per-device update token, hash на сервере, Android Keystore через VpnIdentity; UUID только адресует устройство. Token передаётся в Authorization, не URL/логах; регистрация выдаёт его только устройству. HTTPS update host проверяется обычной PKI независимо от cover SNI data-plane. Новый endpoint принимается только из аутентифицированного конфига. Лимит config body 1 MiB, атомарная запись после подтверждения diff; новый config не включает изменяемые локальные prefs. При incompatible показать APK через C3, проверить hash, package и подпись до Android installer; пользователь подтверждает установку. Никаких автоустановок или остановки прочих выходов.

- [x] **Step 4 — GREEN:** повторить команды ниже; ожидаются PASS / exit 0, Android instrumentation — OK без failures. Проверки, требующие Linux или устройства, не заменять Windows-сборкой.

```text
go test ./internal/admin -count=1 -timeout=120s; Android: ProfileUpdateTest, ServiceTransferTest
```

- [x] **Step 5 — локальная проверка и коммит:** проверить diff и отсутствие секретов; добавить только реально изменённые файлы задачи из Files, включая новые тесты, затем выполнить `git diff --cached --check` и `git commit -m "feat: update per-device configuration and offer required APK"`. Не включать соседние незавершённые задачи.

## Проверка плана

- Сопоставить результат задач с матрицей покрытия в плане 00.
- Review Focus распределён по указанным тестам; транспортные тесты не заменяют испытания на Android.
- Никакой шаг этого плана не является разрешением на публикацию или изменение действующего сервера.
