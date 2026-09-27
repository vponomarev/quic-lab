# QUIC Lab v0.7.4

Android 11+ client, Linux VPN/Echo server and Windows/macOS Wireshark capture helpers.

## Changes since v0.7.0

- Android QUIC/HTTPS VPN reconnects after a closed or unresponsive transport without recreating the Android VPN interface. Tested on a phone with a live server restart. Existing TCP sessions cannot survive loss of server-side connection state.
- Stale transit RTT is shown separately from the age of the last reply. VPN notification shows network, transport and traffic; public AWG Echo avoids Android IMS-only networks.
- AWG channel health checks and expiry prevent outgoing traffic from keeping an offline peer marked online.
- External transport capture with server-side TLS secrets, plus internal per-user capture. Capture streams over the existing HTTPS port. Browser controls, copyable CLI commands, Windows CMD/PowerShell and macOS support; Lua dissector included.
- Main server configuration is now `/etc/quic-lab/server.json`. Explicit CLI flags remain supported. TLS SNI passthrough accepts local or remote IP/DNS backends; certificate reload via SIGHUP remains available.
- Installer preserves configuration, backs it up on updates, migrates legacy ACL settings and rolls back failed service updates. Restricted ACL survives an unsuccessful ACME attempt and retry.

## Downloads

- `quic-lab-0.7.4.apk`: Android ARM64/x86_64, version code 18.
- `quic-lab-server-0.7.4-linux-{amd64,arm64}.tar.gz`: Linux server, AWG/transit workers, installers, example config and documentation.
- `quic-lab-capture-windows.zip`: Windows amd64 CLI/launcher.
- `quic-lab-capture-darwin.zip`: macOS Intel/Apple Silicon CLI/launcher.
- `SHA256SUMS`: checksums for all five packages.

The APK is a signed debug build for manual sideloading, using the existing signing identity; it is not a Play Store release. No deployed server configuration, client credentials or signing keys are included.

## Upgrade notes

Back up `/etc/quic-lab` and `/var/lib/quic-lab` before a server upgrade. Legacy custom systemd drop-ins containing `ExecStart` must be migrated to server.json; the installer refuses to silently override them. It does not move an existing nginx listener automatically. See [server configuration](server-config.md) and [installation](install-server.md).

The Android APK updates compatible existing installations without clearing profiles. Capture archives are optional and require a separately installed Wireshark. Internal capture removes the VPN encapsulation; application-level HTTPS remains encrypted. External TLS decryption is available only where QUIC Lab terminates TLS; AWG is not decrypted with TLS secrets.

IPv4 only. Multiple VPN still uses one DNS profile; separate-server combinations and long Android Doze sessions need broader coverage. HTTPS UDP retains TCP head-of-line blocking. See [development plans](vpn-plan.md).

## Validation

Go tests pass. Android APK/AAR build and lint pass. Installer fresh-install, upgrade and rollback checks passed in Debian 12/13 and Ubuntu 22.04/24.04 containers (systemctl simulated, generated units validated separately). The deployed server was migrated with backup; HTTPS, SNI passthrough and certificate reload without a process restart were verified. Phone VPN recovery after a live server restart was verified before the release metadata update. macOS helpers are cross-built; they were not interactively tested on a Mac for this release.
