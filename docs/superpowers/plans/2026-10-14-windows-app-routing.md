# Windows application routing implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Selected exe TCP/UDP uses VPN, other applications remain direct; selected traffic blocks on VPN failure.
**Architecture:** Exact WFP AppID selection and a platform-neutral immutable policy precede kernel ALE redirection/authorization. No global default-route capture. The relay preserves original destination and redirect provenance, and uses the existing QUIC/HTTPS gateway.
**Tech Stack:** Go, WFP/ALE, existing Windows SCM/WPF, a verified callout driver (deployment choice outstanding).
**Spec:** docs/superpowers/specs/2026-10-03-windows-client-design.md, priority amendment 2026-10-14.

## Global constraints
- First live mode only_selected; retain independent only/exclusion lists. all_except_selected stays unavailable until tested.
- Exact executable identities, no PID/name matching or inherited child-process rules.
- No Secure Boot/signature/test-signing change on owner PC; driver selection is an explicit dependency, not a silent install.
- Preserve unrelated VPNs, management channel, defaults and DNS. Unknown attribution cannot silently leak selected traffic.
- No claims of real per-app routing from model/unit tests.

## Review focus
- PID reuse / same basename in another directory: identity comes from WFP, not process metadata guesses.
- Start failure / tunnel loss / service crash: selected traffic never silently becomes direct.
- IPv6/raw protocols/unknown identity: explicit blocking while policy active; no accidental bypass.
- Conflicting protected management identity: reject policy, never override protection silently.
- Mutable imported lists: compiled policy owns its data; inactive mode list is preserved.

## A1 — policy and native identity foundation
Files: internal/approuting/policy.go, policy_test.go, identity_windows.go, identity_windows_test.go, processes_windows.go; cmd/windows-app-policy/main_windows.go.
Interfaces: Policy{Version,Mode,Only,Except}; Compile(Policy,protectedIDs) (*Compiled,error); Decide(appID,protocol,ipv6,State) Decision; ResolveExecutable(path) (Identity,error).
- [x] RED: only/exclusion selection, independent lists, failed tunnel blocks, explicit Stop releases, protected conflict, unknown identity, IPv6/raw, mutation ownership, strict JSON bounds.
- [x] Implement immutable policy, fixed error strings, exact canonical opaque IDs. Expose all_except semantics in pure model only.
- [x] RED/GREEN Windows read-only executable resolver using FwpmGetAppIdFromFileName0 and canonical handle path; SHA256 identity for model, return raw AppID for future kernel filter installation. CLI inspects explicit exe paths and lists accessible running executable identities; PID is display-only.
- [x] Linux race/vet; Windows native tests against current test executable. No filters/drivers installed. Commit foundation.

## A2 — choose and validate deployable interception backend
- [ ] Evaluate available signed implementation, license and TCP/UDP/IPv6 semantics; otherwise custom driver needs isolated Windows driver VM and signing path. Present concrete choice before modifying host driver security.
- [ ] Require authoritative ALE identity before first outbound packet, redirected-flow provenance, loop prevention and selected-only persistent failure blocking. Shared DNS behavior explicitly documented.
- [ ] Define ABI and relay tests against actual chosen backend, then implement. No fake driver capability advertised.

## A3 — relay, UI and live acceptance
- [ ] Wire selected exe rules to QUIC/HTTPS relay; no global routes. Confirm only verified redirected sockets reach relay.
- [ ] Add manual exe picker / running-process picker, independent remembered lists, apply confirmation and unsupported-backend reason.
- [ ] Two controlled exe probes to same TCP/UDP destination: selected exits VPN, unselected direct; bad VPN endpoint, cancellation, crash, rebind, IPv6, management TCP continuity. Stop removes only owned rules.
- [ ] Repeat for all_except_selected with explicit management exclusions only after only_selected passes. Review, docs and commit; beta requirements remain separate.

Primary sources: https://learn.microsoft.com/en-us/windows-hardware/drivers/network/using-bind-or-connect-redirection and https://reqrypt.org/windivert-doc.html . WinDivert NETWORK lacks process identity; socket/flow event visibility alone does not prove reliable per-app first-packet routing.

## A1 verification — 2026-10-14

Completed foundation only; no Windows filters, driver, routes or service settings changed.
Linux `go test -race ./internal/approuting` passed. Windows amd64 `go vet` and CLI build passed; all seven native tests passed, including native AppID resolution and current-process inventory.
Review regressions reproduced and fixed: duplicate/case-aliased JSON fields are rejected; uninitialized compiled policy blocks active traffic decisions.

A2 remains open. An unmodified ProxiFyre backend is not yet suitable for the promised strict policy: its documentation describes substring path matching and direct fallback for unresolved ownership/fragmented IPv6. A separate Windows driver test environment or further signed-backend research is required before live implementation. No live per-application guarantees are claimed.

## Progress — selection draft, 2026-10-14

The independent A3 list editor was brought forward while A2 driver deployment remains unresolved. The Windows dashboard opens a separate application selection window: manual .exe picker, accessible running-process paths, independent include/exclude lists, local draft persistence. It explicitly states that rules are not applied to the network. The existing service is not sent these paths and no network configuration is changed.

Paths in this UI draft are display/configuration values, not trusted WFP identities. Backend integration must resolve them again with the service-side resolver, reject protected identities, and enforce capability checks before permitting Apply. Running process IDs are not policy identifiers. Child processes are not implicitly selected. The draft is stored in the current user's LocalApplicationData/QUIC Lab/application-selection-draft.json; corrupt drafts cannot be silently overwritten from the editor.

Verification: .NET independent-list/strict-import/persistence tests; native WPF add/mode-switch/save/reopen checks; existing dashboard state, statistics, polling, close/reopen and cancellation regression. No live traffic-routing claim follows from these checks.

See [interception backend assessment](../specs/2026-10-14-windows-interception-assessment.md) for A2 findings and the remaining deployment decision.
