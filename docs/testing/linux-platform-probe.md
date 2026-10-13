# Shared backend Linux probe — 2026-10-14

This is an architecture test for the Windows client, not a Linux product release. It uses the same vpnclient Controller/Backend contract and mobile packet handler, with a Linux-specific ephemeral TUN adapter. It has no desktop UI, persistent service, MDM or package installer.

Run on a disposable Linux build host with Go, iproute2, Python, /dev/net/tun and root: `bash scripts/test-vpn-platform-linux.sh`. The script builds the probe and fixture, creates two uniquely named private network namespaces, and creates their veth pair inside the namespaces. All route/address changes occur inside them; the host management route is compared before/after. Fixture credentials are generated into a private temporary directory and removed. The probe refuses to run in PID1's network namespace.

Verified on 192.168.5.214: real TUN TCP and UDP echo over QUIC, then HTTPS; Start produces running Snapshot, SIGTERM/Stop produces stopped Snapshot and removes the TUN; namespaces and child processes cleaned; management route unchanged. Fake tests cover device-open failure redaction and pre-cancelled Start. W1 separately covers mid-Start cancellation, concurrent Start/Stop and retry after cleanup failure.

Not verified: Windows Wintun, SCM, pipe ACLs, DNS/IPv6 rules, Windows sleep/reboot, physical network changes, AWG/VLESS runtime on Windows. These results cannot substitute for Windows VM acceptance.
