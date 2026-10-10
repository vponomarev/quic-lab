# QUIC Lab 0.9.2

Android maintenance release: restore active MDM and an interrupted VPN when Android recreates the process after system memory cleanup, including a background diagnostics entry.

- Recovery preserves the saved LTE budget and accounting period.
- Recovery requires existing VPN permission and an interrupted session from the same OS boot. An explicit Stop is respected.
- Xiaomi autostart permission is still required; the app does not bypass OS restrictions. Connections can be interrupted while Android restarts the process.
- Server protocol/configuration is unchanged and remains compatible with server 0.9.1; no server restart is needed for this client update.

APK versionCode: 54. Install over the existing application to preserve profiles and MDM enrollment.

Validation: 19 recovery/budget/MDM instrumentation tests passed on Android 11, 12 and 16. Live recents cleanup verified without reopening the app on Note14Pro (Android 16), Note12Pro (Android 12, about 16 seconds) and Note9Pro (Android 11, about 82 seconds). Note9 OS scheduled a delayed service restart; no immediate-recovery guarantee is claimed. Server reports confirmed running VPN after recovery. Build and lint passed.
