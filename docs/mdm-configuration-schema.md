# MDM configuration schema 1 — storage foundation

Status: internal development contract; authoritative Android readers are implemented,
but live VPN coordination and poll application remain pending.

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

The runtime adapter keeps native identity bundles and preference maps inside the
same encrypted atomic envelope. These private bundles preserve existing registration
and update metadata separately from the portable schema. ConfigurationPreferences
rejects edits made through a source opened before a management-generation change.
The adapter currently applies only while VPN is stopped; it is not wired to polling.

## Optional reserve policy (WEB management)

Schema 1 accepts an optional `reserve` object with Boolean `wifi_on`, `wifi_off`, `cell_on`, `cell_off`, `metered_wifi`. Omission preserves compatibility with older documents. Device reports exclude identity contents and carry a local `configGeneration`; WEB mutations require both observed generation and desired revision. A profile whose identity is omitted retains credentials of the same profile ID. Pending desired credentials are preserved server-side across sanitized editor round trips.
