# Windows subnet preview 0.1.0-preview.1

This is a separate Windows preview, not a new Android/server release.

## Included

Windows x64 GUI, service, Wintun; QUIC and HTTPS; up to 16 canonical, nonoverlapping IPv4 CIDRs; connect/stop/cancel; payload TX/RX totals and current rates. Service remains running when the UI closes. VPN is initially off and does not auto-connect on boot. The service itself starts automatically so normal UI use needs no UAC.

## Explicit limits

- No kill switch. The owner explicitly accepted direct fallback on failure for this preview. A surviving unhealthy adapter may instead retain the route until Stop; automatic failover is not promised.
- Only listed IPv4 subnets are routed. IPv6 and destinations outside the list remain on existing routes. System DNS is not changed; DNS packets only use VPN if their resolver IP belongs to a selected subnet.
- No default route, special-use ranges, tunnel address or VPN endpoint inside selected prefixes. Equal/more-specific foreign routes present at startup reject connection. Changes made afterward are not continuously guarded.
- One provisioned profile and physical IPv4 interface. No app routing, AWG/VLESS, automatic sleep/network-change recovery, MDM or research diagnostics in this release.
- Profile provisioning is through a private JSON file and elevated installer; the GUI does not yet import production profiles. The generic fixture JSON editor is not a production profile editor.
- App/service executables are not Authenticode-signed. Bundled Wintun retains its WireGuard signature. .NET runtime is bundled.

## Private profile

Use engine.example.json as a format reference. It contains placeholders, no credentials. Obtain the client certificate/key and server connection details separately. `interface_index` is the physical IPv4 interface index from Get-NetIPConfiguration. `endpoint` is an IPv4:port; hostname and TLS names must correspond to the server. Certificate/key/CA fields follow the QUIC Lab gateway profile format. Never publish the filled profile.

Example selected targets: ["1.1.1.0/24", "198.51.100.0/24"]. Addresses in examples are not a promise of a useful remote private network.

## Installation

1. Extract the archive. In a normal terminal, run `whoami /user` and record your desktop user SID.
2. Open **Windows PowerShell x64 as Administrator**. Run:

   `./Install.ps1 -Profile C:/private/engine.json -OwnerSID S-1-5-21-...`

3. Launch `C:/Program Files/QUIC Lab Windows Preview/ui/QuicLab.Windows.exe` as your normal user. Click Connect. UAC is only needed for installation/profile changes/removal.
4. The service is `QuicLabWindowsPreview`. Its private state is `C:/ProgramData/QUIC Lab Windows Preview`; old development installations are separate.

To change transport/subnets: disconnect, stop the preview service as administrator, update the protected engine.json, run the installed `quic-windows-engine.exe -config <path> -validate`, then start the service. Validation changes no network state. Invalid profiles must be corrected before the service can start.

## Removal

Run `./Install.ps1 -Action Remove` as administrator. It verifies the installed service path, stops it and removes only that service registration. Binaries and private profile are intentionally retained. This first installer refuses to overwrite an existing installation; upgrading requires explicit cleanup/migration rather than silent replacement.

## Verification record

Automated unit/race, Windows vet and native IPC/store checks are recorded with the release. The separate-account installed-service test requires a dedicated account and is not automatically run by this package. Physical sleep/resume and Windows 11 testing remain deferred. Actual subnet and installation results are appended before publication.

### Verified on 2026-10-14

- Go race tests: windowsengine, windowsclient, vpnclient; Windows vet and native IPC/store tests passed.
- Real QUIC and HTTPS: subnet 1.1.1.0/30, HTTPS exit verification, UDP and TCP DNS passed. Foreign 1.1.1.2/32 route correctly rejected. Default routes and DNS unchanged; no owned adapters remained.
- Clean elevated installation passed, Preview service running with VPN initially off. Normal-user GUI connect, payload counters, close/reopen, Stop and cancellation passed against the installed service.
- WPF regression passed. No claim of kill-switch, sleep recovery, Windows 11 acceptance, or automatic installation upgrade support.

### Build

With Go 1.26.8: cross-build `./cmd/windows-vpn-engine` and `./cmd/windows-service` for GOOS=windows GOARCH=amd64, named quic-windows-engine.exe and quic-windows-service.exe. Publish desktop/windows/QuicLab.Windows with .NET SDK 10.0.402, Release, win-x64, self-contained into ui/. Include official signed Wintun 0.14.1 and its prebuilt-binary license, dependency licenses, Install.ps1 and the example profile. payload-sha256.json maps relative package filenames to SHA-256 hashes; never include a filled private profile.

## Post-preview ICMP correction

The original 0.1.0-preview.1 archive forwards TCP/UDP only. Subsequent source adds real IPv4 ICMP Echo for QUIC/HTTPS and requires the corresponding server update. See [ICMP verification and deployment](windows-icmp-echo.md). This does not add general ICMP/traceroute support.
