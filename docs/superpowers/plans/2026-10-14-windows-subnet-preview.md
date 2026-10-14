# Windows QUIC/HTTPS subnet preview release

User-approved scope 2026-10-14: first Windows release supports QUIC/HTTPS and IPv4 subnet selection. AWG/VLESS and automatic sleep recovery are deferred. Application routing is deferred and its draft editor is not shown in the release UI.

## Tasks
1. Extend bounded /32 settings to canonical, nonoverlapping IPv4 CIDRs. Reject default, special-use, tunnel-address and endpoint overlap. Verify all equal/more-specific foreign routes, not just a subnet's first address. Tests before implementation.
2. Show actual route scope in the dashboard, preserve independent system DNS/default routes; make limitations visible. Provide profile example and instructions.
3. Build a self-contained Windows x64 package, official signed Wintun with license, explicit elevated install/remove scripts with ownership/hash checks; private profile separate from package. Version/release label must say preview, not full beta or stable.
4. Run Go race/vet, native UI/runtime regression; live TCP/UDP QUIC and HTTPS to a small subnet, conflict and cleanup tests. Do not claim service-crash protection or sleep recovery without evidence. Resolve any release-blocking safety gaps explicitly.
5. Review, checksums, release notes, publish source/tag/assets to GitHub under Windows-specific prerelease tag. Do not include private configs or unrelated Android files.

The previous broad Windows beta plan remains backlog. This preview does not complete it. Scope notes and test omissions must accompany the artifact.

Owner clarification: optional kill switch deferred to the next phase. Direct fallback on tunnel/service failure is accepted for this preview. Display this accurately; do not promise automatic failover while an unhealthy adapter still holds a route.
