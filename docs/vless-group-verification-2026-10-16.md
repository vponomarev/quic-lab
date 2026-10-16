# VLESS group verification — 2026-10-16

Android candidate: 0.9.5-pre.3 (68). Not published to production. Production quic-lab.vpnc.ru / 45.9.88.105 was unreachable from Windows and the test Linux host; user will investigate later. Note12 MDM configuration has not been changed.

## Implemented

Subscription selection offers separate profiles or one group of up to 32 candidates. Group refresh preserves profile ID, routing/application settings and current selection; temporarily missing selected members remain, deliberately unchecked available members are removed. Credentials and subscription URL remain in encrypted storage. Refresh is manual.

Two failed verified HTTPS probes rotate the VLESS engine without recreating the Android TUN. Candidate cooldown is 30 seconds; full-round exhaustion permits recovery on another available physical network, respecting its retry cooldown and LTE budget. New engine is constructed before old engine closes. Existing application streams may break during rotation. Recent index/count/reason/time events are bounded to 32 in MDM reports (VPN permission and server capability required) and recorded in normal diagnostics. Server rejects group configuration for old clients lacking vless_group_v1, including inherited pending credentials.

## Evidence

- Linux: go test -race ./internal/mdm ./mobile -count=1 passed; go test ./internal/admin -count=1 passed. Server cmd/server builds. No live production installation performed.
- Android: assembleDebug, assembleDebugAndroidTest and lintDebug passed.
- Note9 dc69eb2c: 20 instrumentation tests passed (group policy 4, subscriptions 11, encrypted configuration store 5).
- RED/GREEN confirmed for long-round exhaustion, missing-member retention and old-client capability rejection.
- Live VpnSession test: deliberately unreachable candidate started at 23:40:21; failed probes at 23:40:33 and 23:40:45; switched to candidate 2 at 23:40:45; verified HTTPS healthy at 23:40:46 (device time). Group apply → snapshot validation → refresh preserved current profile and profile count.
- Real Android TUN test on Note9: unavailable first candidate → automatic rotation → captured companion HTTPS request to api.ipify.org and UDP DNS to 1.1.1.1 both passed. Completed in 33.111 seconds. Temporary test profile removed and previous profile selection restored.
- Independent source review: snapshot canonical equality, backup-network cooldown, client capability gating, and missing-member preservation findings corrected; no remaining P1/P2 in reviewed scope.

## Limits / remaining deployment

Live checks used home Wi-Fi and a deliberately unreachable address, not the specific blocking conditions around Note12. Not every provider endpoint was tested. There is no guarantee of uninterrupted existing TCP streams. MDM group events are recent in-memory history; persistent evidence remains in the diagnostic journal. Automatic periodic subscription refresh is not part of this version.

After production returns: deploy the matching server, publish APK and signed update metadata, wait for Note12's new capability report, then add the private subscription as a second MDM profile while preserving existing primary profile, applications and permissions. Do not send a remote VPN-start command before the user's update and MDM activation.

Local APK: artifacts/quic-lab-0.9.5-pre.3.apk.
SHA256: 3dcdd293ce021573c51b5ae112a09a4ba404951e28f1a940e841f0d26e978799.

After tests the previous AmneziaWG profile was selected and its VPN foreground service restarted. Successful connection to the unavailable production server is not claimed. Note9 MDM remains paused from the authorized tests; binding and permissions retained.


## Production restored and updated — 2026-10-17 (Moscow)

User power-cycled the VM. SSH and Web UI recovered; all four services active and no failed systemd units. Deployed exact commit 9a96840 server build with BuildVersion 0.9.5-pre.3 and the tested APK (68). Previous main binary, APK and both capability configurations saved in root-only /opt/quic-lab/rollback/vless-group-pre3-20261017. Only the main service was restarted. Public APK download SHA256 matches the value above; both admin/server capability configurations advertise version 68. Served MDM editor contains group events support.

Note12 still reported 0.9.4 with no vless_group_v1 capability at deployment. Its MDM profile has NOT been changed. Remaining action: user updates through the app and enables MDM; after a fresh capable report, create the second group preserving the newly reported current settings. No remote start command sent.


## Note12 MDM delivery — 2026-10-17 (Moscow)

After user updated and enabled MDM, fresh device report advertised 0.9.5-pre.3 and vless_group_v1. Fetched the private subscription in memory and validated candidates with the same Go VLESS parser; 25 supported unique candidates, 15 unsupported skipped. Posted a checked MDM update through authenticated Web API with CSRF, revision/generation preconditions. Added Liberty group as a second profile, retaining original primary, full original profile and global application list; no VPN-start command issued.

Device acknowledged appliedRevision 5 at 2026-10-16T21:44:52.619839003Z and reported both profiles. Primary and global app list verified unchanged. VPN reported stopped. Group is available for user selection; real failover under Note12's blocking conditions has not yet been observed. Private preparation snapshot retained root-only on server; no subscription credentials included in repository.
