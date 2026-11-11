#!/bin/bash
# Run on Linux with Go, C compiler, pkg-config and OpenGL/X11 development headers.
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
output=${1:-artifacts/quic-lab-linux-amd64}
mkdir -p -- "$(dirname -- "$output")"
CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o "$output" ./cmd/quic-lab
printf 'Built %s\n' "$output"
ldd "$output"
echo 'Build on the oldest supported glibc distribution; this is not a static binary.'
