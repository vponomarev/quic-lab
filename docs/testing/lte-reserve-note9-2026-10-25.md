# Подготовка LTE-резерва и повторный HA-прогон Note9

Дата: 2026-10-25. Статус: исправление проверено, полевой прогон прошёл (201,172 с).

## Итог Note9

До переключений подтверждены основной AWG/Wi-Fi и резервный VLESS/LTE; тот же набор оставался готовым через 10 секунд. warm_lte_before_switch=true.

| Переход | Основной путь до → после | HTTPS-обменов | Максимальное время ответа |
|---|---|---:|---:|
| Wi-Fi → LTE | AWG/Wi-Fi → VLESS/LTE | 586 | 95 мс |
| LTE → Wi-Fi | VLESS/LTE → AWG/Wi-Fi | 682 | 296 мс |

В каждом направлении использовалось отдельное 90-секундное TLS/TCP-соединение: connections=1, ok=true, vpn=true. Время ответа отдельного обмена не является точным временем переключения. bond-1, exitId и generation=1 сохранялись, lostSessions=0 во всех снимках. Полевой прогон проверяет собственные AWG/VLESS, а не стороннюю подписку. Позднее появление LTE при уже заполненном Wi-Fi-пуле отдельно покрыто регрессионными native-тестами.

Хранилище приложения восстановлено побайтно, SHA-256: 1e583855d7a2f7026ed14e42ba00beca8d76e5c5e3ceb628f4d73cbcc779c91f. Независимая итоговая инвентаризация подтвердила четыре исходных профиля, исходный выбранный профиль, multiple=true, cell_on=false и cell_off=false. VPN остановлен, ACTIVATE_VPN=ignore; Wi-Fi включён, mobile_data0=1 и mobile_data1=1. Общий устаревший mobile_data=0 на этом телефоне не отражает состояние двух SIM. Временный приватный профиль удалён, ошибок очистки нет. Новый исправленный APK оставлен установленным. Сервер не изменялся.

Доказательства: artifacts/lte-reserve/field/live-report.json, live-device.log, restoration.json; artifacts/lte-reserve/final-inventory.log. Обычный запуск instrumentation подтвердил пропуск полевого теста без field_ha=true и успешную инвентаризацию.

## Исправление

При позднем появлении разрешённой мобильной сети заполненный HA-пул теперь может перенести резервный слот с Wi-Fi на LTE, сохраняя проверенный основной путь и предел двух путей. Учитываются разрешения сети/резерва, экономия, общий бюджет трафика, разрешённый адрес, backoff, ёмкость профиля и исключительное владение AWG-peer. Освобождается только резервный слот; при неудаче подключения основной продолжает работать.

Механизм возврата односетевого соединения на Wi-Fi больше не убирает намеренно подготовленный LTE-резерв, когда HA уже имеет готовый Wi-Fi-путь.

## Проверки

Регрессионные тесты сначала воспроизвели отсутствие LTE, затем повторные переподключения LTE из-за механизма возврата. Целевые relay-тесты после исправления прошли. Добавлены проверки запрета LTE, экономии, отсутствующего адреса, backoff, исчерпанного бюджета и исключительного AWG-peer. Независимый review не выявил обязательных исправлений.

Полный go test ./... не проходит в этом рабочем дереве на Windows. Полный список имён провалившихся тестов и пакетов сохранён в artifacts/lte-reserve/standard-failures.txt, подробности — standard-suite.log. Причины включают смешанные Go-пакеты в игнорируемых artifacts, отсутствующий awgserver.Worker.listenReload на Windows, Linux-only backup/restore, ошибки directory sync в backup/MDM, ошибки конфигурации vlessserver и TestStandaloneImportRejectsUnsupported в windowsengine. Эти ошибки не исправлялись в рамках изменения LTE-резерва.

## Полевой сценарий

Собственные AWG/VLESS к общему HA-серверу. Требуется подготовленный LTE-путь при работающем Wi-Fi и его сохранение минимум 10 секунд до переключения. Затем два отдельных 90-секундных HTTPS-соединения: Wi-Fi → LTE и LTE → Wi-Fi; каждый переход после первого успешного обмена. Оригинальное зашифрованное хранилище восстанавливается побайтно, временные ключи удаляются; Wi-Fi и мобильные данные остаются включёнными по разрешению пользователя.

Сборка AAR, APK и lintDebug прошли. SHA-256 установленного APK: 4C442962884DC0A179CBA136CC7D569BE81F1EF52CC2C5A47D801CB6EF84C4C2.

Полный набор фактических mobile GoFiles/TestGoFiles без Linux-зависимого public_awg_test.go прошёл за 83,971 с. Регрессия TestRelayLateCell* повторена пять раз, все прогоны прошли (13,091 с). Это не заменяет провалившийся стандартный go test ./....

## Ошибки общей проверки вне исправления

Ниже имена тестов и пакетов из standard-suite.log; они перечислены, чтобы успешные целевые проверки не скрывали состояние общего набора:

