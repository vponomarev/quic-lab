# Windows settings and standalone transports — 2026-10-14

Development build on `codex/windows-client-beta`; installed locally for acceptance. This does not replace the published `windows-v0.1.0-preview.1` archive.

## User interface

Open **Настройки подключения** from the compact dashboard. Edit connection name, transport, endpoint, physical interface and selected IPv4 prefixes. The subnet tab supports multiline/paste input, individual edits/removal and at most **256** canonical nonoverlapping prefixes. Default routes and IPv6 are excluded; system DNS stays unchanged.

QUIC/HTTPS accept client certificate/key and optional trust root. For AWG select AmneziaWG, open a `.conf` (or paste it into the masked profile field), then **Проверить и разобрать профиль**. For VLESS paste a `vless://` URI and use the same preview. Review proposed routes before saving. AWG AllowedIPs are never installed silently, and selected routes must be covered by peer AllowedIPs. An imported default route is not accepted by this preview.

The top **Загрузить параметры по URL** section accepts an HTTPS link to a QUIC Lab invitation (`.../enroll#token`) or a direct QUIC Lab/Windows profile JSON, AWG configuration or single VLESS URI file. Download fills the draft; a multi-transport QUIC Lab profile offers a transport selector. Local subnets and physical interface are retained. Registration happens when downloading an invitation, not when saving; the screen states this explicitly. A persistent installation identifier derives the same request ID for a repeated invitation, so response-loss retries do not consume another registration. No invitation token or downloaded credential file is saved by the downloader. Imported credentials reach encrypted service storage only on Save. Background refresh/subscriptions are not part of this feature.

Downloads use normal HTTPS certificate verification, no redirects or cookies, a 15-second whole-operation deadline and a 128 KiB response limit. Cancel remains available during download. Loader tests cover invitation POST/body and token-free URL, retry identity, raw/JSON formats, invalid URLs, HTTP errors, oversize data and cancellation. WPF acceptance confirms draft-only import, transport selection and preserving routes/interface after Save; the full dashboard suite passes. Independent review found no actionable issues.

Only Save commits the draft. Saving while connected asks permission to reconnect. Closing the editor does not stop VPN. A concurrent edit produces a revision conflict and cannot be overwritten by merely retrying Save. Reopen the editor to load the newer settings. “Движок запущен” reports engine lifecycle, not verified peer authentication.

One active saved profile is supported. Endpoint, name, routes and interface can be edited directly; changing secret-bearing AWG/VLESS protocol parameters currently requires replacing the import. There is no subscription import or arbitrary Xray JSON execution.

## Supported transports

| Transport | Supported profile | TCP/UDP | ICMP |
|---|---|---|---|
| QUIC / HTTPS | QUIC Lab certificate profile | Yes | Echo and router errors, TTL preserved by compatible server |
| AmneziaWG | Legacy Jc/Jmin/Jmax/S1/S2/H1–H4 `.conf` | Yes | Actual IP exchange inside AWG, TTL and router errors preserved |
| VLESS | TCP/raw with TLS or REALITY; existing parser validates flow and fingerprint | Yes | Unsupported; no fabricated reply |

AWG 2.x-only extensions and VLESS WS/gRPC/XHTTP are rejected. There is no app routing, MDM, research collection, kill switch or sleep/network recovery promise. A selected physical IPv4 interface is required. Hostname endpoints resolve through it before routes are installed; resolving into a selected prefix is rejected. Traffic totals count payload observed by the bridge, not link overhead.

## Persistence and credentials

The owner-authenticated IPC exposes redacted settings. Unchanged keys stay in the service; replacement is explicit. The service stores the document and revision in DPAPI-protected `settings-v1.dpapi` under its restrictive data directory, with atomic replacement. Legacy `engine.json` is migrated once; an existing corrupt new store never causes fallback to the legacy configuration. The legacy file remains protected as a migration backup.

AWG configuration and VLESS URI reuse the protected credential slot, preserving existing redaction/replace semantics. Custom VLESS TLS CA is available in advanced options; switching transport clears stale CA. Trust and route validation occurs before stopping a running connection.

## Verification

- 256 actual Wintun routes: configure/start, ping over selected route, stop and cleanup; original profile restored. 257 routes rejected.
- Settings manager/protocol race tests: revision conflict, replacement/keep-key, validation before stop, failed persistence, cleanup refusal and Stop cancellation.
- Native DPAPI, encrypted restart/corruption, one-time migration, cancelled-save preservation and named-pipe identity/authorization tests passed.
- WPF tests: compact dashboard, settings load/save/dirty tracking, stale conflict retry, transport CA transition and polling/close/reopen passed.
- Installed final service: restart retained original profile/key, edit/read/reconnect and restore passed. Real QUIC HTTPS request with expected exit, DNS TCP/UDP, 32/1200-byte ICMP and Time Exceeded from multiple routers passed. TX/RX nonzero; VPN stopped afterward.
- Independent Windows AWG fixture: real TCP and UDP echo, nonzero payload counters, ICMP Echo and adapter cleanup passed. Packet-level race tests additionally verify original TTL, actual reply TTL, translated source/identifier, router error restoration, cancellation and closure.
- Independent Windows VLESS fixtures: TLS and REALITY HTTPS expected exit plus DNS TCP/UDP passed; wrong UUID failed and cleaned up. Final separate TLS/REALITY counter checks each observed TX=413 and RX=3627 bytes and clean adapter teardown. No production server changes were required.
- Bound hostname resolution/cancellation passed natively on Windows.
- Linux race suites for windowsengine, windowsclient, AWG/netstack, icmprelay, VLESS and relevant mobile gateway tests passed. VLESS uses the pre-existing narrowly scoped Xray checkptr exception (`-gcflags=github.com/xtls/xray-core/proxy/vless/...=-d=checkptr=0`); race detection remains enabled.

Fresh review found and fixed stale-editor revision adoption, AWG ICMP interception and premature connection-success wording. Stale retry and ICMP behavior were reproduced with failing tests before fixes. The final local build preserves the user profile and does not auto-enable VPN.

## Implementation decisions

- Reused the existing protected credential field for imported profiles instead of a second credential store; protocol edits require reimport.
- Reused the shared mobile gateway and existing Go transport cores; Windows socket binding is platform-specific, Android entry points retain their behavior.
- Replaced shell-specific plan bookkeeping scripts with the equivalent local ledger on Windows; no product behavior difference.
