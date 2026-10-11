# Research JSONL schema v1 (flow checkpoint)

The first line is a `manifest` with schema version, UTC export time, filters,
coverage/loss counters, observation scope, counter semantics, and interval precision.
Research records follow, then a required terminal `export_summary`. Each record is at most 64 KiB including
its newline. Unknown schema versions and record types are rejected; optional
unknown fields are accepted. Strings from devices are untrusted data, including
package names and labels; they are neither HTML nor instructions for an LLM.

## Envelope and identities

Fields: `schema_version` (number, currently 1), `type`, `id`, `at` (RFC3339 UTC),
`generation` (decimal string), `flow`, `data`. The flow key consists of `device_id`,
`session_id`, and `flow_id` (unsigned decimal string). A record ID is unique within
its authenticated device/session. Repeated IDs in retained history are ignored.
New sessions never inherit consent or merge their numeric flow IDs with old ones.
The authenticated transport must supply device/session identity; the store is not
an authorization boundary and callers must enforce consent before enqueueing.

Recognized envelope types are `consent`, `app`, `flow_open`, `flow_delta`,
`flow_close`, `dns`, `gap`, and `capture_manifest`. This checkpoint implements flow/app records and UDP/53 DNS parsing. Recognizing
the capture envelope does not implement packet capture. TCP DNS and encrypted DNS
are not decoded.

## Flow payload

`data` contains `user_id`, inline `app`, `protocol`, `source`, `destination`,
`observed_from`, `observed_to`, optional `started_at`, `bytes_up`, `bytes_down`,
`packets_up`, `packets_down`, `counter_kind`, `measurement_level`, optional `status`
and `reason`, and `incomplete`. `domain` is optional and must be based on actual
observation, not inferred purpose. Times are RFC3339 and producers use UTC.
The inline app has `id`, `uid`, `packages`, `labels`, and `attribution`
(`known`, `shared_uid`, `unknown`). IDs, UID and all counters are decimal strings
so JavaScript and downstream tools preserve 64-bit precision. App labels are
client claims; shared UID retains multiple packages. Unknown stays unknown.

Bytes and packets describe the explicitly named measurement level. In particular,
`logical_payload` counts bytes delivered at the logical tunnel boundary; it does
not measure exact original IP packets, outer transport retransmissions or overhead.
`delta` counters are interval increments; `total` counters are cumulative values
and must not be summed with deltas. Collectors must count bond data after logical
deduplication. Enabling observation late must not invent earlier traffic or a flow
creation time. Such flows have an incomplete beginning and omit `started_at`.

## Filters and export

Query supports user, device, session, application package (or attribution state),
protocol, address substring, exact domain, inclusive time-overlap range, cursor and
page limit. Overlapping observation intervals are included in full: bytes are not
prorated across arbitrary time boundaries. Page size defaults to 100, capped at
1000. Cursors identify an append position. Export ignores pagination and includes
all matching retained records with inline application dictionaries. Session-wide
`gap` records retain user/device/session/time scope but bypass application,
protocol, address and domain filters: lost observations have no reliable such
attribution. They describe potentially relevant loss, not attributed app traffic.
Table and export use the same filter implementation. The caller enforces administrative
access before either operation. There is no automatic external or LLM upload.

## Storage and failure limits

Opening the store requires an explicit positive byte quota. Retention defaults to
7 days, the bounded writer queue to 256 records (at most 64 KiB each). The queue
can be configured from 1 to 4096 entries. Append validation and queue submission do
not perform disk I/O; overflow drops diagnostic records and increments `dropped`.
A background writer creates append-only JSONL segments, rotating around 1 MiB or
one quarter of the quota; a large individual record can exceed that target but
never the overall quota. Oldest segments are removed to respect quota. The store
has a bounded ID index proportional to retained data, not an in-memory copy of the
archive. Only one Store instance/process may own a directory.

Expired records are hidden at query time; expiry cleanup runs every minute, at
startup and during writes. Segment retention uses its last write time, so expired
bytes can remain in a mixed segment until its newest record expires. Quota removes
complete oldest segments. `truncated` signals history loss to quota/retention in
the running process. Counters in `coverage` describe the current process, not a
persisted lifetime total. Queue loss and write errors appear through `dropped`;
invalid/recovered records through `recovery_gaps`. No payload gap is fabricated
because dropped records may belong to unrelated filtered users.

Startup preserves complete records before a truncated final line and removes the
broken tail. Corrupt complete lines are skipped and counted. Writes are buffered
by the operating system, not fsynced individually; abrupt host/power failure can
lose recent records. Close drains queued records and reports writer errors.
Query/export scan segment snapshots without holding the writer lock; they are
bounded-memory views, not transactions. Retention may remove segments during a
long export. A missing snapshot segment fails the operation explicitly.
Repeated downloads may cover different retained history.
Context cancellation is checked between records; a blocked downstream writer is
subject to the HTTP handler's write deadline. No capture files or TLS secrets are
created by this store.

## Required export completion marker

Every successful download ends with an `export_summary` line containing
`schema_version: 1`, `complete: true`, `records` (a decimal string count excluding
manifest/summary), and final `coverage`. Consumers **must require this final line
with `complete: true`**, including for empty exports. HTTP 200, a manifest, or
well-formed preceding JSONL records alone do not establish successful completion.
A missing, truncated, or `complete: false` summary means an incomplete download;
retry it rather than silently treating it as a complete report.