```text
FAIL	quiclab/artifacts [setup failed]
FAIL	quiclab/artifacts/research-live [setup failed]
FAIL	quiclab/artifacts/backup-probe [build failed]
FAIL	quiclab/artifacts/editor-live [build failed]
FAIL	quiclab/cmd/server [build failed]
FAIL	quiclab/internal/admin [build failed]
FAIL	quiclab/internal/awgserver [build failed]
--- FAIL: TestSnapshotPinnedGeneration (0.00s)
--- FAIL: TestSnapshotConfigScope (0.02s)
--- FAIL: TestSnapshotDeadline (0.00s)
--- FAIL: TestSnapshotRepresentativeDataset (7.84s)
--- FAIL: TestSnapshotSystemdStateDirectoryAlias (0.02s)
--- FAIL: TestPublicationSnapshot (0.00s)
--- FAIL: TestPublicationDeadline (0.00s)
--- FAIL: TestPublicationEpoch (0.00s)
--- FAIL: TestJobsLimits (0.00s)
--- FAIL: TestJobsDownloadLease (0.00s)
--- FAIL: TestJobsQuota (0.00s)
--- FAIL: TestJobsCrashCleanup (0.00s)
--- FAIL: TestRestoreIdentity (0.01s)
--- FAIL: TestRestoreRejectLiveOwner (0.00s)
--- FAIL: TestRestoreCrashRollback (0.03s)
--- FAIL: TestRollbackInterruptedSwapBoundaries (1.64s)
--- FAIL: TestInstallationGateSurvivesMissingData (0.00s)
--- FAIL: TestRuntimeRejectsUnfinishedRestore (0.02s)
--- FAIL: TestInstallationGateCrossProcess (0.00s)
FAIL	quiclab/internal/backup	21.264s
FAIL	quiclab/internal/echoawg [build failed]
FAIL	quiclab/internal/gateway [build failed]
--- FAIL: TestSnapshotMDMConfigScope (0.37s)
--- FAIL: TestConfigInvalidDoesNotChangeRevision (0.36s)
--- FAIL: TestOwnershipEnrollmentAndMigration (0.45s)
--- FAIL: TestUserDeletionKeepsMDM (0.56s)
--- FAIL: TestProbeCommands (0.65s)
--- FAIL: TestProbeSnapshots (0.54s)
--- FAIL: TestProbeResumeEndsPreviousRemoteRun (0.37s)
--- FAIL: TestProfileSelectionWaitsForClientUpgrade (0.15s)
--- FAIL: TestRelayCapabilityAndDowngrade (0.03s)
--- FAIL: TestRelayStatusBoundsAndRights (0.01s)
--- FAIL: TestRelayPendingIncompatibleIdentityRejected (0.01s)
--- FAIL: TestReportsRightsAndOrdering (0.01s)
--- FAIL: TestDesiredCASRetry (0.02s)
--- FAIL: TestReportInventoryLimits (0.05s)
--- FAIL: TestPendingIdentitySurvivesEditorRead (0.01s)
--- FAIL: TestVLESSGroupReportBoundsAndPermission (0.01s)
--- FAIL: TestVLESSGroupRequiresCapability (0.00s)
--- FAIL: TestResearchRightsDefaultAndRevocation (0.01s)
--- FAIL: TestResearchScopeRightsAndCapabilities (0.00s)
--- FAIL: TestDirectAdmissionUsesReportedDeviceRights (0.01s)
--- FAIL: TestGrantedRightsAreReportedAndRevoked (0.01s)
--- FAIL: TestEnrollmentRetry (0.01s)
--- FAIL: TestResumeDropsCommands (0.02s)
--- FAIL: TestRevisionCASAndRestart (0.02s)
--- FAIL: TestMDMHTTPIsolation (0.03s)
--- FAIL: TestLongPollWakeAndCancel (0.01s)
--- FAIL: TestWriteFailureDoesNotPublishRevision (0.01s)
--- FAIL: TestTelemetryDurableIdempotentAndConsent (0.02s)
--- FAIL: TestTelemetryRejectsStaleEpochPausedAndMalformed (0.02s)
--- FAIL: TestTelemetryRetentionAndDelete (0.01s)
--- FAIL: TestTelemetryHTTPRequiresRealTLSAndSecret (0.01s)
--- FAIL: TestTelemetryPolicyVisibleAndValidated (0.01s)
--- FAIL: TestTelemetryMaintenancePhysicallyRemovesExpiredInactiveHistory (0.01s)
--- FAIL: TestTelemetryDroppedWithoutPendingRecords (0.01s)
FAIL	quiclab/internal/mdm	9.568s
--- FAIL: TestConfigRejectsInvalidSecurityAndDestination (0.00s)
--- FAIL: TestClientURIRoundTrips (0.00s)
--- FAIL: TestControlAcknowledgesAppliedRevision (0.25s)
--- FAIL: TestControlRejectsForeignSocketFile (0.09s)
--- FAIL: TestAdmissionBrokerProtocol (0.12s)
--- FAIL: TestControlRejectsOversizedAndUnknownMessages (0.48s)
--- FAIL: TestControlStatusRequiresAppliedRevision (0.15s)
--- FAIL: TestPrivateStateDirectoryAllowsSystemdAlias (0.23s)
--- FAIL: TestProxyProtocolOnlyOnLoopback (0.00s)
--- FAIL: TestReadConfigValidatesWithoutOpeningTLSMaterial (0.07s)
--- FAIL: TestAdmissionSourceMetadata (0.00s)
--- FAIL: TestWorkerRevisionContract (0.01s)
--- FAIL: TestWorkerRevokeSurvivesApplyFailureAndRestart (0.00s)
--- FAIL: TestWorkerWriteFailureClosesRuntime (0.00s)
--- FAIL: TestWorkerInvalidConfigPreservesCurrent (0.00s)
--- FAIL: TestWorkerConfiguredDeviceLimit (0.00s)
--- FAIL: TestWorkerInactiveRecordsDoNotConsumeAdmissionLimit (0.00s)
--- FAIL: TestWorkerDemuxPinSurvivesRestart (0.00s)
FAIL	quiclab/internal/vlessserver	5.846s
--- FAIL: TestStandaloneImportRejectsUnsupported (0.00s)
FAIL	quiclab/internal/windowsengine	7.882s
FAIL	quiclab/mobile [build failed]
```
