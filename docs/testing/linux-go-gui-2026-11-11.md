# Linux Go GUI acceptance — 2026-11-11/12

Python/GTK frontend and Python privileged controller replaced by one Go/Fyne
executable. Existing Linux engine extracted into internal/linuxcli without
network behavior changes; old CLI invocation remains supported by symlink.

## Build and automated checks

Authorized Debian 13 Linux host, Go toolchain 1.26.8, Fyne 2.8.2:

- Full `go test -tags ci ./...` passed (including mobile, server, Windows core).
- Focused `go test -race -tags ci ./cmd/quic-lab ./internal/desktopcontrol
  ./internal/desktopinstall ./internal/desktopui ./internal/linuxcli` passed.
- Focused `go vet -tags ci` passed; Linux client race tests and mobile
  ICMP/desktop/probe regressions passed.
- Standalone Linux CLI and Windows engine build with CGO_ENABLED=0. Their
  dependency graphs and the mobile package do not include Fyne.
- Real UID peer authorization, root peer authentication, private config paths,
  strict request schema, bounded requests, cancellation/rollback and socket
  ownership covered by controller tests.
- Installer tested for preservation of profiles/autostart/inactive VPN,
  rollback of touched files, unsafe directory rejection, and readiness failure.
- Widget/reducer tests cover non-flickering polls, queued actions, confirmation,
  RTT expiry, preserving actionable errors, and closing without disconnect.
- Task reviews and final integration review clean after correcting error
  persistence and requiring controller/engine readiness during installation.

Release executable: `quic-lab`, 39 MiB (approximately), SHA256
`3f5799c52a12d2fc5ca009faf780ecf21f203cadbee23c7a61f55d4802fcf3d6`.
Dynamically linked, requires glibc >=2.38 for this build and system OpenGL,
Wayland-client/X11 libraries. Ubuntu 24.04.5 laptop glibc2.39 resolves all of
these libraries. `--help`, `status`, installer and engine validation work
without DISPLAY/WAYLAND_DISPLAY. Python is not a client runtime dependency.

## Laptop migration

Thinkpad-T14 / Ubuntu24.04.5. Installed `/usr/local/bin/quic-lab`; client/gui
aliases point to the same executable. Existing root profiles, selected profile and VPN unit preserved. Controller and
the recognized DNS/IPv6 helper now execute the Go binary.
Original migration backup: `/var/backups/quic-lab-client/20261111T205947Z-go-gui-3209478128`.
Final helper migration backup: `/var/backups/quic-lab-client/20261111T211857Z-go-gui-2877998069`.

Post-migration controller and native GUI active, QUIC with fresh RTT and VPN
autostart enabled. LAN peer192.168.5.200 routes over Wi-Fi; 8.8.8.8 routes through
TUN; public exit observed168.119.242.240. Global server settings not changed.

## Reproducible live GUI check

Explicitly opt-in; it switches real connections and toggles VPN boot policy.
Close the normal window first. Build/run only for an authorized desktop:

```sh
go build -tags guiacceptance -o quic-gui-acceptance ./cmd/linux-gui-acceptance
GUI_SCREENSHOT="$HOME/QUIC-Lab/GUI-PREVIEW.png" ./quic-gui-acceptance
```

The harness uses real Fyne widgets, native confirmation dialogs and the real
controller. It samples three completed polls, exercises disconnect/connect/
reconnect, switches all four profiles, toggles autostart off/on, captures the
canvas and checks that closing keeps the same VPN InvocationID. It restores
QUIC/autostart on failure. This harness is excluded from production builds.

The final live results are recorded below.

Additional dependency audit found a Python DNS/IPv6 helper in the pre-existing
laptop service. Its behavior is moved into the same Go executable; migration
recognizes the exact legacy source SHA256 before replacing it with an alias.
Installer tests cover repeat updates, unknown-helper rejection and rollback.
The first live run passed stable polling and disconnect/connect/reconnect, then
exposed an acceptance-harness traversal error: Fyne wraps native popups in an
OverlayContainer. Corrected the test traversal; native-dialog regression went
RED to GREEN and UI race suite with guiacceptance tags passed. Production dialog
code did not change.

Final native GUI run after the Go network-helper migration: PASS.

- Three completed background polls kept controls enabled and RTT fresh.
- Disconnect/connect/reconnect changed the service as expected; stale RTT cleared.
- Native confirmation dialogs accepted all four switches. Observed RTT samples:
  QUIC47.5ms, HTTPS81.5ms, AWG126.8ms, VLESS368.9ms; these are observations,
  not comparative benchmarks (VLESS uses a full HTTPS probe).
- Autostart checkbox disabled and re-enabled the actual boot setting.
- Closing preserved the current VPN InvocationID; final QUIC active, boot enabled.
- Go network hook ExecStartPre returned0, DNS drop-in uses1.1.1.1 and IPv6
  disables match the prior setup. LAN direct,8.8.8.8 through TUN, public exit
 168.119.242.240 rechecked. GUI reopened from the installed binary.
- Screenshot: `/home/vitaly/QUIC-Lab/GUI-PREVIEW.png`; updated GUI.md alongside it.
- Full Go tests passed again with the network helper included; helper race/vet
  and the final scoped security/migration review are clean.
