# MDM core progress

2026-10-08. Execution branch: codex/mdm-core, based on v0.9.0.
Scope: accepted plan docs/superpowers/plans/2026-10-04-mdm-core.md. No MDM production deployment.

Task 1 server foundation implemented in bc3669b:
- durable private snapshot, hashed invitation/device credentials, idempotent enrollment;
- independent authenticated device API with TLS >=1.2 and 1 MiB request bound;
- epoch invalidation on activation/resume, 5-minute VPN commands, bounded queue;
- optimistic configuration revisions, idempotent events and 90-day pruning;
- cancellable 25-second long polling.
Observed missing-contract RED, then Linux race PASS (three repeats), vet PASS. Failed-write rollback, corrupt store and restart covered.
The module is not wired into server bootstrap/admin yet; no Android MDM agent exists. Full document schema validation is Task 4; periodic pruning/bootstrap belongs to Task 6.
Next: Task 2 independent Android transport/service and shared-budget adapter, followed by voluntary client lifecycle (Task 3).
