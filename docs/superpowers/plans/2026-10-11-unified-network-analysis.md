# Unified Network Analysis Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans; record evidence and keep future stages explicit.

**Goal:** Объединить декодирование и данные разных источников, сохранив локальный DNS для будущей маршрутизации.
**Architecture:** Чистое общее ядро, протокольные адаптеры, наблюдения с источником, серверное объединение и независимые потребители.
**Tech Stack:** Go/gomobile, Kotlin, существующие JSONL/HTML и x/net/dnsmessage; без новых зависимостей.
**Spec:** [Согласованная архитектура](../specs/2026-10-11-unified-network-analysis.md).

## Global Constraints
Go-тесты только на Linux 192.168.5.214. Android/ADB через Windows. Исследовательский сбор off by default; локальный декодер не имеет побочных эффектов. Production и телефоны не обновлять в этой подготовительной итерации. Не суммировать разные источники/уровни байтов. Старые записи читаются без выдуманного происхождения.

## Review Focus
Поддельный origin от клиента; переполнение DNS counts/compression; потеря TTL/CNAME/class; двойной учёт после обновления старого клиента; отсутствие разрешения на исследование не должно мешать локальному разбору.

## A — исполняемая первая итерация
- [x] A1. Написать RED-тесты `internal/netobserve/dns_test.go`: A/AAAA/CNAME/TTL/class, NXDOMAIN/TC, повреждённые и чрезмерные сообщения. Добавить типизированный `DecodeDNS`, сохранить совместимый JSON через `Fields`. Перевести `mobile/research_dns.go` на общее ядро; прежние mobile-тесты остаются регрессией.
- [x] A2. RED-тесты `internal/research/provenance_test.go` и `internal/gateway/research_origin_test.go`: optional origin/received_at, старые архивы, независимые пространства источников, сервер переопределяет клиентскую подделку. Добавить происхождение в Record; gateway назначает client и время приёма. Показать источник в таблице и сохранить его в JSONL.
- [x] A3. Linux race: netobserve/research/gateway/admin/mobile. Сборка Android AAR/APK и lint. Проверить SVG и документацию, провести ревью, зафиксировать результат.

## B — серверный QUIC/HTTPS
- [ ] Привязать application metadata к серверному logical flow через расширение открытия потока с версией/capabilities; старые клиенты продолжают работать.
- [ ] Ограниченный асинхронный серверный наблюдатель TCP/UDP и DNS; отдельные счётчики наблюдения и время приёма.
- [ ] Проверить consent generation/revoke на нескольких путях, server/client byte semantics, перезапуск и пропуски. Никакого двойного учёта.
- [ ] Принять Note9/Note12 на стенде, затем отдельно подготовить выпуск.

## C — AWG
- [ ] Адаптер после AdmissionTUN peer validation до NAT; учёт IP-пакетов с честным measurement_level.
- [ ] TCP состояния/повторное использование tuple/ретрансляции/фрагменты, UDP idle timeout; ограниченные таблицы и reassembly.
- [ ] Отдельная серверная opt-in политика пользователя; проверить альтернативным клиентом.

## D — VLESS
- [ ] Обнаружить точку authenticated logical connection в embedded xray-core; обычных access logs и user totals недостаточно.
- [ ] События потоков и ограниченный DNS-парсер, сохранив пользовательскую идентичность через multiplexing.
- [ ] Проверка TLS/REALITY нашим клиентом и HAPP; unknown application не превращается в выдуманное имя.

## E — объединение и маршрутизация
- [ ] Точное соответствие ID, выбор основного источника счётчиков, сохранение конфликтов и gaps.
- [ ] Таблица объединённых потоков + экспорт raw observations/merged flows с происхождением каждого поля.
- [ ] Локальный DNS-кеш: TTL/CNAME, разделение по выходам, отрицательные ответы, смена сети, неоднозначные IP. DNS routing тестировать при выключенном research.

## Результат итерации A — 2026-10-11

Реализованы типизированный ограниченный DNS-декодер, использование его мобильным сборщиком, origin/received_at, принудительная маркировка клиентских записей сервером, совместимая дедупликация и отображение происхождения в админке. Новые значения схемы для серверных источников зарезервированы; адаптеры B–D пока отсутствуют.

Проверки: RED новых тестов подтверждён на Linux до реализации; `go test -race` для netobserve/research/gateway/admin прошёл. Первый общий mobile race-прогон дал таймаут `TestBothPathsTemporarilyUnavailable`; пять отдельных повторов и повторный полный mobile race-набор прошли (53.537s). Причина одиночного таймаута не установлена; не выдаём повторный успех за исправление этого теста. Android AAR, assembleDebug, assembleDebugAndroidTest и lintDebug — успешны. SVG отрисован Chromium и проверен визуально. Независимое ревью кода — без замечаний.

На телефоны и production эту подготовительную итерацию не устанавливаем. Следующий шаг — B: серверный наблюдатель QUIC/HTTPS и точная связь app metadata с серверным logical flow.
