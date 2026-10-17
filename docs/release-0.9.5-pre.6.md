# 0.9.5-pre.6 — VLESS subscription probing

Android and WEB MDM now expose one-page checks for imported VLESS subscriptions/groups. Check all nodes, retry unsuccessful checks, cancel, filter and sort by measured RTT. Results distinguish verified HTTPS, optional shared-exit authentication/data exchange, DNS/network/budget failures and interrupted checks. The phone performs the checks on its selected physical network, at most two nodes concurrently; working VPN profiles and sessions are not switched.

Android entry: connection editor or VLESS subscriptions → «Проверка серверов VLESS». WEB: MDM device → «Проверить серверы VLESS». Remote checks require active MDM, VPN and telemetry rights and client capability `vless_probe_v1`. A stopped foreground owner can require opening the app first. Measurements and server receipt time are shown separately in local browser time.

Historical results stay bounded and contain no subscription URI credentials. Old MDM epochs cannot poison normal device reporting. A resumed binding does not silently restart an old check.

See [test evidence and limits](testing/vless-subscription-probing.md). Note12 upgrade remains user-controlled; publication does not change its selected profile.

Published to quic-lab.vpnc.ru on 2026-10-17. Public capabilities advertise code 71 / 0.9.5-pre.6; public nonce echo and downloaded APK hash verified. Service active, no new error-priority journal entries. Rollback: `/opt/quic-lab/rollback/vless-probing-pre6-20261017`.

SHA-256:
- APK: `eeeeeac241ebf8b45a42578d4762ad9783938ff9c6f6626f064f3e21cc757c75`
- Linux server: `3c0b442cb72975cc156303cdd3c3e8b7f75d2d8f5d586e95b7e2342931b70e25`
