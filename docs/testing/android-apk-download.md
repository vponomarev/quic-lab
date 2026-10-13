# In-app APK downloads — 0.9.4-pre.7 (63)

Approved flow: Update downloads to public Downloads using Android DownloadManager, displays progress/cancel/retry, verifies package/signing certificate/version, and opens the Android package installer from the visible update Activity. Background completion sends an install notification. Unknown-source permission is requested through system settings; returning with permission continues without re-downloading. Cancelling installation keeps the public APK.

Implementation: ApkDownloads persists the DownloadManager ID and offer. Installer receives a validated private copy through the existing narrowly scoped FileProvider. Copy is capped at 256 MiB; stale private staging copies are removed after one day. No browser or new storage permission. Receiver is exported for DownloadProvider's distinct UID but only accepts the completion action for the persisted owned download ID and verifies DownloadManager status. Android 13+ notification permission is requested in foreground; denial has a visible manual-return fallback.

## Verification, 2026-10-13

- Initial test build failed because ApkDownloads did not exist (red); implementation and instrumentation APK then built.
- Note9 Android 11: package/signature/version rejection and actual installed APK validation passed.
- ApkDownloadTest + ApplicationUpdatesTest + ProfileUpdateTest: OK (19 tests; opt-in live tests skipped). Initial run had seven ProfileUpdateTest setup failures because IsolatedContext inherited real noBackupFilesDir and read actual MDM state. Added a noBackupFilesDir override in the test context; production authorization is unchanged. All 19 passed on repeat.
- Real HTTPS staging DownloadManager run: OK (4 tests). Verified public download completion, actual receiver notification, duplicate-start reuses ID, private-copy validation/content URI grant, HTTP 404 feedback, retry creates a fresh ID, cancellation clears it.
- Read-only independent code review: exported-receiver and notification-permission findings fixed; recheck found no further blockers.
- APK SHA256 f7812343417dbd347fef856db191cf0c7d135f509de61ff4dd8544c7223e9931; 59,927,920 bytes. Installed on Note9.
- Production APK replacement before acceptance was rejected by automatic approval review. Used separate temporary HTTPS staging endpoint on port 18457 (30-minute lifetime); primary APK/version metadata remain pre.6. No bypass of that restriction.

## Physical acceptance and publication, 2026-10-13

- Note9 Android 11: denied unknown-source permission leaves APK available with explicit explanation. User granted permission; system installer opened from app using downloaded file.
- Cancelled installer and reopened successfully without downloading again. Play Protect scan completed, then system installation of pre.7 over pre.7 succeeded (lastUpdateTime 16:46:28 Moscow, versionCode 63). This is an installer-path acceptance, not a pre.6-to-pre.7 upgrade test.
- Rotated update screen to landscape and back: completed download/status/install button retained. Restored user_rotation=0 and original screen timeout 60000 ms.
- Existing redmi note 9 pro profile retained. Restored AmneziaWG VPN through normal UI; connected, RTT 26.1 ms and exit IPv4 visible.
- Published accepted APK on quic-lab.vpnc.ru. Public download SHA256 and capabilities apk_sha256 both match f7812343417dbd347fef856db191cf0c7d135f509de61ff4dd8544c7223e9931; versionCode 63 / 0.9.4-pre.7. Corrected initially stale checksum metadata during verification. Previous APK/config retained in root-only rollback directory /opt/quic-lab/rollback/apk-pre7-20261013.
- Main, VLESS, AWG and transit services active after publication. Temporary staging server stopped.
- API33+ notification permission UX has not been tested on physical hardware in this iteration. Note12 not attached. Older installed versions need one final update using their previous download flow; subsequent updates use the new in-app flow.

## 0.9.4 build 65 — active-VPN download fix (2026-10-13)

Production Note12 (Android 12) had DownloadManager job 944 pending, zero bytes, unsatisfied CONNECTIVITY despite validated LTE. The job selected the VPN network while the application UID was excluded from that tunnel. Disabling VPN immediately started the same job; 58,701,896 bytes then completed with the release checksum. This proves the active-VPN scheduling interaction; the precise MIUI/framework defect is not claimed as isolated.

New downloads use an application-owned foreground dataSync service and HTTPS connection. No VPN settings or routes change. A successful private download is package/version/signer validated, then published to public Downloads with MediaStore IS_PENDING. Installation uses the existing immutable private copy and standard installer. Interrupted processes become FAILED/retry on reopening; retry starts from zero. Existing DownloadManager successes remain supported.

Evidence: baseline regression tests first failed to compile because ApkTransfer did not exist. assembleDebug/assembleDebugAndroidTest/lintDebug then passed. Note9: 24 regression tests, plus 4 explicit live tests (full candidate, unchanged VPN network, completion notification, validation, 404/retry/cancel). Note12: 8 tests, including the full HTTPS candidate with VPN continuously present and unchanged. Assertions use network identity, not merely a service-running label.

Candidate test endpoint was a temporary HTTPS server restricted to two APK paths, using the existing server TLS certificate locally without exporting it. It is stopped after validation. Main public update URL remains unchanged.

A real process-lifetime check on Note9 started a throttled download, ended the instrumentation process while bytes were arriving, then ran a new process. Reopening reported retry; retry downloaded and validated the complete APK. Both stages passed (1 test each; recovery 47.236 seconds).
