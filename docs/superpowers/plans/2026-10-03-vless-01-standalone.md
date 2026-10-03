# VLESS V1 — фиксированные профили и Android standalone

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development. Track completion with checkboxes.

**Goal:** Импортировать один VLESS-профиль и передавать TCP/UDP выбранных Android-приложений через внешний сервер.
**Architecture:** Ограниченный типизированный импорт; секреты в существующем зашифрованном хранилище. Адаптер Xray через публичные CreateObject/Dispatcher API предоставляет реальные deadline, TCP half-close и пакетный UDP. Физические сокеты остаются защищёнными, привязанными к Network и учитываются бюджетом.
**Tech Stack:** Go/Xray v1.260327.0, gomobile, Kotlin, Android VpnService.
**Spec:** ../specs/2026-10-03-android-vless-design.md

Согласованный roadmap: 2026-10-03-vless-00-roadmap.md. Продолжение реализации разрешено владельцем. V2 (управляемый сервер), V3 (demux), V4 (WS/XHTTP), V5 (подписки) остаются отдельными поставками.

## 1. Ограниченный импорт (GPT-6.1-sol)
Files: internal/vless/import.go, import_test.go; mobile/vless_import.go, vless_import_test.go.
- [x] RED: тесты URI и одного JSON outbound; TLS/REALITY/Vision; отрицательные — неизвестные параметры, дубликаты, chaining/routing, небезопасная TLS, неподдерживаемые транспорты, размеры. Linux test должен падать до реализации.
- [x] Реализовать ParseImport -> ImportedProfile{Name, Config}; канонический bridge для шифрованного хранения и отдельную безопасную metadata validation.
- [x] GREEN: Linux targeted test; ошибки не содержат входные секреты. Импорт не исполняет полный Xray JSON и не меняет его смысл молча.

## 2. Потоки Xray с корректной семантикой
Files: internal/vless/flow.go, flow_test.go, engine.go, engine_test.go, integration tests.
- [x] RED: deadline изменяет уже блокированный Read/Write, сброс deadline позволяет продолжить; локальный CloseWrite сохраняет ответ; remote EOF не поддержан upstream; UDP 1/2048/8190 bytes сохраняет границы; закрытие отменяет блокированные операции.
- [x] Использовать common.RegisterConfig + core.CreateObject для законного контекста движка; Dispatcher.DispatchLink с двумя ограниченными очередями buf.MultiBuffer. Никаких приватных core.XrayKey и изменений upstream.
- [x] Передача TCP ограниченными блоками, UDP одним Buffer (до 8190 bytes: подтверждённый предел выбранного Xray); очереди ограничены. CloseWrite завершает только uplink, Close освобождает обе очереди и отменяет поток.
- [x] GREEN: реальный локальный TLS VLESS TCP/UDP fixture, half-close и race; существующие socket isolation/cancellation/revoke тесты.

## 3. Gateway и модель выхода
Files: mobile/gateway.go, gateway_backend.go, gateway_vless.go/test; internal/vpnmodel/config.go/test.
- [x] RED: VLESS допустим только standalone, неизвестный/невалидный профиль не запускается; нет direct fallback; TCP/UDP используют VLESS, Stop и смена сети закрывают старые потоки.
- [x] Подключить новый backend, lifecycle, end-to-end health, exit IPv4, существующий бюджет. DNS endpoint разрешает Android Network; сохранённый SNI не заменяется IP.
- [x] GREEN: Linux gateway/model regression. Standalone reconnect может оборвать сессию; V3 ещё не реализован.

## 4. Android
Files: VlessImport.kt, VpnIdentity.kt, VpnProfiles.kt, profile import UI, VpnSession.kt, instrumentation tests.
- [x] RED: import/review/save, шифрованная идентичность, конфигурация сервиса, несовместимый тип маршрута.
- [x] Добавить URI/JSON import по образцу AWG; сохранять секреты только в encrypted identity. Отображать VLESS и поддерживаемую безопасность; не показывать UUID в диагностике.
- [x] VpnSession: выбранная Network, reconnect при смене сети, health через выход, исключить QUIC-only standby/migrate. Сохранить selected-app режим и fail-closed.
- [x] GREEN: Android assemble/lint/instrumentation; аппаратный standalone TCP/UDP и Wi-Fi/LTE при доступном телефоне. Реальные credentials только ignored artifacts и private app storage, очистить fixture.

## 5. Приёмка checkpoint
- [x] Linux full test/vet + targeted race; Android regression; обновить docs/vless-compatibility.md и roadmap по фактическим результатам.
- [x] Один свежий review всего изменения; исправить существенные замечания и проверить diff на секреты.
- [x] Локальный коммит; не выполнять production deploy/push/release без отдельного задания. Непройденные аппаратные проверки оставить явными.



Результат: V1 принят 2026-10-03. Linux full test/vet, targeted race, Android build/lint, 16 regression + 1 live VPN test PASS. Внешний профиль проверен также Linux HTTPS/UDP. Ограничения и доказательства: [совместимость](../../vless-compatibility.md). Следующий этап — V2, собственный управляемый сервер.
