# Implementation plan

- [x] Identity/schema: bounded vless_uris, strict validation, encrypted Android compile/snapshot and group import/refresh.
- [x] Runtime: atomic validated engine switch, deterministic candidate/cooldown policy, serialized event-driven rotation, physical DNS/budget preservation.
- [x] MDM: bounded credential-free group events and web display; capability.
- [x] Tests: Go regression/race, Android instrumentation, Note9 blocked→working candidate and refresh; independent review.
- [ ] Release: publish APK/server, add second Note12 MDM profile without changing primary, document actual deployment and remaining limits.

Deployment blocked by production outage acknowledged by user. Candidate 0.9.5-pre.3 (68) and verification saved in docs/vless-group-verification-2026-10-16.md.
