# QUIC Lab 0.9.4

Android versionCode 65; coordinated Android and server release. Build 65 replaces build 64 with an Android download hotfix; server binaries are unchanged.

## Changes since 0.9.3

- Temporary WEB configuration editor with explicit phone consent, revision checks and lifecycle audit.
- Opt-in per-application traffic research for QUIC/HTTPS: flows, DNS, JSONL and account-scoped WEB statistics. Client analysis can cover VPN-only, VPN plus selected applications, or all applications; direct-traffic analysis requires separate consent. Server observations cover VPN traffic only. Client/server counters are separate measurement points and must not be added.
- Passive protocol evidence for DNS, HTTP, TLS and QUIC v1/v2 Initial; SNI and ECH-offered indications, offered ALPN, source/destination ports and connection summaries. No encrypted application-payload decryption. ECH may be GREASE; h3 offer is not proof of negotiated HTTP/3.
- APK downloads inside the application using a user-started HTTPS foreground transfer, progress/cancel/retry, package/version/signing-certificate validation and the standard Android installer. Installation remains user-confirmed. APK stays available in Downloads.
- Denser MDM configuration interface, overview and permission indicators, telemetry summary and navigation to device/account research.
- Research timestamps in browser-local time; period presets before application statistics; self-hosted JavaScript compatible with production CSP.

## Verification

- Fresh full Linux race regression: cmd/internal/mobile passed, using the existing targeted Xray inbound/outbound checkptr exclusions. These exclusions are test qualifications, not production build flags.
- Fresh Android native AAR build for arm64/amd64, assembleDebug, assembleDebugAndroidTest and lintDebug passed.
- Fresh MDM Chromium regression on release sources passed: desktop/mobile layout, draft/tab retention, checkbox mapping, preview and apply preserving routes/application settings.
- Independent release diff review found no Critical/Important issues. Minor deferred: research from/to form values lose fractional seconds when rendered; exact subsecond boundary preservation is not guaranteed.
- Final Note9 Android 11: APK installed over pre.7 with existing signature, profiles retained; ApkDownloadTest/ApplicationUpdatesTest/ProfileUpdateTest finished OK (20 tests, 3.994s; opt-in live cases remain conditional). VPN and MDM services restored through launcher/UI. Historical pre.6/pre.7 live protocol/download checks remain documented separately and are not claimed as repeated on every Android version.

## Build 65 download hotfix

- On Note12 / Android 12, DownloadManager left the update at zero bytes with an unsatisfied network constraint while VPN was active. Stopping VPN immediately allowed the same queued job to complete.
- New downloads run under the application's own UID, outside its TUN, without stopping VPN. The APK is checked before publication to Downloads; the standard installer still requires user confirmation.
- HTTPS-only redirects, bounded size, truncated-body detection, progress, cancellation and retry are retained. Process termination produces a clear retry state on reopening; partial bytes are not resumed.
- Existing completed DownloadManager jobs remain readable. Pending legacy jobs are replaced when starting the new updater.
- Note9 / Android 11: 24 core regression tests and four opt-in live download tests passed. Note12 / Android 12: eight tests including the complete APK download with an unchanged active VPN network passed. Build and lint passed; independent review reported no Critical/Important issues.
- Android 16 download UI was not repeated in this hotfix.

## Known limits

- QUIC UDP datagram discard counter increased during YouTube/ReVanced playback on October 13. The reason and relationship to reported stalls are unresolved; 0.9.4 does not claim a fix. Detailed drop reasons and per-flow stall diagnostics remain future work.
- This release does not close the deferred VPN mobility/failure/load matrix or certify a new eight-hour soak. Android 16 notification/install UX was not rechecked for this release.
- Full annotated research PCAPNG, server AWG/VLESS flow analysis, unified client/server counters and domain routing are not included.
- Windows/Linux client plans are separate and not part of these release binaries. APK preserves the existing project signing identity and debug build type; it is not a Play Store release.

## Supporting reports

- [APK downloads](testing/android-apk-download.md)
- [Protocol and QUIC research](testing/research-protocols-quic.md)
- [MDM UI](testing/mdm-compact-layout.md)
- [Client research scope](testing/client-research-scope.md)

## Deployment

Published server and APK on quic-lab.vpnc.ru; all four services active. Rollback preserved in `/opt/quic-lab/rollback/release-094-20261013`. APK SHA256 `46fce26d991b4d74652324235094c87eb872c390a6d6a18a676961d8e9f85ebe`; server amd64 binary SHA256 `749dd2db7007708d2ab0768d158ea18e548e6c4588264292c79b454c1121d886`.
