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

## Leaflet 1.9.4

`internal/admin/vendor/leaflet/leaflet.js` and `leaflet.css` are the official Leaflet 1.9.4 distribution, vendored from https://unpkg.com/leaflet@1.9.4/dist/ . BSD-2-Clause license is included in that directory. Upstream: https://leafletjs.com/ and https://github.com/Leaflet/Leaflet/tree/v1.9.4 .

SHA-256: JS `db49d009c841f5ca34a888c96511ae936fd9f5533e90d8b2c4d57596f4e5641a`; CSS `a7837102824184820dfa198d1ebcd109ff6d0ff9a2672a074b9a1b4d147d04c6`. Matches upstream published SRI. Only circle/vector markers are used; default marker/layer-control images are not bundled.

Map tiles are requested interactively from OpenStreetMap with visible attribution and origin referrer; no prefetch or bulk export. Policy: https://operations.osmfoundation.org/policies/tiles/ . Cell estimates use Mylnikov `data=open` API: https://www.mylnikov.org/archives/1056 . Provider source attribution/terms cover OpenCellID (CC-BY-SA 3.0), openBmap (ODbL), Mozilla Location Service (CC0); these data are not part of this source distribution. Exact cell matches are not verified and estimates may be area centroids.

## SQLite cell location database

Server-only Go driver: modernc.org/sqlite v1.39.1, BSD-3-Clause; embedded SQLite is public domain. Upstream documentation and license: https://pkg.go.dev/modernc.org/sqlite@v1.39.1 and https://gitlab.com/cznic/sqlite/-/blob/v1.39.1/LICENSE . Go module checksums are recorded in go.sum. The dependency is not imported by the mobile package.

License texts for SQLite, modernc libc/mathutil/memory, go-humanize, bigfft and
Leaflet are bundled in `licenses/server` and included in server release archives.

## Linux desktop GUI

Linux desktop uses Fyne v2.8.2 (Apache-2.0), with pinned Go dependencies in
`go.mod`/`go.sum`. Its embedded fonts include Inter, EmojiOne Color and
DejaVu/Powerline; their original license notices are retained in the upstream
module. `scripts/build-linux-client-release.py` collects runtime-module license
texts recursively, including Fyne font notices, into `licenses/` in the client
archive and records module versions in `licenses/MODULES.json`. Go's license
and the local netstack adapter's license are included as well. System graphics
libraries are dynamically linked and supplied by the Linux distribution.
