# Windows saved profile library

Continue autonomous roadmap from cd841e8. Service owns up to 32 named profile
snapshots in its existing atomic DPAPI settings document (total plaintext <=1 MiB).
Current settings remain the active configuration; legacy documents load with an
empty library. List responses contain only ID/name/transport/endpoint. Credentials
are never returned, including through read_settings. One shared revision fences
all edits. Save/delete do not stop or alter the active VPN; persistence failure
leaves memory/revision unchanged. Regular settings edits preserve the library.

Chosen approach: extend the existing encrypted document rather than independent
files (would need cross-file transactions) or UI storage (would expose credentials
and split ownership). User has requested autonomous work; these are implementer
choices. No extra package/dependency or installed-service changes.

Profile save accepts the current editor draft, optionally replacing a selected
snapshot; first configure/save one active connection before using the library.
Activation copies VPN transport/endpoint/credentials/trust/name from the snapshot,
but retains current local routing_mode, targets, DNS, uplink/interface and app
policy. Revalidate the merged configuration. Running activation requires explicit
reconnect; stopped activation does not start VPN. Failed reconnect retains the
saved selected settings as existing Apply does; other snapshots remain available
for manual rollback. No automatic rollback/failover or multiple simultaneous exits.
Deleting a snapshot does not delete its already active settings copy.

WPF capability-gated Profiles tab: list, save new draft, replace selected, activate,
delete; explicit dirty-draft/disconnect/delete confirmations. Older service keeps
existing editor usable. Library save retains editor draft and updates revision.

Checks: RED/GREEN lifecycle, revision/secret isolation, failure/size/ID limits,
merge local routing; native DPAPI library roundtrip; WPF save/reopen/activation
and old-service compatibility. Full Linux Go suite, targeted race/vet, cross-built
native Windows tests, full WPF/build, one fresh review and scoped local commit.
Physical routing/sleep/upgrade/release and Linux profile UI remain separate tasks.
