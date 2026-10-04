# VLESS settings and Phase 1 closure — design

Date: 2026-10-04
Status: scope and linked written design approved in conversation on 2026-10-04.

## Intent and scope

Finish Phase 1 on Android/Linux, with a discoverable, usable editor for the existing managed VLESS server. One active VLESS inbound switches between TCP/RAW TLS and REALITY; simultaneous inbounds are explicitly unnecessary. Preserve device identities, revocations, existing imported profiles and other VPN services. VLESS multiplexing, subscriptions, WS/XHTTP and Windows remain subsequent work.

## Administrator experience

Place a visible «VLESS-сервер» navigation item near the top of the users page, with a return link. Use the existing admin visual language and three stable-size tabs: «Подключение», «TLS / REALITY», «Дополнительно». Retain form drafts across tab changes. Show configured/applied revision and errors without losing entered values. Never label an unconfirmed update applied.

Connection tab: public host/port, TLS or REALITY, client SNI, Vision. Explain that the public address is the socket destination and SNI is the handshake name. Security tab shows only the selected security's fields. TLS: certificate/key paths and certificate name/expiry validation. REALITY: target, allowed server names, key generation/replacement, public-key copy, short-ID generation/list and exported-ID selection. Existing private keys are never rendered. Generating a new key prepares a draft; saving requires an explicit key-change acknowledgement because existing client profiles need updates.

Advanced tab: supported fingerprint choices (current core contract: default/chrome/firefox/safari), SpiderX, internal listener, standalone/demux-only and demux target, common-port/PROXY state. Fingerprint and SpiderX are client export settings. Existing profile defaults remain unchanged. No enable-Mux action is added.

## Configuration and client compatibility

Extend the existing managed Config with explicit exported short-ID selection and SpiderX. Empty values migrate to the old behavior: first configured short ID and `/`. Selected IDs must belong to the allowed set. Keep existing key/ID validation and uniqueness. Carry SpiderX through URI and supported JSON import, profile persistence, native client configuration and exported QR/link. Test percent-encoded paths/query components and reject control characters and non-path values. Do not expose arbitrary Xray JSON or expand transports/encryption implicitly.

Generated X25519 keys and short IDs use cryptographic randomness. Generation/check endpoints require authenticated admin access and CSRF on mutations. Secrets and full connection URIs never enter logs. Existing private key is retained when replacement input is empty. Public key derives from the selected private key, never from a separately editable field.

## Apply, common TCP/443 and recovery

Own only the managed VLESS routing entries; preserve manual routes, web fallback and mTLS SNI. In REALITY, route all configured accepted server names, not just the one selected for export. Validate collisions with local web/mTLS names and manual routes before changing worker or durable settings. Preserve loopback-only backend and PROXY protocol when using the common frontend. Split-port deployments remain supported.

Introduce an apply coordinator between the admin store, worker and frontend routing. The worker and routing must acknowledge the same configuration revision before the UI reports success or exports the new profile. Serialize settings changes; retain revision concurrency checks. A failed application restores the previous VLESS configuration and routes using a new revision. Never roll back the whole identities store: concurrent revocations, users, devices and expiry changes remain authoritative.

Persist pending/previous configuration metadata so a process crash at every apply boundary can reconcile to a known configuration at startup. During uncertain apply state do not issue new connection exports as if they were confirmed. If recovery cannot be confirmed, expose a recovery-required state and keep VLESS admission closed; other services and admin access remain available. Service interruption for VLESS settings changes is acceptable and must be shown before applying.

Use a runtime managed-route overlay derived from authoritative VLESS state, avoiding a second independently editable copy in server.json. Migrate only the uniquely matching existing VLESS route; reject ambiguous ownership for manual resolution. Startup and installer must reconstruct the same effective routes and retain manually configured entries. Preserve optional web TLS 1.0, VPN mTLS TLS 1.3 and separate AWG port.

Certificate preflight checks key match, validity and selected SNI coverage. Display actionable errors without secret contents. Keep existing ACME renewal integration; changing SNI does not imply that DNS or certificate issuance has occurred.

A bounded target check tests only the administrator's specified REALITY target and SNI: reachability, TLS 1.3 and certificate-name validation. Use an overall timeout of 10 seconds, no redirects, no subnet scanning, bounded concurrency. Return observations, not a guarantee against blocking or future target changes. A transient network failure is shown separately from structural configuration invalidity.

## Validation and release gates

Regression tests cover defaults, key/ID generation, URI/JSON round trips and Android persistence. Apply tests cover conflicting SNI, bad certificate, worker rejection/timeout, frontend rejection, stale revisions, restart at each transaction boundary and concurrent revocation. Browser tests verify navigation, stable tabs, conditional fields, drafts and errors. Live checks cover our Android client and HAPP with TLS and REALITY, preserving the existing production TLS configuration after tests.

Then close the existing Phase 1 B4/E2 physical gates and E5 acceptance matrix: prepared path recovery <=1 second, cold allowed LTE <=10 seconds, size-selective and 1/2 MiB session blackholes, two exits, AWG, budgets, DNS/IPv6 exclusions, revocation and configuration update. Run 15-device progress and at least 30-device capacity at the agreed 100 Mbit/s server-link target. The configured admission limit must be >=30; rejection of client 31 is tested only in a fixture explicitly configured for 30. Larger configured limits are valid.

An 8-hour screen-off/traffic soak and measured resource/battery behavior remain required; a short smoke test is not a substitute. Run Go binaries, load/fault tools and server tests only on the isolated Linux test host. Windows handles edits, Android builds and USB/ADB. Mark unavailable physical tests not run and request the phone when necessary. Record exact commit/build/device/environment and outcomes. Existing accepted all-apps Android switching limitation stays documented.

After passing gates: independent code review, versioned client/server artifacts, release notes, GitHub release, production update and matching APK download. Follow existing deployment/recovery procedures and verify public APK version/hash. Do not label Phase 1 complete while required gates remain open.

## Self-review

Scope matches the approved single inbound. Secrets never enter public export. Managed SNI ownership, crash recovery and revocation-preserving rollback are explicit. No new transport or subscription work is smuggled into closure. Capacity wording reflects the user's minimum maximum of 30. Physical/long-run evidence is not replaced by unit tests. Implementation plan will name exact coordinator interfaces and fault-injection boundaries after this design review.
