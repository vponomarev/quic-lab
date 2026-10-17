# 0.9.5-pre.7 — Proxy SNI in VLESS probe results

Android and WEB display «SNI прокси» below each node endpoint. Probe JSON carries optional `serverName`, taken from the canonical VLESS proxy configuration (TLS/REALITY). This is not the inner HTTPS echo target or shared-exit SNI. Projection includes no proxy credentials. Existing snapshots remain readable and show a dash until a new check reports the field.

Validation on 2026-10-17:
- New JSON round-trip regression failed before implementation, passed after it; stored snapshots preserve SNI; absent fields remain valid and oversized values are rejected.
- Linux race tests: internal/mdm, internal/admin, cmd/server passed.
- Android assembleDebug, assembleDebugAndroidTest and lintDebug passed; Note9 instrumentation: 9 tests passed. Its original AWG VPN is connected after testing; MDM remains paused.
- JavaScript syntax and git whitespace checks passed.
- Windows full MDM tests could not run due to directory fsync Access Denied; the Linux suite above passed.

Published server and Android code 72 to quic-lab.vpnc.ru. Public capability version and downloaded APK SHA-256 verified. Note12 update is user-controlled; run a fresh probe after updating.

SHA-256:
- APK: `ffa994ca62d8967aeb4b8e5bc5ce368ee57a94d58e9762f34955bda79792c012`
- Server: `7b16a37d103050d71ba7da73b64df34d2d5f176b66069290eefd76e86ff42f47`

Rollback: `/opt/quic-lab/rollback/vless-probing-pre7-20261017`.
