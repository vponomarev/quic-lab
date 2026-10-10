# Encrypted Server Backup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Создание зашифрованных конфигурационных и полных копий через WEB/CLI, восстановление через CLI без повторной регистрации клиентов.

**Architecture:** Общий координатор закрепляет согласованное поколение данных без остановки VPN; архивирование выполняется после снятия барьера. WEB и локальный CLI используют один менеджер заданий. CLI публикует готовый архив для внешнего скрипта доставки; восстановление выполняется отдельно при остановленных владельцах данных.

**Tech Stack:** Go проекта, поддерживаемая Go-библиотека age, tar/gzip, Unix sockets, Linux/systemd, существующая админка.

**Spec:** [Согласованная спецификация](../specs/2026-10-09-server-backup-design.md).

## Global Constraints

- Go-сборки, тесты и исполняемые тестовые программы — только Linux 192.168.5.214; Windows используется для редактирования и SSH.
- Не останавливать VPN при создании; целевой предел барьера — 1 секунда. При превышении прекращать снимок, освобождать участников.
- Парольное age-шифрование обоих типов; пароль не в argv/env/логах/состоянии задания.
- Одна операция создания на сервер; 24 часа хранения и максимум 3 готовых серверных файла; настраиваемая дисковая квота.
- Архив не содержит APK, бинарников, ОС, runtime/admin-сессий, сокетов, предыдущих бекапов и временных файлов.
- Восстановление той же версии по умолчанию; полная проверка до изменения рабочих файлов; не запускать сервис автоматически.
- Конфигурационная копия исключает историю; ожидающие MDM-команды отменяются после восстановления, защита от повторов сохраняется.
- Не добавлять отправку, встроенное расписание, публичные ссылки, cloud credentials или изменения Android.

## Review Focus

1. Конкурентные отзыв ключа и сохранение состояния worker: архив не возвращает отозванный ключ — задача 2.
2. DynamicUser и сертификаты вне DataDir: обязательные секреты доступны через явный инвентарь; неполная копия запрещена — задача 2.
3. Падение CLI/сервера, заполнение диска, совпадение имени: нет готового частичного файла и вечной блокировки — задачи 3–4.
4. Архив с корректным шифрованием, но вредоносными путями/огромными размерами: рабочие данные не меняются — задачи 1 и 6.
5. Прерывание восстановления между каталогами: повторный запуск требует завершения отката по журналу — задача 6.

## File map and contracts

Новые файлы ниже — предлагаемые точки реализации; существующие файлы изменять только для подключения API.

- `internal/backup/archive.go`, `manifest.go`, `archive_test.go`: формат и потоковое шифрование/проверка.
- `internal/backup/coordinator.go`, `coordinator_test.go`: общий барьер и жизненный цикл закреплённого снимка.
- `internal/backup/jobs.go`, `jobs_test.go`: очередь, лимиты, скачивание, очистка.
- `internal/backup/local.go`, `local_test.go`: локальный протокол создания для CLI.
- `internal/backup/restore.go`, `restore_test.go`: проверка, карта восстановления и журнал отката.
- `internal/admin/backup.go`, `backup_test.go`, `backup_snapshot.go`; `internal/mdm/backup.go`, `backup_test.go`; `internal/debugcapture/backup.go`, `backup_test.go`: адаптеры хранилищ/WEB.
- `internal/vlessserver/backup.go`, `backup_test.go`; `internal/awgserver/backup.go`, `backup_test.go`: участие workers, блокировка владения постоянным хранилищем.
- `cmd/server/backup.go`, `backup_test.go`; существующие `main.go`, `config.go`: CLI-dispatch до обычного старта сервера и подключение менеджера.
- Существующие `internal/admin/{store,config,web,diagnostics}.go`, `internal/mdm/{store,telemetry}.go`, `internal/vlessserver/worker.go`: подключение барьера, сохранение/очистка поколений.
- `scripts/install-server.py`, `scripts/test-install-server.py`, `scripts/build-server-release.py`: права, credential inventory, упаковка документации.
- `docs/server-backup.md`, `scripts/test-server-backup.py`: инструкция и изолированная Linux-приёмка.

