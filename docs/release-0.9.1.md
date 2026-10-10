# QUIC Lab 0.9.1

Stable MDM iteration: manage an Android device from the Web UI independently of its VPN connection.

- Explicit QR/link enrollment, device permissions, pause and removal.
- Edit device VPN configuration and selected applications, apply revisions, start/stop VPN and see acknowledged state.
- Wi-Fi/cell telemetry and optional coordinates with separate consent; queue telemetry when LTE uploads are prohibited.
- Standalone MDM installation: `install-server.py DOMAIN --management-only` (no VPN/Echo services).
- Encrypted configuration/full backups through Web UI or CLI; verify, offline restore and rollback. CLI can be used by an external scheduled upload job.
- Includes fixes for MDM configuration/inventory UI stalls from prereleases.
- Every launch/old shortcut opens VPN. Standalone Echo remains an internal Diagnostics screen, separate from the launcher.

APK versionCode: 51. Install over the current application to preserve profiles, device identity and MDM binding. APK uses the existing distribution signing key.

Linux server archives include the server, AWG/VLESS/transit workers, installers and documentation for amd64 and arm64. Source is attached by GitHub for the release tag.

Validation: Linux full Go suite, targeted race/vet, 60 installer tests, release-archive backup/restore and Android12 MDM instrumentation. Android14/15 live validation is deferred; the broader VPN phase2 matrix remains outside this release. See `docs/mdm-stable-091-acceptance.md` for exact evidence and limitations.

Before upgrading, keep an encrypted backup outside the VPS. Offline restore requires the server build version recorded in the archive; preserve the previous server package. A server update briefly interrupts active connections.
