#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then echo "Usage: sh install-macos.sh https://lab.example/lab/"; exit 2; fi
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
case "$(uname -m)" in arm64) arch=arm64;; x86_64) arch=amd64;; *) echo "Unsupported architecture"; exit 1;; esac
"$here/quic-lab-capture-darwin-$arch" -trust-server "$1"
app="$HOME/Applications/QUIC Lab Capture.app"
mkdir -p "$HOME/Applications"
if [ -e "$app" ]; then echo "Application already exists: $app. Move it aside before installing the update."; exit 1; fi
/usr/bin/osacompile -o "$app" "$here/launcher.applescript"
cp "$here/quic-lab-capture-darwin-$arch" "$app/Contents/Resources/quic-lab-capture"
chmod 755 "$app/Contents/Resources/quic-lab-capture"
/usr/libexec/PlistBuddy -c 'Delete :CFBundleIdentifier' "$app/Contents/Info.plist" >/dev/null 2>&1 || true
/usr/libexec/PlistBuddy -c 'Add :CFBundleIdentifier string ru.vpnc.quiclab.capture' "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleURLTypes array' "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleURLTypes:0 dict' "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleURLTypes:0:CFBundleURLName string QUIC Lab Capture' "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleURLTypes:0:CFBundleURLSchemes array' "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleURLTypes:0:CFBundleURLSchemes:0 string quic-lab' "$app/Contents/Info.plist"
/usr/bin/codesign --force --deep --sign - "$app"
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$app"
echo "Installed: $app. Install Wireshark separately, then open the capture page in your lab admin."
