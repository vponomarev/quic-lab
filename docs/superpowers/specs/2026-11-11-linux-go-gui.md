# Linux single executable Go client

User approved replacing Python with Go/Fyne after discussing distribution
limitations. Preserve stage-1 controls, profile switching confirmation, autostart,
status, traffic, sanitized events, Android-style transport RTT freshness and
polling without flicker. No profile editor or new routing behavior.

One installed executable `/usr/local/bin/quic-lab`: default/UI mode runs Fyne
without root; `engine` runs the existing Go engine; `control` runs the privileged
local controller; `install --user LOGIN` installs/migrates an existing configured
Linux client. Legacy CLI flags and symlinks `quic-lab-client`, `quic-lab-gui`
preserve the existing service/helper/desktop integration. No Python runtime.

Keep `/etc/quic-lab` profiles, chosen profile and VPN autostart intact. Installer
must back up touched installation files, reject unsafe/unmanaged locations and
support rollback on failed migration. No removal of system Python/GTK packages.
Runtime may depend on system graphics libraries. Prove headless `control` and
`engine -validate` do not initialize a display. Verify the final executable on
Ubuntu laptop, not only the Debian builder (glibc compatibility matters).

Controller retains strict command schemas, fixed unit/profile whitelist,
SO_PEERCRED authorization and root peer verification, bounded IO/concurrency,
private regular profile files, managed ExecStart validation, switch rollback,
current InvocationID journal parsing and monotonic RTT freshness. No raw errors,
keys, endpoint addresses or arbitrary commands exposed over the socket.

GUI/control contract: package internal/desktopcontrol exports Command{Op string,
Profile string, Enabled *bool}; Profile{ID,Label string}; Status{Selected string,
Profiles []Profile, Active string, Autostart bool, Invocation string, Failed bool,
Started bool, SessionState string, Readiness string, Events []string, TXBytes,
RXBytes uint64, TransportState string, RTTMS *float64, ProbeAt *float64,
ProbeIntervalMS int, Probe string}, with snake_case JSON matching Python API.
Call(context.Context, Command) (Status,error), Monotonic() float64 and
ErrorText(error) string are public. New(root string) (*Controller,error),
Serve(context.Context,path string,*Controller) error, DefaultSocket constant.
GUI exports Run() error and uses Fyne v2, background serialized requests and
Fyne UI-thread dispatch. GUI never stops VPN on exit.

Acceptance: focused Go race/controller/socket/installer/UI tests; real systemd
fixture where useful; all four laptop profiles, RTT updates, no polling flicker,
closing GUI preserves VPN; final QUIC active and boot-enabled. Existing Windows,
Android and standalone Linux CLI builds must remain isolated from GUI imports.

Acceptance refinement after inspecting the installed laptop: the existing
DNS/IPv6 helper also uses Python. Port its behavior into Go, preserve fixed
paths and sysctl/DNS semantics, migrate only the recognized legacy helper to a
same-binary symlink with backup/rollback. Unknown helpers must not be replaced.
This removes the remaining client runtime dependency on Python.
