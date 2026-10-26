# Android 0.9.5-pre.9

Published 2026-10-18 as a preview update.

This version adds a single-run check for every configured VLESS source, including reserve nodes. Results identify the source and transport (TCP or WebSocket/TLS), and a run is limited to 256 nodes. VLESS import/probing now accepts WebSocket/TLS and IPv6 endpoints in supported subscription/Xray formats.

Validation: `go test -mod=readonly ./internal/vless -count=1`; Android `assembleDebug`, `compileDebugAndroidTestSources`, and `lintDebug` passed. The final instrumentation run on Note9 was blocked by Android returning `INSTALL_FAILED_USER_RESTRICTED`; Android tests compiled but were not rerun on-device after the last changes.

Version code: 74
Version name: 0.9.5-pre.9
APK SHA-256: `17f9b1d62e6a2a6366debfbee3d6fa08eb901187787418827a7888b026c08e0e`
