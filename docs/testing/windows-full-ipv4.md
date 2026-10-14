# Windows full IPv4 and network recovery — 2026-10-14

## Use

Open **Настройки подключения → Маршрутизация → Весь IPv4-трафик**. DNS defaults to 1.1.1.1 and is sent through VPN. Under connection advanced settings enable **Выбирать сеть автоматически (Wi-Fi / LTE)** to follow physical network changes. Save reconnects only with confirmation. Selected subnet mode retains its independent list (up to 256) while full mode is selected.

New example profiles use full IPv4 + automatic uplink. Existing profiles missing `routing_mode` / `uplink_mode` remain selected-subnet / manual-interface. URL imports retain local routing and adapter settings. No silent upgrade of existing routing.

IPv6 routes, bindings and addresses are not configured or blocked. IPv6 remains outside this IPv4 VPN. No kill switch; interruption can expose IPv4 traffic on its normal route. Sessions may break during network changes. No per-app routing in this build.

## Routing and DNS

The disposable Wintun adapter owns generated IPv4 prefixes covering all IPv4 except special-use ranges, its own address, the resolved VPN server, and existing non-default routes on other active adapters. Existing LAN, VirtualBox, Hyper-V and ZeroTier routes are preserved. This intentionally respects explicit system routing exceptions rather than overriding them. Physical default gateways/routes are never rewritten. Transport sockets bind to the chosen physical interface.

Full mode uses a separately tagged NRPT catch-all rule pointing to the chosen IPv4 DNS resolver. Existing effective DNS policies cause connection refusal, not replacement. Only rules tagged to this adapter are deleted on Stop. Failed deletion retains ownership for retry. Abandoned rules carry process identity/start time and are recovered before service/CLI connection attempts; live owners and unrelated rules remain intact. DNS setup is independent of IPv6 routing. `-validate` remains read-only.

IPv4 route/address/interface notifications trigger a debounced rescan; 30-second polling is fallback. Physical default choice uses route+interface metric. Address, gateway, interface and route-exception changes require serial stop/cleanup/reconnect. Dead QUIC/HTTPS transports also reconnect on unchanged network. Explicit Stop cancels retries; failed cleanup blocks a new engine. UI distinguishes waiting for network, reconnecting and ready.

## Verification

- Go race tests: windowsengine, windowsclient, vpnclient.
- Native Windows tests: full-route planning, DNS ownership/retry, unchanged-network transport death, network loss/recovery, cleanup failure fence, Stop during startup, notification registration/cleanup, large PowerShell payload (70,000 characters).
- WPF: full/subnet and auto/manual save/reload, unchanged legacy editor, retained local routes on URL import, existing dashboard/editor regressions.
- Live QUIC full IPv4: expected external exit 168.119.242.240, DNS TCP+UDP and system resolver; three consecutive successful auto-uplink start/stop cycles. Abandoned DNS rule removed. DNS baseline restored, no extra adapter, IPv6 route snapshot unchanged; previous connection restored.
- Live testing found Stack.Wait blocked by TCP handshakes arriving during shutdown. Windows now closes/waits for packet input before shutting down the gateway stack. Shutdown-order regression RED→GREEN, then three native cycles passed.
- Live HTTPS full IPv4: expected exit, UDP/TCP DNS and system resolver passed. A deliberately failed subnet-mode startup removed the abandoned DNS rule before connecting. IPv6/DNS baselines restored.
- Installed service/UI hashes verified; stored encrypted profile unchanged, previous VPN restored, ping successful.
- Independent review fixes: dead transport recovery with unchanged uplink; stdin-based network script avoids Windows command-line limit; CLI recovers abandoned DNS even before an unsuccessful/subnet startup.

Physical Wi-Fi↔LTE switching and sleep/resume have not been exercised on a laptop modem. Fake uplink lifecycle tests cover recovery, but do not substitute for that hardware acceptance. AWG/VLESS retain their previous live subnet checks; this iteration's full-mode end-to-end tests cover QUIC and HTTPS.

This is a development build; the published preview archive is not replaced by these source changes.

## Windows beta publication

These capabilities are packaged in [Windows 0.1.0-beta.1](../release-windows-0.1.0-beta.1.md). Earlier development/preview statements above describe the historical verification stage.
