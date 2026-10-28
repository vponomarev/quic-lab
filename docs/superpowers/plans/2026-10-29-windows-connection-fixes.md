# Windows profile switching and connection startup

User requested fixing the confirmed wrong-credentials transport switch and slow AWG startup. Implement inline in the existing isolated Windows worktree; exclude unrelated admin work.

Design: synchronize both existing transport selectors and populate transport-specific endpoint/credentials atomically. Preserve local routing. Report a useful profile error instead of a certificate error for AWG/VLESS. Keep profile material in window memory only. Avoid a new transport implementation.

Startup: reuse the automatic controller's discovery snapshot for that start attempt only, batch uplink CIM reads, verify all owned prefixes against one route-table snapshot, and combine stale DNS recovery with DNS setup. Retain strict competing-route rejection, preferred-address readiness, foreign/live DNS preservation, cancellation and cleanup. No early connected indication or reduced handshake requirements.

- [x] Reproduce switching QUIC → AWG → HTTPS in WPF, then fix both selector paths and missing-profile feedback.
- [x] Pin discovery reuse and route/DNS behavior with regression tests; optimize startup and benchmark read-only operations.
- [ ] Run Windows service/engine and WPF checks; review changes, build from a clean commit, publish beta.3 Setup.exe and checksums.

Validation limits: sandbox WMI reads require the normal user context; no elevated adapter mutations or UAC installation can be claimed without an actual test. Record measured component costs separately from end-to-end connection latency.
