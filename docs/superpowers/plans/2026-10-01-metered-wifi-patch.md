# Metered Wi-Fi preference patch — pre.2

Approved in conversation 2026-10-01. Bounded fix, before group C.

1. Reproduce current APK staying on LTE when Android Wi-Fi is metered, selected-app routing, persistent TCP; restore phone override.
2. Separate preferred Wi-Fi candidate eligibility while active on cellular from idle reserve permission. Preserve validation, successful path probing, dwell, backoff and disabled background LTE checks. Apply to ordinary QUIC/HTTPS/AWG, not new B4 policy.
3. Add policy coverage and opt-in real handover regression; verify on Android with metered Wi-Fi and default reserve settings. Run existing cold-standby delayed-response tests on Linux. Build/lint APK and focused lifecycle/configuration tests.
4. Release client 0.8.0-pre.2 (22), compatible with unchanged pre.1 server. Publish GitHub prerelease and update public APK atomically. Verify hashes and phone settings. Keep separate all-app limitation and bond hedge investigation explicitly out of this patch.
