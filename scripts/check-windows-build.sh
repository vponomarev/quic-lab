#!/bin/sh
# Compile only; run on Linux. Does not install or execute Windows code.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT HUP INT TERM
failed=0
for pkg in ./internal/gateway ./internal/awg ./internal/vless ./mobile ./cmd/gateway-client; do
  printf 'Checking %s\n' "$pkg"
  if GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o "$out/check.exe" "$pkg"; then
    printf 'PASS %s (compile only)\n' "$pkg"
  else
    failed=1
    printf 'FAIL %s\n' "$pkg"
  fi
done
exit "$failed"
