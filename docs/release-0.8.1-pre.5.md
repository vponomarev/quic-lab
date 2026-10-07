# 0.8.1-pre.5
Android versionCode: 38.

The application update check now immediately shows progress and disables duplicate clicks. On completion it reports available updates or a current version, with a timestamp. Failed metadata checks identify the server and distinguish DNS, timeout, TLS, connection, HTTP and invalid response errors. These errors do not determine VPN tunnel health.

Also includes the server admin fix for revoking devices and invitations without navigating away from the current dialog/tab.

Validation: assembleDebug, assembleDebugAndroidTest and lintDebug passed. Three Android instrumentation tests passed on Redmi Note 9 Pro, including visible progress and disabled check button. Manual metadata fetch from the phone succeeded. Admin browser regression and Go admin tests passed for the revocation fix.
