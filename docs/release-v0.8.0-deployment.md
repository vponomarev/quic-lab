# 0.8.0 post-deployment verification

2026-10-04, approximately 23:57 Europe/Moscow.

- Both phones report versionName 0.8.0 / versionCode 33.
- After the user unlocked and pressed Connect (MIUI rejected ADB input injection), Redmi Note 9 Pro showed active AmneziaWG over Wi-Fi, fresh RTT 18.1 ms and a resolved exit IPv4.
- Xiaomi 22101316UG showed active QUIC over Wi-Fi, fresh RTT 15.1 ms, traffic counters and the same resolved exit IPv4.
- Both VPN connections were left running. These observations are connection smoke checks, not a repeat of the eight-hour soak.
- Production TLS 1.0 handshake for quic-demo.vpnc.ru succeeded with certificate verification OK (OpenSSL legacy cipher security level explicitly selected).
- Production TLS 1.3 handshake for ui.quic-demo.vpnc.ru succeeded with certificate verification OK.
- Four deployed binaries byte-match the amd64 release archive. Public APK matches SHA256SUMS. GitHub reports matching digests for all five payload assets.
- No release binary or tag was changed after publication; this document is an additional deployment record.