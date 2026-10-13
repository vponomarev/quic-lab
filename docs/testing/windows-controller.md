# Windows client shared controller — 2026-10-14

`internal/vpnclient` is platform-neutral. Config wraps the existing vpnmodel configuration and revision. A backend owns platform resources; Controller serializes Start/Stop, cancels pending startup, keeps the running session independent of UI/request context, and retries cleanup before permitting another start. Snapshot contains only state/reason/revision. Backend errors are replaced by fixed safe reason codes.

NetworkPlan uses exact before/after values and owned resource identifiers. Journal intent must be durably saved before OS mutation; recovery handles crashes on either side of mutation and rollback. A foreign value produces a conflict and stays unchanged. JournalStore durability and ownership-aware OS compare/write are platform contracts, not yet implemented Windows APIs.

Verification on Linux Go1.26.8: failing tests observed before implementation; additional behavioral failures for same-revision/different-config and pre-cancelled Stop corrected; complete package race suite passed ten repetitions, vet passed, Windows amd64 test binary compiled. Fake backends and fake network stores only. No Wintun/SCM/routes/DNS/GUI runtime claim.
