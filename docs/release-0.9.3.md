# QUIC Lab 0.9.3

Android configuration transfer release (versionCode 56).

- Settings → Import and export: transfer all settings or selected profiles, routing, network/DNS/budget settings and diagnostics.
- Credentials are excluded by default. Including VPN keys requires password-encrypted export; secret-bearing plaintext is rejected.
- Import supports explicit add/update/replace mapping, a redacted preview, confirmation and rollback of the last import. Device registration, MDM binding/consents and traffic counters are not exported.
- Stale previews and missing applications are rejected. Active MDM ownership prevents local configuration replacement.
- Durable import recovery respects manual Stop, device reboot and management changes. Import can reconnect the VPN; seamless traffic is not guaranteed.

Validation before release: two-way encrypted synthetic VLESS transfer between Android 11 and 12, wrong-password rejection, private update-token exclusion and exact rollback; 23 focused regression tests on each phone; actual process termination before/after configuration publication in an isolated fixture. UI export through the Android document picker was verified.

Known acceptance limits: a complete UI import/apply sequence has not been verified; ADB selection in the file picker did not advance and active MDM blocks local apply on the test phone. Crash tests use a simulated VPN port and do not prove uninterrupted traffic during import. Android 16 transfer-specific acceptance is pending. Publication of this bounded scope was requested after these limitations were reported.

Temporary WEB editor and remaining tasks in the configuration-transfer/editor plan are not included. Existing server 0.9.1 remains compatible; no server protocol change is required. Deployment refreshes the advertised APK version/hash in server capabilities and briefly restarts the service. Install over the existing app to preserve profiles and MDM enrollment.
Release build: assembleDebug/assembleDebugAndroidTest/lintDebug passed. The final APK passed23 focused tests on Android11 (6.918s) and Android12 (2.451s). Full Go regression passed on Linux. APK signing certificate matches0.9.2.
