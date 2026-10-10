# Third-party components

`third_party/quic-go` contains a modified copy of quic-go v0.63.0. Its MIT license,
source copyright notices and asset licenses are preserved in that directory.
Local changes are documented in `third_party/README.md` and `migration-fixes.patch`.

Go and Android dependency versions are pinned in go.mod/go.sum and Gradle files.
They retain their respective upstream licenses. The Gradle Wrapper is distributed
under the Apache License 2.0; see https://github.com/gradle/gradle/blob/master/LICENSE.

VPN uses tun2socks core (MIT), gVisor netstack (Apache-2.0), smux (MIT), and coder/websocket (ISC). Bundled license texts are in android/app/src/main/assets/licenses and are included in the APK.

QR generation uses skip2/go-qrcode (MIT). Android scanning uses JourneyApps ZXing Android Embedded and ZXing core (Apache-2.0). License texts are bundled in the APK.

AmneziaWG client and server use amnezia-vpn/amneziawg-go (MIT), pinned in go.mod. Its license is included in the APK and server archives. The local netstack adapter retains its MIT license in internal/awg/netstack/LICENSE.

VLESS uses unmodified Xray-core v26.3.27 (Go module v1.260327.0, MPL-2.0).
Corresponding source: https://github.com/XTLS/Xray-core/tree/v26.3.27 .
The application adapter lives in internal/vless; it does not modify upstream Xray files.
Licenses for Xray, REALITY, uTLS and new transitive runtime dependencies are bundled
in android/app/src/main/assets/licenses. The existing gVisor version is retained
with an explicit module replacement for tun2socks compatibility.

Server backup encryption uses filippo.io/age v1.3.2 (BSD-3-Clause) and its
filippo.io/hpke dependency (BSD-3-Clause). License texts are bundled in server archives.