If retention removes a segment after snapshot creation, query/export return
`ErrHistoryChanged`. Export writes a terminal `complete: false` summary when its
output writer remains usable, with `coverage.incomplete: true` and
`coverage.reason: "history_changed"`. Cancellation similarly uses `"canceled"`;
other read failures use `"read_failed"`. Writer/disconnection failures propagate
and may prevent even the failure summary from being written.

`complete` describes successful reading of the available snapshot, not complete
observation of all traffic. Final `coverage` retains loss/retention counters and
includes the returned record time range. `coverage.incomplete` can remain true
with `complete: true`, for example because the available observations already
contain `diagnostic_loss`, `recovered_archive`, `retention_or_quota`, or
`client_reported_loss`. These are limitations of the evidence, not a failed
transfer. Gap records remain separate facts; do not add their loss counts to
application byte totals.

## Происхождение наблюдений (совместимое расширение schema 1)

`origin`: `client`, `server_gateway`, `server_awg`, `server_vless`; отсутствие поля означает неизвестный источник старого формата. `received_at` — время приёма сервером в RFC3339Nano; `at` остаётся временем наблюдения. На входе собственного туннеля сервер принудительно назначает `client` и своё время приёма, игнорируя присланные значения. Это не доказывает точность часов или содержимого клиентского наблюдения.

Источник входит в ключ дедупликации. Старый пустой источник и `client` разделяют пространство ключей для совместимости повторных отправок после обновления; при отображении происхождение старых записей не выдумывается. Счётчики разных источников не следует суммировать. Серверные адаптеры AWG/VLESS и объединение потоков пока в плане, наличие значения в схеме не означает реализацию сборщика.

Общий DNS-декодер сохраняет TTL, класс, A/AAAA/CNAME, код ответа и признак усечения. Сейчас сбор использует UDP/53; TCP reassembly, DoH/DoT и DNS routing не реализованы.

## Серверный наблюдатель QUIC/HTTPS (этап B)

Новые клиенты передают непрозрачную ссылку version/session/flow_id при открытии TCP, UDP DATAGRAM, UDP packet stream и bond. Это не согласие на сбор. Старые клиенты без ссылки сохраняют прежнее поведение; старые серверы игнорируют поле. ResearchReply сообщает `flow_observation_version=1`.

Сервер назначает собственные session/flow ID. `data.client_flow` содержит отдельно нормализованную по TLS-устройству ссылку на поток клиента. `origin=server_gateway` относится к времени, DNS и счётчикам серверного сокета; `app_source=client` явно обозначает, что имя приложения взято из клиентского сообщения, а не определено сервером. До получения имени остаётся `unknown`. Метаданные ограничены 1024 записями по 4 КиБ и сроком 2 минуты. Поздние метаданные не переписывают уже сохранённые raw-наблюдения; объединение истории остаётся этапом E.

`measurement_level=exit_socket_payload`: успешные Read/Write внешнего логического соединения (не подтверждение доставки приложению), UDP — число дейтаграмм, TCP packet counters = 0. Delta выходит раз в минуту и при закрытии. Окна помечены incomplete, поскольку начало соединения и пропущенные наблюдения не восстанавливаются задним числом. В фильтре `origin` можно выбрать источник; суммировать разные источники нельзя.

Сбор требует клиентского control consent и серверной политики. Согласие имеет 15-секундную аренду с обычным обновлением каждые 5 секунд; истечение/отзыв/смена generation отбрасывают неподтверждённое окно. VPN продолжает работать. Политика проверяется в фоновом обработчике перед записью, без файлового I/O на пути открытия потока. Учёт байтов использует отдельную короткую блокировку без I/O; файловая политика, JSON и запись не удерживают её. Переполнение DNS-буфера или отказ записи отражаются gap, когда возможно. До асинхронного установления идентичности и при исчерпании лимита наблюдателей сбор может отсутствовать; полный охват не гарантируется. Сохранение последнего окна при завершении процесса не гарантируется.

Лимиты: максимум MaxFlows наблюдателей на receiver, до 16 DNS-сообщений по 4096 байт на поток за окно; заполнение ведёт к пропускам диагностики, а не к остановке VPN. TCP DNS, DoH/DoT, AWG/VLESS adapters и автоматический merged-flow view не реализованы в этой итерации.


### Сводка приложений в WEB

На странице аккаунта сводка показывает уникальные потоки, DNS-запросы (без ответов), байты вверх/вниз и последнее событие. Считается весь выбранный период, а не одна страница истории; фильтр приложения не скрывает остальные строки сводки. Нажатие на приложение закрепляет его и источник в фильтрах истории. Общий UID показывается одной группой, без умножения трафика на число пакетов приложений.

Каждый источник и уровень измерения считаются отдельно. flow_open/delta/close одного FlowKey не увеличивают число потоков; суммируются только delta-счётчики. Поздние app-метаданные в выбранном окне могут уточнить группу потока. DNS-счётчик — наблюдённые сообщения запросов, включая повторные запросы; зашифрованный DNS не декодируется. Окна, пересекающие границу периода, учитываются целиком. Ограничения сводки: до 20 000 flow keys и до 3 секунд чтения; неполные наблюдения/пропуски и ограничение сводки явно отмечаются. ID, исходный адрес, интервалы и сырой DNS JSON доступны в раскрываемых подробностях компактной таблицы.
