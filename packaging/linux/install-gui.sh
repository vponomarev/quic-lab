#!/bin/bash
# Build/download quic-lab first, then: sudo bash install-gui.sh LOGIN /path/to/quic-lab
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run installer with sudo' >&2; exit 1; }
[[ $# == 2 && $1 != root && $1 != -* ]] || { echo 'Usage: install-gui.sh LOGIN /path/to/quic-lab' >&2; exit 2; }
[[ -f $2 && -x $2 ]] || { echo 'Supply the compiled Go desktop executable' >&2; exit 2; }
exec "$2" install --user "$1"
