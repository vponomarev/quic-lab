# Phone MDM import and live acceptance

User approved continuing with import and a live test-server round trip, and explicitly
authorized full VPN configuration including profiles/import. Existing autonomous
authorization applies; use the current clean codex/server-exits worktree (0732e0b).

Use the existing portable quic-lab-config JSON and age format. A native Android
document picker reads at most 2 MiB. Plaintext credentials are rejected; age is
decrypted locally with the entered password. No files/passwords are sent to server
or written to app storage. Existing native configtransfer validates/remaps profiles,
routing, groups and identities in add/update/replace modes. Mapping targets come
from the server draft, never local runtime. Administrative profileSelection is
preserved unchanged for server-side trimming; the file cannot grant MDM rights.

The phone WebView gains an import button. Native import returns only a candidate
draft and warning metadata; it does not call local apply or server submit. The user
can inspect/edit/cancel before explicit Send, using the existing CAS/permission
checks. Pause/detach and Activity destruction fence late import completions. Server
revocation during a picker remains authoritative at submit. Passwords/errors are
not logged; the password dialog explicitly enables the secure-window flag.

Scope: portable JSON/age bundles (all four sections), profiles/keys/group references;
no raw AWG-file or VLESS-URL importer, subscription fetching or new group authoring UI.
Those formats can already supply keys through the editor's explicit fields.

Live acceptance uses a disposable HTTPS fixture on authorized Linux host
192.168.5.214, synthetic enrollment/profile data, isolated Android preferences/files,
real MDM transport and server handlers. Exercise grant, import draft, save, retry,
sync/apply/report/ack, read redaction, revoke; do not change production MDM or VPN.
