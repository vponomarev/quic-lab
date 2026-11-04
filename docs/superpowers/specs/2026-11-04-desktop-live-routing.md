# Desktop live application routing

Continue the user's autonomous desktop roadmap from d414279. New connections use
atomically published application rules; existing TCP/UDP connections retain their
exit until they close. Enabling/disabling per-app must not recreate TUN. Invalid
rules and failed Windows persistence leave the active policy unchanged.

Separate the desktop router/TUN/direct lifetime from VPN transport generations.
On detected VPN loss on the same physical network, retire only VPN flows, reject
new VPN/DNS flows while unavailable, and reconnect with bounded single attempts.
Never redirect failed VPN flows to direct. Keep counters cumulative across VPN
generations. Explicit Stop and physical-uplink changes still tear down both exits.

Approach chosen by implementer under the user's autonomous-work instruction:
extend the existing shared DesktopAppRouter with atomic policy replacement and
serialized VPN replacement. Rebuilding the whole engine would break direct;
adding a second TUN/router would duplicate ownership and route lifetimes.

Windows: service settings-only app changes save and publish without restart;
route/credential/uplink changes retain the existing explicit reconnect flow.
Coordinate updates with Automatic's network reconciliation; retain latest policy
for future engines. Show hot-apply capability to WPF; older services keep the old
confirmation. Same-uplink recovery is available through the managed supervisor.

Linux: SIGHUP reopens the private config and accepts only app-policy changes;
invalid/non-policy edits keep the old session. Per-app sessions reconnect after
observed loss while retaining TUN/direct; attempts are serialized and bounded,
backoff is capped. Legacy startup with no per-app keeps its old failure behavior.
AWG/VLESS still report engine liveness, not verified remote readiness. Physical
network replacement and full IPv4/DNS/GUI remain separate roadmap tasks.

Verification: RED/GREEN unit tests for pinned flow selection, VPN retirement,
counters, Stop/update races and settings save failure; Linux namespace acceptance
holds actual direct TCP/UDP across policy reload and VPN server loss/recovery;
source-address probes check new-flow decisions. Full Go suite, focused race/vet,
Windows cross-build/native tests and WPF tests. No host route/profile activation.
