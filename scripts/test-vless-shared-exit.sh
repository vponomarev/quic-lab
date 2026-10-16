#!/usr/bin/env bash
# Run real VLESS carriers and the authenticated gateway in a disposable network namespace.
# No host routes, firewall rules, production identities or external destinations are used.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ $(id -u) != 0 ]]; then
  echo "Run as root (network namespace only)." >&2
  exit 1
fi
scratch=$(mktemp -d /tmp/quic-relay-acceptance.XXXXXX)
trap 'rm -f -- "$scratch/mobile.test"; rmdir -- "$scratch"' EXIT
go test -c -o "$scratch/mobile.test" ./mobile
unshare --net sh -c 'ip link set lo up; exec "$1" -test.v -test.run "^TestBondVLESSSharedExitContinuity$" -test.count=1' sh "$scratch/mobile.test" | go tool test2json -t -p quiclab/mobile
