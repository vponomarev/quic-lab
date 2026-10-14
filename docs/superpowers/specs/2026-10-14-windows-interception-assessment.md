# Windows per-application interception assessment

Date: 2026-10-14. Status: deployment decision open; no driver installed.

## Required behavior

The application identity must be authoritative before the first selected TCP/UDP traffic leaves. Selected traffic must not revert to direct routing when the VPN relay or controlling service dies. Unselected and protected management applications remain direct. IPv6 and unsupported traffic need explicit handling, not accidental bypass. Installation must preserve the owner's driver-signing and Secure Boot settings.

## Candidates reviewed

| Candidate | Evidence | Remaining issue |
| --- | --- | --- |
| Custom WFP ALE callout plus user-mode relay | Microsoft documents TCP/UDP redirection and redirect context/records. Existing A1 code resolves WFP AppIDs. | Requires driver build/test environment, restricted device ACL and ABI, provenance validation, loop prevention, failure guards and a signing/distribution path. None is implemented yet. |
| WinDivert / ProxyBridge | ProxyBridge uses WinDivert. WinDivert separates packet-level interception from socket/flow process metadata. | A process-selection UI does not establish exact attribution or first-packet/failure guarantees. Must audit and test the actual implementation; not selected as a verified backend. |
| ProxiFyre | Its configuration documentation describes path substring matching and direct fallback cases. | Cannot adopt the unmodified implementation as proof of the strict policy required here. |
| NetFilter SDK WFP | Supports TCP/UDP filtering; documentation explicitly says unexpected closure of the attached process returns the driver to bypass-all. | Requires independent persistent protection and appropriate SDK terms before use. It is not an out-of-the-box solution to the service-crash requirement. No purchase or installation performed. |

## Decision and next steps

Keep the application-selection draft separate from active policy. Do not expose an Apply button until the interception backend and crash protection have been tested together. Prefer an isolated Windows VM/test computer for custom WFP development. If that is unavailable, continue auditing signed implementations rather than changing security settings on the owner's primary PC. This is an engineering assessment, not a claim that all signed-driver approaches are impossible.

The live acceptance matrix remains: two independent executable probes to the same TCP/UDP destination, different observed exit IPs; same-basename exe elsewhere; tunnel outage; abrupt service death; IPv6; cancellation; explicit stop; management connection continuity. A relay alone is insufficient. Shared system DNS attribution remains separate from application socket attribution.

## Primary sources

- [Microsoft: Using bind or connect redirection](https://learn.microsoft.com/en-us/windows-hardware/drivers/network/using-bind-or-connect-redirection).
- [WinDivert documentation](https://reqrypt.org/windivert-doc.html).
- [ProxyBridge Windows documentation](https://github.com/InterceptSuite/ProxyBridge/blob/master/Windows/README.md).
- [ProxiFyre configuration](https://github.com/wiresock/proxifyre/blob/main/docs/configuration.md).
- [NetFilter SDK: Using API](https://netfiltersdk.com/help/nfsdk_wfp/using_api.html), notably default bypass and process-close behavior.
