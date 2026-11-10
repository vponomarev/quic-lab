# Linux full IPv4 capture

The user wants a compact full-laptop VPN configuration and explicitly suggested
using two /1 routes without touching the physical default. Work is autonomous.
Implement in the existing Linux client, preserving selected-subnet mode.
`targets: ["0.0.0.0/0"]` opts in; mixing /0 with other targets is invalid.

Approaches considered: splitting public space (rejected as unwieldy), policy
tables (unnecessary here), lower-metric VPN default (cannot outrank metric zero
without changing the original), two /1 routes (selected after user suggestion).
Preserve every existing default and every more-specific route. Reject existing
/1 capture routes before mutation to avoid interfering with another VPN.

Transport/direct sockets retain SO_BINDTODEVICE. Pin an endpoint /32 only when
not already protected by a more-specific main-table route; remove only our own
pin on orderly stop or startup failure. TUN and /1 disappear on process exit;
an abrupt kill can leave the physical endpoint pin until manual removal. This
known limitation is documented; no claim of abrupt-exit pin cleanup is made.
No automatic physical-network migration, IPv6 tunnel, DNS management or kill
switch is added. Keep the laptop's reversible DNS/IPv6 service helper.
Keep production server AWG transit via 88.99.87.121 unchanged.

Acceptance: Linux unit tests and isolated namespace payload tests for all four
transports, /1 preference/LAN/endpoint/bound direct socket behavior,
existing physical route preservation and cleanup including partial setup error.
Install and verify on the user's laptop with rollback backup; migrate full and
Firefox examples to /0, retain 8.8.8.8/32 examples; update usage documentation.
