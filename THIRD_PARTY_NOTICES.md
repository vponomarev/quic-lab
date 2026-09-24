# Third-party components

`third_party/quic-go` contains a modified copy of quic-go v0.63.0. Its MIT license,
source copyright notices and asset licenses are preserved in that directory.
Local changes are documented in `third_party/README.md` and `migration-fixes.patch`.

Go and Android dependency versions are pinned in go.mod/go.sum and Gradle files.
They retain their respective upstream licenses. The Gradle Wrapper is distributed
under the Apache License 2.0; see https://github.com/gradle/gradle/blob/master/LICENSE.

VPN uses tun2socks core (MIT), gVisor netstack (Apache-2.0), smux (MIT), and coder/websocket (ISC). Bundled license texts are in android/app/src/main/assets/licenses and are included in the APK.

QR generation uses skip2/go-qrcode (MIT). Android scanning uses JourneyApps ZXing Android Embedded and ZXing core (Apache-2.0). License texts are bundled in the APK.
