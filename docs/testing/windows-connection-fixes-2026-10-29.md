# Windows connection fixes, 2026-10-29

Baseline: windows-v0.1.0-beta.2 (dda20e0). User confirmed AWG succeeds when selected from the imported bundle instead of the lower transport control.

## Reproductions and results

- New WPF regression failed with `lower AWG selector retained QUIC credentials/address`; fixed synchronization and full profile application.
- Saved credential-edit test failed with `switch reverted saved credential edit`; cached imported profile now tracks successful saves.
- Independent review found the same risk after `backend_start_failed` (save succeeded, reconnect failed). The new test reproduced it and passes after updating the cache for this persisted-save outcome.
- Discovery test observed two startup queries, then one after per-attempt snapshot reuse.
- Real PowerShell discovery script tested against deterministic CIM substitutes: bounded query count, metric selection and local subnet preservation.
- Route script tested against simulated OS mutations: no per-prefix CIM lookup, strict rejection of a more-specific foreign route using the complete table.
- DNS script tested with simulated OS rules: only stale owned rules removed; live/foreign rules preserved and effective foreign policy blocks setup. DNS cleanup retry and subnet-mode tests pass.
- Windowsclient/windowsengine tests and full WPF suite passed during development. Final clean-source checks and payload verification are part of release preparation.

## Read-only measurements

Go benchmark on the same AMD Ryzen 5 3600 Windows machine, three timed iterations each, no VPN startup or network mutation:

| Operation | beta.2 | updated |
| --- | ---: | ---: |
| DiscoverWindowsUplink | 2.0706 s/op | 1.7975 s/op |
| Discovery calls per automatic startup | 2 | 1 |

Separate read-only PowerShell measurements: 60 Find-NetRoute calls 1.482 s including shell/module startup; one Get-NetRoute snapshot 0.697 s. Existing table verification remains mandatory. Combined DNS setup eliminates one shell launch. Do not present these figures as a measured full connection time.

Initial sandbox WMI queries were denied and their timings were discarded. Measurements above ran in the normal user context. No elevated install, adapter mutation, end-to-end AWG timing or UAC acceptance was performed for this update.
