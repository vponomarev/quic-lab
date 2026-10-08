# MDM configuration schema 1 — storage foundation

Status: internal development contract; Android runtime adapter is pending.

A full JSON document contains schema=1, profiles (1–32), currentProfileId,
enabledProfileIds, multiple, globalApps, dns {mode, profileId}, budget {limitBytes},
and diagnostics {enabled, detailed}. Maximum encoded document size is 1 MiB.
Unknown fields, nulls, duplicate keys and invalid references are rejected.

Each profile has id (default or lowercase UUID), name, settings, and optional
identity or removeIdentity. Settings use the existing Android types: mode is an
integer 0–3, apps is a string array, pool size is 1–5. The executable allowlist
and bounds live in internal/mdm/config.go; the native Android entry point calls
the exact same validator.

identity accepts certificate/key (a matching TLS pair), awg_config (validated
AWG configuration), and vless_uri (one supported VLESS URI). Omission retains
credentials for the same profile ID; removeIdentity=true explicitly discards
them. These fields do not carry MDM credentials, permissions or executable code.
Existing registered VPN bundles have additional private update metadata: the
runtime adapter must preserve that separately before it can replace local state.

MdmConfigurationStore encrypts both personal and external snapshots with a
separate Android Keystore alias, in noBackupFilesDir. One AtomicFile transaction
publishes the complete state. Current writes personal; external overlays personal;
detach removes external and its ownership/revision, leaving personal unchanged.
A failed save does not advance the stored revision. Same-binding stale revisions
are ignored; another binding cannot overwrite the store until detach.

The backing encrypted envelope is bounded at 2 MiB. If the combined personal and
external state exceeds this bound (including overhead), applying fails without
publishing a partial snapshot. This is separate from the 1 MiB wire limit.

Authorization, active VPN transitions, pending cleanup recovery and applied-revision
acknowledgement belong to the upcoming runtime adapter. The store itself does not
grant rights, start/stop VPN, or expose remote settings to existing readers.
