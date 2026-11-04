# Server exits implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** Administrator-controlled, client-selected AWG server exits, extensible
to other providers, across managed ingress protocols and client platforms.
**Architecture:** Public catalogue/policy in shared Go package, persisted user ACL
and device selection, authenticated control API and provider/ingress adapters.
**Tech Stack:** Existing Go AWG/Xray/TUN, Android Kotlin, Windows WPF, Linux CLI.
**Spec:** docs/superpowers/specs/2026-11-04-server-exits-design.md

## Global Constraints
- Base 4e2bd00, clean managed server-exits worktree; preserve phase1-vpn dirty work.
- Go tests only on authorized Linux source copy; no user keys/profiles transferred.
- New named providers AWG only; fail closed on denied/unavailable exits.
- One selection per registered device; server ACL remains authoritative.
- No production network/service changes, push or merge.

## Tasks
- [x] 1. Catalogue/policy: internal/serverexit/config.go and policy.go plus tests.
  Definition{ID,Name,Protocol,AWGConfig}; metadata excludes AWGConfig; <=32,
  unique safe IDs, AWG config validation. Policy{Allowed,Default}; Resolve validates
  explicit choice/default against current catalogue. RED then focused race GREEN.
- [x] 2. Persistence/control: internal/admin/server_exits.go and tests; user policy,
  device selection/revision, API handlers and admin management page. Authenticate
  before reading choices, expected revision before mutation, atomic durable save,
  no secret exposure. Verify restart, revoked ACL, siblings, persistence failure.
- [x] 3a. internal/serverexit/runtime.go: Provider interface for dial, resolver,
  ICMP/probe/close; immutable Runtime starts one userspace AWG engine per enabled
  definition. Separate netstacks isolate duplicate internal IPs. Startup failure
  closes already started providers; missing/disabled provider never uses direct.
  Test two fake providers and real AWG peers, cleanup, DNS, TCP/UDP, failed peer.
- [x] 3b. internal/gateway/egress.go: TLS identity captured in request context;
  Egress callback selects one immutable provider for each request. Every TCP,
  UDP/datagram, DNS, ICMP and exit-IP path uses it; gateway echo remains local.
  Existing server fields supply legacy behavior. cmd/server wires Store and Runtime.
  Test two identities, denied selection, bond paths and all request families.
- [x] 3c. internal/serverexit/broker_linux.go: bounded private Unix IPC owned by
  main server, peer identity validated by Store and protocol-specific credentials.
  Stream/packet requests lease current device policy and close on policy changes.
  VLESS serverSockets routes authenticated UUID through broker; demux-only keeps
  its existing inner gateway route. Test stale sessions, UUID spoofing and UDP.
- [x] 3d. internal/awgserver/exit_tun.go: intercept managed peers after AWG source
  authentication, send packets to broker-owned per-device forwarding stack; keep
  legacy peers on existing kernel TUN. Refresh peer map on acknowledged reload;
  replace stack on policy change, discard old kernel replies for managed peers.
  Test TCP/UDP/ICMP, peer source spoof, policy transition and broker outage. Keep
  old capture behavior for legacy peers; attach managed flow capture at broker.
- [x] 4. Android/Windows/Linux client selection: authenticated catalogue/selection,
  persisted metadata, explicit reconnect of changed selection; expose errors and
  empty/legacy server states; keep local per-app/profile exit IDs separate.
- [ ] 5. Private namespace two-exit acceptance, all ingress protocols, legacy suite,
  relevant race/vet, platform builds/UI checks, docs/migration/backup integration,
  one fresh final review and one RED/GREEN fix pass, scoped local commits/artifacts.

## Review Focus
- Forged device/exit identifiers or revoked user ACL cannot select an exit.
- Control acknowledgement cannot precede durable policy or permit stale traffic.
- Partial worker failure/restart never falls back or roams one peer across owners.
- Resolver/diagnostic/UDP paths cannot bypass selected exit.
- Old profiles, shared credentials and concurrent device selections remain explicit.
