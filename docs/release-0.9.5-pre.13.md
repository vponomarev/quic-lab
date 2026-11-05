# QUIC Lab Android and server 0.9.5-pre.13

Intermediate preview, 2026-11-05. Android version code 78.

## Changes
- Multiple named AWG server exits, administrator access grants and device selection from Android, Windows and Linux. Failure does not silently fall back to another exit.
- Administrator-approved editing of VPN configuration from the Android MDM editor, including profiles, keys and portable JSON/age imports. Edits are submitted to the server and applied through normal provisioning.
- Android group transfer/stability, quiet MDM and remote diagnostic sequence improvements accumulated since pre.12.

## Scope and upgrade
This release ships Android and Linux server archives. All four server binaries (server, AWG, transit, VLESS) must be upgraded together; stop old workers before starting replacements. Existing configuration and legacy direct/AWG exit remain unchanged until an administrator enables named-exit policies. See [server exits](server-exits.md).

Windows/Linux client source improvements are included in the source tag; this release does not publish new desktop binaries. Full kill switch/IPv6 isolation, new exit protocols, automatic exit failover and all-device OEM/Doze acceptance are not claimed.

Validation and deployment results will be recorded in the release acceptance report before publication.
