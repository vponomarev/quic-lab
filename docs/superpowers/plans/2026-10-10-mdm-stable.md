# MDM stable release 0.9.1

Authority: accepted client-management design (2026-10-04), web management design (2026-10-08), subsequent user amendments; release explicitly authorized 2026-10-10.

## Task 1: Standalone management server
- Add explicit management_only startup and installer mode; no VPN/Echo listeners or workers, no VPN provisioning endpoints. Preserve default combined mode and backup support.
- Test runtime isolation and configuration/installer validation on Linux.

## Task 2: Acceptance
- Run server race/vet and installer/backup regression.
- Run Android MDM lifecycle/configuration/consent/report/telemetry suites on dedicated Redmi; verify process recovery and reboot without enabling VPN implicitly.
- Check editor conflict and layout; investigate outstanding system DNS probe.
- Android 14/15 runtime checks deferred by the user until a suitable device is available; this release records Android 11/12 evidence only.

## Task 3: Stable release
- Fresh review, fixes and verification; update acceptance records.
- Build 0.9.1 server archives and signed APK with next versionCode, backup production, deploy and verify public download/control/traffic.
- Commit/push source, tag and publish GitHub release with assets and checksums.
