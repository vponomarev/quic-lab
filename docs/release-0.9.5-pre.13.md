# QUIC Lab Android and server 0.9.5-pre.13

Intermediate preview, 2026-11-05. Android version code 78.

## Changes
- Multiple named AWG server exits, administrator access grants and device selection from Android, Windows and Linux. Failure does not silently fall back to another exit.
- Administrator-approved editing of VPN configuration from the Android MDM editor, including profiles, keys and portable JSON/age imports. Edits are submitted to the server and applied through normal provisioning.
- Android group transfer/stability, quiet MDM and remote diagnostic sequence improvements accumulated since pre.12.

## Scope and upgrade
This release ships Android and Linux server archives. All four server binaries (server, AWG, transit, VLESS) must be upgraded together; stop old workers before starting replacements. Existing configuration and legacy direct/AWG exit remain unchanged until an administrator enables named-exit policies. See [server exits](server-exits.md).

Windows/Linux client source improvements are included in the source tag; this release does not publish new desktop binaries. Full kill switch/IPv6 isolation, new exit protocols, automatic exit failover and all-device OEM/Doze acceptance are not claimed.

## Validation and deployment

- Full Linux Go suite, vet, focused race suites and all 60 installer tests passed.
- Native Android AAR rebuilt for arm64/x86_64; APK, instrumentation APK, lint and JVM tests passed.
- Note9 upgraded without clearing data: **98/98 regression tests passed**. A separate real AWG echo/transit test against the upgraded server also passed.
- APK signing certificate matches pre.12. Public update metadata and independently downloaded APK hash match this release.
- Existing production server upgraded with all four binaries; services active with zero restarts and VLESS applied revision confirmed.

APK SHA-256: `0844fcc490289387e4ec836f3f79c808b9538230c701d070a9e1413e77e76bdf`.

Private rollback backup remains on the server at `/opt/quic-lab/rollback/pre13-20261105`. Do not blindly restore old identity/MDM state after the upgraded server has accepted changes: revocations and acknowledged writes must be preserved. Detailed evidence and limits: [release acceptance](testing/preview-release-2026-11-05.md).
