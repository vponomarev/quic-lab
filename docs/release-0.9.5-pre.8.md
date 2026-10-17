# 0.9.5-pre.8 — Probe stages and independent proxy exit IPv4

Android and WEB show endpoint DNS, observed protected outer TCP connection, HTTPS result/last stage, inferred proxy traversal, and independent proxy exit IPv4 from https://api.ipify.org?format=json. JSON/MDM retain the optional fields and accept older reports. IP is explicitly the VLESS proxy exit, before any configured shared QUIC Lab exit.

The ipify request runs concurrently with our nonce echo through the same VLESS client and selected protected/metered physical network. It has its own 8-second bound; the node remains bounded by 20 seconds. No direct fallback or redirects. Response body is capped at 1024 bytes and IPv4 validated. Public HTTPS uses system roots independently of the echo target's optional private CA. Failure of either target preserves the other's result.

Xray does not expose an outer TLS/REALITY completion callback at this boundary. Successful verified inner HTTPS confirms traversal; otherwise the UI explicitly says unconfirmed. The last inner HTTPS stage distinguishes proxy/target TLS from waiting for HTTP and reading the response; it must not be interpreted as proof of a specific outer handshake failure. Outer TCP observation aggregates the node's actual connections.

Validation:
- New stage regression failed before implementation, then passed.
- Linux race tests: probe mobile tests, internal/mdm, internal/admin, cmd/server.
- Real VLESS TLS with independent ipify request and private-CA echo passed on the Linux test host; external IPv4 observed.
- Fault tests: unavailable echo with working IP, malformed/private/oversized IP, certificate rejection, cancellation and real carrier/shared exit.
- Android AAR/APK/instrumentation APK and lint built; Note9: 10 focused tests passed.
- WEB visual check: mixed success/failure and legacy reports.

APK code 73. Note12 update remains user-controlled; a new probe is needed for stage/IP fields.

Note9 real REALITY fixture: 1 additional device test passed (68.67 seconds), covering probe alongside persistent TCP and reserve-path transfer. Production server published 2026-10-17; service active. Public capability version/code and downloaded APK hash verified.

SHA-256:
- APK: `09dc332d27adf7121b119c578f35dde54d833483c311057b29e5c0255ee4d586`
- Server: `5bf2422b0f5c222102609ab928e7a423192cce973a666438efaf4992f5deb9ad`

Rollback: `/opt/quic-lab/rollback/vless-probing-pre8-20261017`.
