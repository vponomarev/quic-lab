# Standalone MDM server

Available since 0.9.1. The same server archive can host management without a VPN server. Clients may use third-party VPN profiles or keep VPN off.

Fresh Debian/Ubuntu system with systemd and a DNS name:

```sh
sudo ./install-server.py mdm.example.org --management-only
```

The installer uses direct HTTPS and ACME, preserving the usual manual certificate options (`--cert` and `--key`). Open TCP 443 and TCP 80 for ACME. No UDP, Echo, VPN gateway, AmneziaWG, VLESS, transit or capture workers are started. Re-running the installer preserves the saved mode. Combining transport flags with this mode is rejected. Conversion of an existing combined installation is deliberately manual, with transport units stopped/disabled before configuration changes.

Both `/etc/quic-lab/server.json` and `admin.json` must contain `"management_only": true`. Server gateway/web/demo/fallback listeners and SNI routes must be empty. Admin transport/capture settings must be absent. The normal `listen` field remains for configuration compatibility but is not bound in this mode. Existing combined deployments default to false and remain unchanged.

Entry: `/lab/mdm`. Separate invitation QR/link, explicit client consent, current/external configuration, commands and telemetry work independently of VPN provisioning. MDM device API still requires TLS 1.2 or newer. VPN provisioning/capture endpoints return 404. Management devices can remain unassigned to VPN users.

Encrypted GUI/CLI backup and offline restore remain available; see [server-backup.md](server-backup.md). Keep copies off the host. Restoring an archive requires the matching server version.
