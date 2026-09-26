#!/bin/sh
# Run on the Linux server; destination must match admin.json apk_path.
set -eu
if [ "$#" -ne 2 ]; then
  echo "Usage: $0 SOURCE.apk /absolute/path/to/quic-lab.apk" >&2
  exit 2
fi
source_apk=$1
destination_apk=$2
case "$destination_apk" in /*.apk) ;; *) echo "Destination must be an absolute .apk path" >&2; exit 2 ;; esac
[ -f "$source_apk" ] && [ -s "$source_apk" ] || { echo "Source APK is missing or empty" >&2; exit 1; }
# Fail before replacing a working APK if the upload is incomplete.
unzip -tqq "$source_apk"
unzip -Z1 "$source_apk" | grep -qx 'AndroidManifest.xml'
directory=$(dirname "$destination_apk")
install -d -m 755 "$directory"
pending_apk=$(mktemp "$directory/.quic-lab-apk.XXXXXX")
trap 'rm -f "$pending_apk"' EXIT HUP INT TERM
cp "$source_apk" "$pending_apk"
chmod 644 "$pending_apk"
mv -f "$pending_apk" "$destination_apk"
sha256sum "$destination_apk"
echo "APK published; no server restart required."