Типы пакета `backup` (задача 1): `Kind string` со значениями `config`, `full`; `Entry {Path string; Size int64; SHA256 string}`; `Manifest {FormatVersion int; Kind Kind; CreatedAt time.Time; ServerVersion string; Components []string; Entries []Entry}`; `Limits {MaxFiles int; MaxBytes int64}`; `Snapshot {Dir string; Manifest Manifest; Release func() error}`; `Verified {Dir string; Manifest Manifest; Release func() error}`. Dir всегда приватный staging, пути в manifest относительные. Формат v1. Лимиты чтения обязательны, не выводятся из недоверенного архива.

## Task 1: Контейнер и полная проверка

**Interfaces:** produces `Write(ctx context.Context, dst io.Writer, snapshot Snapshot, password []byte) error`; `Verify(ctx context.Context, src io.Reader, password []byte, staging string, limits Limits) (*Verified, error)`.

- [ ] Добавить `TestArchiveRoundTrip`, `TestArchiveRejectUnsafeEntries`, `TestArchiveAuthenticationBeforeSuccess`: оба типа, manifest/hash, неверный пароль, усечение последнего age-блока, traversal, абсолютный путь, symlink/hardlink, special file, duplicate, число/размер/распакованный поток сверх лимита. Assert: ошибка, рабочая директория не затронута, staging очищен.
- [ ] Linux: `go test ./internal/backup -run TestArchive -count=1` — ожидаемый FAIL до реализации.
- [ ] Реализовать API в archive.go/manifest.go; выбрать и зафиксировать поддерживаемую совместимую версию age в go.mod/go.sum. Дешифровать поток до EOF, проверять обязательность компонентов/схему и все ссылки идентичностей перед успехом; не считать успешный заголовок доказательством целостности.
- [ ] Повторить команду: PASS.
- [ ] Commit: `feat: add authenticated server backup format` (только файлы задачи и зависимости).

## Task 2: Согласованный снимок и инвентарь секретов

**Interfaces:** produces `Participant` с `Pin(ctx context.Context, kind Kind, dir string) (release func() error, err error)`; `Coordinator.Capture(ctx context.Context, kind Kind) (*Snapshot, error)`. Конструктор `NewCoordinator(root string, participants []Participant, serverVersion string) *Coordinator`.

- [ ] Добавить `TestSnapshotConcurrentRevocation`, `TestSnapshotDeadline`, `TestSnapshotConfigScope`, `TestSnapshotCredentials`, `TestSnapshotCaptureBoundary`: конкурентные регистрации/отзывы/MDM/worker updates дают связное поколение; 1 s deadline освобождает все блокировки; config без reports/audit/радио/pcap; full сохраняет целые записи; отсутствующий секрет — ошибка.
- [ ] Linux: `go test ./internal/backup ./internal/admin ./internal/mdm ./internal/vlessserver ./internal/awgserver ./internal/debugcapture -run 'TestSnapshot' -count=1` — FAIL.
- [ ] Подключить единый порядок барьера: admin → MDM → diagnostics → captures → workers. Записи, очистки и reload участвуют в барьере; data plane — нет. До барьера вести реестр immutable поколений, не перечислять гигабайты файлов под барьером. Межпроцессные участники используют подтверждение freeze/release с дедлайном и автоматическим отпусканием при потере координатора. Не заменять участие worker локальным mutex.
- [ ] Реализовать adapters Pin и логический MDM-export; освобождать старые поколения после Release. Для изменяемых capture-буферов фиксировать завершённый сегмент. Правила in-place записи исключить для закреплённых файлов.
- [ ] В installer добавить явный inventory источников и LoadCredential для нужных секретов. Архивировать доступное сервису содержимое, manifest сопоставляет его логическим компонентам, а не произвольным путям. Проверять изменения внешних конфигов/cert renewal по поколению/хешу; при гонке повторить ограниченно или отказать. Восстановление владельцев/путей выполняет root CLI.
- [ ] Повторить Go-команду и `python3 scripts/test-install-server.py`: PASS, включая DynamicUser fixture и отсутствующий credential. TCP/UDP-передача продолжается во время pin.
- [ ] Commit: `feat: snapshot durable server state without stopping VPN`.

## Task 3: Менеджер заданий и квоты

