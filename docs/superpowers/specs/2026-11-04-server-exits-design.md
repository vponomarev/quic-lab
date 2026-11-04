# Server exits: administrator grants, device selects

User request: several server-side exits instead of a single global direct/AWG
uplink. Administrator permits exits; the client chooses one. First provider AWG;
other supported transports must be addable without changing identity/policy APIs.

Use server_exit_id terminology, distinct from existing client-local exit_id (a
profile/group identity). Exits have stable IDs, names, protocol and private provider
configuration. Upstream AWG keys remain on the server. Limit 32 exits initially.
User ACL defines allowed IDs and default; each authenticated device has its own
optional selection. No selection inherits the administrator default. No policy
preserves legacy behavior. Explicitly empty ACL denies exits; deleted, disallowed,
unavailable or malformed selection never silently falls back to direct.

Existing user autonomy authorization applies. Builder choices: one selection per
registered device, shared by its ingress transports; changing selection ends that
device's existing flows so an acknowledged change cannot leave old-exit traffic.
Administrator revocation takes precedence over cached client metadata. Persist
policy/selection atomically before acknowledging; validate again at traffic admission.

Approaches considered:
1. Wire-only request field on QUIC/HTTPS: small, but native AWG/VLESS cannot carry
   it and clients can spoof choices without server ACL. Rejected as sole authority.
2. Separate server installation/listener per exit: easy isolation but duplicates
   identities and cannot offer a single managed choice. Rejected.
3. Server catalogue + authenticated per-device selection + ingress adapters:
   selected. Reuses device identities/control plane, keeps authorization in server,
   separates provider lifecycle from client protocols. More integration/testing.

New control API lists public metadata and accepts an ID with revision protection;
use existing per-device bearer authentication and mTLS identity where available.
No keys/config paths in metadata/errors/logs. Client choice is optional; old clients
retain legacy/default behavior. Admin panel grants/default and displays selections;
Android/Windows/Linux expose the available choices and report selection failure.

All existing ingress types require an adapter: QUIC/HTTPS (including bond), native
AWG by authenticated peer/source address, VLESS by authenticated UUID. TCP, UDP,
DNS, exit-IP diagnostics and supported ICMP use the same selected provider. Local
gateway RTT/control endpoints remain local. Kernel forwarding must retain a
fail-closed guard while an AWG exit is down. Coincident provider private addresses
must be isolated or rejected explicitly rather than silently misrouted.

Provider interface carries capabilities rather than pretending every future
protocol forwards arbitrary IP. AWG is the only new configurable provider in v1;
legacy direct/global transit remain compatible, not implicitly allowed alternatives.
One upstream AWG peer has one owner; never start several independent instances of
the same key for separate ingress processes (peer roaming breaks correctness).

Verification: ACL/default/selection persistence, cross-device isolation, disabled/
expired identities, bad/stale revisions, missing provider/no fallback, redaction;
two simultaneous AWG exits with distinguishable TCP/UDP/DNS responses, failure of
one not affecting the other, all ingress adapters, client UI/control tests and
legacy regression. Source-only Go tests on authorized root@192.168.5.214, private
network namespaces, no host route/production deployment changes. Native Windows
test binaries may run; do not run go test on Windows. Fresh final review before
completion. Deployment/migration remains explicit and reversible.
