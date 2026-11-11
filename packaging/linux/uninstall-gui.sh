#!/bin/bash
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run with sudo' >&2; exit 1; }
systemctl disable --now quic-lab-control.service 2>/dev/null || true
# Explicit owned paths only. Never remove the VPN unit, binary or profiles.
rm -f -- /etc/systemd/system/quic-lab-control.service /etc/quic-lab/gui-control.json /usr/local/bin/quic-lab-gui /usr/local/share/applications/quic-lab.desktop
for name in control.py client.py model.py app.py; do
 rm -f -- "/usr/local/lib/quic-lab-gui/$name"
done
rmdir /usr/local/lib/quic-lab-gui 2>/dev/null || true
systemctl daemon-reload
echo 'GUI removed. VPN service and configuration preserved.'