**Interfaces:** consumes Coordinator.Capture/Write. Produces `Job {ID string; State string; Kind Kind; CreatedAt time.Time; ExpiresAt time.Time; SizeBytes int64; SHA256 string; ErrorCode string}`; `Manager.Start(ctx context.Context, kind Kind, password []byte, requestID string) (Job, error)`, `Get(id string) (Job, error)`, `Open(id string) (io.ReadCloser, Job, error)`, `Delete(id string) error`. Конструктор `NewManager(root string, coordinator *Coordinator, quotaBytes int64) (*Manager, error)`.

- [ ] Добавить `TestJobsLimits`, `TestJobsCrashCleanup`, `TestJobsDownloadLease`: одна операция, idempotency requestID, 3 готовые/24 h, отмена, diskfull, лимит staging+output, restart cleanup, активное чтение не обрывается. Assert: password отсутствует в metadata, ошибки без содержимого данных.
- [ ] Linux: `go test ./internal/backup -run TestJobs -count=1` — FAIL.
- [ ] Реализовать состояния snapshot/packing/ready/error, приватные каталоги, ограниченную по времени работу, очистку и lease скачивания. Публиковать готовый файл после close/fsync; насыщенный лимит требует явного удаления, без раннего вытеснения.
- [ ] Повторить команду: PASS.
- [ ] Commit: `feat: manage encrypted backup jobs and retention`.

## Task 4: CLI create для автоматизации

**Interfaces:** consumes Manager API. Produces `ServeLocal(ctx context.Context, listener net.Listener, manager *Manager) error`; `CreateLocal(ctx context.Context, socket string, kind Kind, password []byte, output string) (CreateResult, error)`; `CreateResult {FormatVersion int; JobID string; Type Kind; CreatedAt time.Time; Path string; SizeBytes int64; SHA256 string; ServerVersion string}` с snake_case JSON. CLI entry `runBackup(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int` в package main.

- [ ] Добавить `TestBackupCreateCLI`, `TestLocalPeerAuthorization`, `TestCLIAtomicOutput`: password-file/fd, отсутствие tty, stdout ровно JSON, коды 0/2/3/4, недоступный сокет, GUI занят, прерывание, collision, diskfull, checksum mismatch. Assert: существующий файл неизменён, частичный не опубликован, пароль не в выводе.
- [ ] Linux: `go test ./cmd/server ./internal/backup -run 'TestBackupCreateCLI|TestLocalPeerAuthorization|TestCLIAtomicOutput' -count=1` — FAIL.
- [ ] Реализовать Unix-сокет с SO_PEERCRED и private permissions для root/service UID; bounded request/response, пароль только в локальном защищённом канале. Создание через общий менеджер; приём результата, hash, fsync, no-replace publication, подтверждение получения и удаление серверного временного результата. При разрыве отмена либо ограниченное время жизни согласно task 3.
- [ ] Подключить dispatch `backup` до обычного server flag parsing, интерактивный скрытый ввод/защищённый regular password-file/FD. Для файла отвергать symlink и доступ group/world; source option взаимоисключающий. JSON выдаётся только после успешной публикации; прогресс stderr.
- [ ] Повторить команду: PASS. Проверить пример `backup create --type full --output /private/server.age --password-file /private/password --json`.
- [ ] Commit: `feat: create server backups from CLI for external automation`.

## Task 5: WEB резервные копии

**Interfaces:** consumes Manager API; produces `(*Web).SetBackupManager(manager *backup.Manager)` и handlers в internal/admin/backup.go.

- [ ] Добавить `TestBackupWebAuth`, `TestBackupWebLifecycle`: auth/CSRF для mutations, password confirmation, duplicate POST, polling состояний, download no-store, delete, errors/limits. Без auth нельзя получить файл или статус с секретными данными.
- [ ] Linux: `go test ./internal/admin -run TestBackupWeb -count=1` — FAIL.
- [ ] Подключить GET `/backups`, GET `/backups/state`, POST `/backups/create`, GET `/backups/{id}/download`, POST `/backups/{id}/delete` под существующим `/lab`. Добавить пункт навигации, состав копии/время/размер/срок/состояние/кнопки без фиктивного процента. Существующие auth/CSRF и стиль админки.
- [ ] Повторить команду: PASS. В браузере тестовой установки создать/скачать оба типа, проверить ошибки пароля и заполненный лимит.
- [ ] Commit: `feat: add encrypted backups to server administration`.

