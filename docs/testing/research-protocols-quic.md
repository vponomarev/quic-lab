# Protocol detection and QUIC Initial research

Approved scope: DNS, HTTP, TLS, QUIC v1/v2 evidence; ALPN is offered, never asserted negotiated. Shared bounded client/server observer; client direct under existing consent only; server VPN only. No payload persistence, no TLS application decryption. QUIC Initial CRYPTO reassembly handles fragmented, reordered and repeated data with bounded resources. ECH extension is offered and may be GREASE, inner SNI unavailable.

Implementation ledger:
- QUIC parser delegated to isolated agent; parent runs Linux tests. Build host is existing authorized 192.168.5.214.
- Model/UI/query implementation delegated separately; existing source-port and pending-report changes preserved.
- Parent owns shared protocol observer, lifecycle/consent integration, APK/server builds and physical acceptance.
- All protocol names require payload evidence; ports only scope DNS parsing. Unknown remains unknown. Initial collection window and memory/work caps preserve forwarding priority.

## Implementation and checks, 2026-10-13

- `ProtocolProbe` recognizes DNS messages, HTTP/1 request lines, TLS ClientHello and authenticated-format QUIC v1/v2 Initial packets. Initial keys are public: this is protocol evidence, not peer authentication.
- QUIC CRYPTO reassembly: 32 KiB content plus 32 KiB presence map, 64 datagrams and a 10-second collection window. Duplicate/reordered fragments are supported; conflicting overlap and malformed frames stop inspection. Raw prefixes are cleared on completion and consent revocation; payload forwarding has priority.
- Protocol evidence is exported as `data.detected_protocol.{name,evidence}`. TLS metadata remains in `data.tls`, including `server_name`, `ech_offered`, `alpn_offered` and `versions_offered`. `h3` is an offer, not proof of negotiated HTTP/3. `<ECH>` may be GREASE; hidden SNI is not recovered.
- Table and protocol filter work with legacy records lacking metadata, and preserve client/server separation. Late metadata enriches a connection before table/summary filtering. Raw JSONL filtering remains event-level.
- Published RFC 9001 / RFC 9369 Initial packet vectors, synthetic fragmentation/reordering/retransmission/coalescing/ECH and packet number rollover passed. FuzzQUICProbe: 562,374 inputs in 20 seconds, no failures.
- Client/server UDP integration tests passed for v1, v2 and ECH, including unchanged byte counters and revocation cleanup. Full Linux Go race suite passed (existing Xray inbound/outbound checkptr exclusions).
- APK 0.9.4-pre.6 (62) built successfully. Physical acceptance uses debug-only probes with public synthetic Initial packets (`api.example.org`, offered `h3`) over UDP echo, not full QUIC/ECH negotiation. Production application contains no fixtures.
- First physical round: 30/30 data exchanges passed. Eight seconds before profile restoration was insufficient for periodic UDP metadata reports; acceptance harness now waits 70 seconds per transport. This is a test timing correction, not evidence of successful metadata delivery by itself.

## Acceptance and publication

- Final Note9 round: 30/30 exchanges passed over QUIC and HTTPS, with VPN and direct probe UIDs. Waited 70 seconds for UDP reporting before each restore. Preparation/restoration tests passed; original profile restored; VPN and MDM foreground services confirmed. Note12 was not connected during this iteration.
- Live JSONL verified all three sources/routes (client VPN, client direct, server VPN) for QUIC v1, QUIC v2, ECH and HTTP. No direct observations on server. Protocol filters and JSONL event filtering passed. Real background QUIC ClientHello also yielded offered h3 and ECH metadata; this does not prove ECH negotiation.
- Published production server and APK on quic-lab.vpnc.ru. Server SHA256: `47545f1e7bf1d0d572a5c4fc2994e111fcb95790558479a5c9c41ad4ece051eb`. APK SHA256: `5ab478774970b4ed9c49299e0dc5ae6cd0bef84270239855d39577f19d4e4785`, 58,669,040 bytes.
- Corrected stale public capabilities (previously 0.9.3/56) to match published 0.9.4-pre.6/62. Verified public metadata and full HTTPS APK download checksum. Main, VLESS, AWG and transit services active; no error-priority entries after restart. Previous binary, APK and server config preserved in root-only rollback directory `protocol-pre6-20261013`.
- End-to-end protocol evidence was verified on the isolated test server. New production observations were not required for deployment and are not claimed as verified.

## Local research timestamps, 2026-10-13

Research tables, application statistics, gap timestamps, coverage/window boundaries and event headings render browser-local time from RFC3339 time elements. Browser zone is explicit; raw JSON/JSONL remains untouched. From/to filters use local datetime inputs, translating edits to UTC; unchanged filters retain original precision. Chromium checks passed for Europe/Moscow and America/New_York, date rollover, changed/empty filters and unchanged nanosecond timestamps. Admin suite and server build passed. Deployed with rollback in /opt/quic-lab/rollback/local-time-20261013; services active.

## Quick periods, 2026-10-13

Nine preset buttons above application stats select a fixed UTC from/to interval ending at click time: 5/15/60 minutes, 6/12/24 hours, 2/7/14 days. Preserve form filters, reset cursor, highlight selected interval. Refresh-period explicitly moves it to now; links/export keep the same snapshot for consistent results. Editing local dates clears preset. Browser checks passed for every duration, app/route/tab preservation, pagination reset, active highlight, refresh and manual override. Admin suite passed (10.951s); build passed; deployed with rollback /opt/quic-lab/rollback/periods-20261013.

## Period panel CSP correction

Initial synthetic browser checks omitted production CSP and missed blocked inline JavaScript (also affected local time). Reproduced with failing template test. Moved JS to self-hosted research-ui.js; panel rendered visibly before Applications instead of relying on JS relocation. Admin suite passed (10.956s). Chromium tested actual Go-rendered HTML under exact production CSP: visible ordering, local Moscow timestamp, 15m navigation and selected state all passed. Deployed; /lab/research-ui.js returned HTTP 200 with expected contents. Rollback: /opt/quic-lab/rollback/periods-csp-20261013.
