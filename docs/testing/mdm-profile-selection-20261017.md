# MDM profile selection — 0.9.5-pre.4

Server-issued permission: ConfigurationDocument.profileSelection (enabled, profileIds, serverRevision). Missing policy denies local managed selection. The allowed list refers to existing profiles; multiple exits and enabled selection cannot be combined.

Android exposes existing Select buttons for allowed profiles while keeping editing locked. VPN must first be stopped, as with existing profile selection. Selection updates only current in the encrypted current/external runtime. Reports include the selected profile. Unchanged server selection survives new configuration revisions; changing currentProfileId or serverRevision explicitly overrides it. Removing permission/selected profile returns to the server choice. Detach clears authorization metadata and binding ownership is checked before local selection.

The server can stage this policy for an old client; Sync withholds the entire new configuration until the device reports profile_selection_v1. Thus old clients do not receive unsupported documents. The web editor shows controls for supporting clients.

Verification: Linux race tests internal/mdm and internal/admin passed. Android debug APK/test APK and lint passed. Note9: 14 instrumentation tests passed, including encrypted reopen, repeated sync, current/external modes, explicit server choice and revocation. Independent review identified inherited permission across bindings; regression reproduced the failure and passed after clearing authorization metadata on detach and checking binding ownership.

Deployment: server and APK published as 0.9.5-pre.4 (69). Public capabilities and downloaded APK SHA256 verified: 26202a729d38ca09ccf988e596824617387aaad6019e951c6db622a0fd07d8b6. Rollback retained on server.

Note12: permission enabled for its two existing profiles in desired revision 7. At verification it still reported pre.3 / applied revision 6, so permission waits for update. VPN remained stopped; no start or active-profile command sent.
