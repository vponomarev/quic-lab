#!/bin/bash
# Run from this source checkout: sudo bash packaging/linux/install-gui.sh USER
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run installer with sudo' >&2; exit 1; }
[[ $# == 1 && $1 != root && $1 != -* ]] || { echo 'Usage: install-gui.sh LOGIN' >&2; exit 2; }
uid=$(id -u -- "$1")
[[ $uid != 0 ]] || exit 2
source_dir=$(cd -- "$(dirname -- "$0")/../.." && pwd)
[[ -x /usr/local/bin/quic-lab-client && -d /etc/quic-lab/profiles && -L /etc/quic-lab/client.json ]] || {
 echo 'Install and configure the Linux VPN service and profiles first.' >&2; exit 1;
}
/usr/bin/python3 -c 'import gi; gi.require_version("Gtk", "3.0"); from gi.repository import Gtk' || {
 echo 'Install dependencies: sudo apt install python3-gi gir1.2-gtk-3.0' >&2; exit 1;
}
systemctl cat quic-lab-client.service >/dev/null
/usr/bin/python3 - <<'PY'
import subprocess
value=subprocess.check_output(['/usr/bin/systemctl','show','quic-lab-client.service','--property=ExecStart','--value'],text=True)
expected='{ path=/usr/local/bin/quic-lab-client ; argv[]=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json ; ignore_errors=no ;'
assert value.startswith(expected) and value.count('argv[]=')==1, 'Unsupported VPN ExecStart/config path; installer will not change it'
PY
for name in control.py client.py model.py app.py; do
 [[ -f "$source_dir/desktop/linux/$name" ]] || exit 1
done
# Refuse a pre-existing symlink/writable installation directory before root copies.
/usr/bin/python3 - <<'PY'
from pathlib import Path
import os,stat
for name in ['/etc/quic-lab','/etc/quic-lab/profiles','/usr/local/lib','/usr/local/bin','/usr/local/share/applications']:
 p=Path(name)
 if not p.exists(): continue
 s=p.lstat()
 assert stat.S_ISDIR(s.st_mode) and s.st_uid==0 and not s.st_mode&0o022, 'Unsafe directory: '+name
p=Path('/usr/local/lib/quic-lab-gui')
if p.exists() or p.is_symlink():
 s=p.lstat(); assert stat.S_ISDIR(s.st_mode) and s.st_uid==0 and not s.st_mode&0o022
for name in ['/etc/quic-lab/gui-control.json','/usr/local/bin/quic-lab-gui','/etc/systemd/system/quic-lab-control.service','/usr/local/share/applications/quic-lab.desktop']:
 assert not Path(name).is_symlink(), 'Refusing symlink: '+name
PY
systemctl stop quic-lab-control.service 2>/dev/null || true
install -d -m 755 /usr/local/lib/quic-lab-gui /usr/local/share/applications
for name in control.py client.py model.py app.py; do
 install -o root -g root -m 644 "$source_dir/desktop/linux/$name" "/usr/local/lib/quic-lab-gui/$name"
done
# Explicitly add only the root-owned package path; ignore PYTHONPATH/user packages.
cat > /usr/local/bin/quic-lab-gui <<'SH'
#!/bin/sh
exec /usr/bin/python3 -I -c 'import sys,runpy; sys.path.insert(0,"/usr/local/lib/quic-lab-gui"); runpy.run_path("/usr/local/lib/quic-lab-gui/app.py",run_name="__main__")'
SH
chmod 755 /usr/local/bin/quic-lab-gui
/usr/bin/python3 - "$uid" <<'PY'
import json,os,sys,tempfile
fd,path=tempfile.mkstemp(prefix='.gui-config-',dir='/etc/quic-lab')
try:
 with os.fdopen(fd,'w') as f: json.dump({'allowed_uid':int(sys.argv[1])},f)
 os.replace(path,'/etc/quic-lab/gui-control.json')
finally:
 if os.path.exists(path): os.unlink(path)
PY
install -o root -g root -m 644 "$source_dir/packaging/linux/quic-lab.desktop" /usr/local/share/applications/quic-lab.desktop
install -o root -g root -m 644 "$source_dir/packaging/linux/quic-lab-control.service" /etc/systemd/system/quic-lab-control.service
systemctl daemon-reload
systemctl enable --now quic-lab-control.service
for attempt in $(seq 1 50); do
 [[ -S /run/quic-lab-control/control.sock ]] && break
 sleep .1
done
[[ -S /run/quic-lab-control/control.sock ]] || { echo 'Control socket did not become ready; inspect quic-lab-control.service' >&2; exit 1; }
echo 'Installed QUIC Lab GUI. Open QUIC Lab in the application menu, without sudo.'
echo 'VPN profile, service, routes and autostart were not changed.'