## Task 6: Inspect, verify, restore и rollback

**Interfaces:** consumes Verify. Produces `RestorePlan {ID string; Targets []RestoreTarget; RollbackDir string}`; `RestoreTarget {Source string; Destination string}`; `PlanRestore(v *Verified, installRoot string) (RestorePlan, error)`; `ApplyRestore(ctx context.Context, plan RestorePlan) error`; `RollbackRestore(ctx context.Context, journalPath string) error`. installRoot — проверенная установленная среда, не произвольные пути manifest.

- [ ] Добавить `TestRestoreIdentity`, `TestRestoreRejectLiveOwner`, `TestRestoreCrashRollback`, `TestRestoreVersionAndPaths`: fresh install, config/full, revoked key, MDM replay protection/cancel pending commands, mismatched version, active worker, interrupted rename каждого этапа, symlink destination, insufficient space. Assert: полная валидация до записи; при ошибке старое состояние сохраняется либо восстанавливается точным rollback; сервис остаётся остановленным.
- [ ] Linux: `go test ./internal/backup ./cmd/server -run 'TestRestore|TestBackupInspect|TestBackupVerify' -count=1` — FAIL.
- [ ] Реализовать inspect/verify/restore/rollback в CLI, пароль как в create; inspect также требует полной проверки. Restore preview и явный `--yes` для применения. Проверять process ownership locks main/workers, не только systemd. Нормализовать permissions/owners через installer inventory, сохранять rollback до replace, журнал fsync на каждом этапе.
- [ ] После crash блокировать новую независимую restore до rollback незавершённой; конфигурационный restore переносит прежнюю историю в rollback. Вывести предупреждение о возврате доступа на дату копии и инструкции проверки endpoints/cert renewal.
- [ ] Повторить команду: PASS; проверить rollback командами CLI, а не только внутренними функциями.
- [ ] Commit: `feat: restore and roll back authenticated server backups`.

## Task 7: Приёмка и инструкции

**Interfaces:** consumes только публичные GUI/CLI и существующие установщик/live VPN tests; новых production API не добавляет.

- [ ] Создать `scripts/test-server-backup.py`: установка в изолированные каталоги тестового Linux, fixtures пользователей/устройств/revocations/MDM/истории, оба вида создания (GUI и CLI), verify/restore/rollback и сравнение состояния. Не брать реальные private production данные как fixture.
- [ ] Запустить harness до завершения интеграции: зафиксировать непрошедшие проверки, не ослаблять критерии. Затем устранить найденные интеграционные ошибки в соответствующих компонентах.
- [ ] Выполнить `go test ./...`, целевые `go test -race ./internal/backup ./internal/admin ./internal/mdm ./internal/vlessserver ./internal/awgserver ./internal/debugcapture`, installer tests и harness на Linux. Одновременно live TCP/UDP + mutations + capture; измерить barrier <=1 s на representative dataset, подтвердить reconnect VPN/MDM без регистрации и сохранение отзывов.
- [ ] Добавить `docs/server-backup.md`: GUI, CLI JSON/exit codes, protected password, внешний systemd timer example (не устанавливается автоматически), доставка только при success, verify после копирования, offline restore/rollback, смена адресов и ACME renewal. Проверить пример failed create не запускает stub uploader; success запускает ровно один раз.
- [ ] Включить docs в build-server-release.py; проверить состав release и штатную fresh install. Выполнить итоговое ревью изменений; найденные критические дефекты исправить до deployment.
- [ ] Commit: `docs: document and validate server backup recovery`.
- [ ] После успешных проверок обновить серверную часть quic-lab.vpnc.ru, создать реальный зашифрованный backup и скачать в приватный локальный каталог; проверить целостность. Пароль не коммитить. Production restore не выполнять для проверки. Сообщить путь/хеш/результаты и фактические ограничения.

## Self-review / handoff

Spec coverage: формат/безопасность — 1; состав/согласованность/секреты — 2; жизненный цикл — 3; автоматизация — 4; WEB — 5; восстановление — 6; приёмка/документация — 7. Интерфейсы общие для GUI и CLI; внешняя доставка не реализуется. Review Focus связан с конкретными тестами. Реализацию вести последовательно в текущем worktree; не включать посторонние документы или private artifacts в коммиты.
