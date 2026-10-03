# Unified TCP and QUIC ingress

Fresh installations use the Go server on TCP/443 for HTTPS, the administrator UI,
and the mTLS VPN. Echo and authenticated QUIC VPN/bond share UDP/443. Optional
VLESS uses an exact, distinct SNI routed to its private TLS backend. AmneziaWG
retains its own configurable UDP port (initial default 51820).

```sh
sudo ./install-server.py quic.example.org --enable-vless \
  --vless-server-name vless.quic.example.org --email admin@example.org
```

Both DNS names must have A records pointing to this server. TCP/80 is required
for ACME HTTP validation and renewal. The generated frontend uses IPv4; do not
publish AAAA records until the complete IPv6 frontend/challenge path is configured.
TCP/443 and UDP/443 must be available. An existing nginx is never stopped, moved,
reinstalled, or upgraded by the direct installer. If another frontend owns 443,
move it to a private backend manually or select split ingress.

The optional VLESS configuration has `listen: 127.0.0.1:9444`,
`endpoint: vless.quic.example.org:443`, and `accept_proxy_protocol: true`.
`server.json` contains an exact `tls_routes` entry with `proxy_protocol: true`.
The routed name must differ from `tls_host` and every `vpn_sni_names` entry.
Unrelated manually configured routes are preserved; conflicting routes are
rejected. `--vless-port` chooses the private backend port, never the exported
unified public port. Do not open the private port in public firewall rules.

VPN and bond ALPNs always require verified client certificates. A mixed offer
containing echo and VPN/bond ALPNs uses VPN authentication. Echo-only offers
remain usable without a client certificate. Session tickets are disabled on the
shared QUIC ingress to isolate authentication policies.

## Existing installations and explicit migration

An ordinary reinstall preserves the saved frontend, ports, certificate paths,
administrator credentials, routing policy, and persisted VLESS settings. It does
not migrate an existing split installation. `--ingress unified` alone is refused
for an existing split installation; use `--migrate-unified` deliberately.

For the current production names, after verifying the HTTP challenge webroot:

```sh
sudo ./install-server.py quic-demo.vpnc.ru \
  --migrate-unified --enable-vless \
  --vless-server-name ui.quic-demo.vpnc.ru --vless-port 9444 \
  --acme --acme-webroot /var/www/quic-demo
```

The existing HTTP server must serve `/.well-known/acme-challenge/` from that
webroot for **both** names before running this command. The direct installer
never edits that HTTP server. If nginx owns public TCP/443, migration refuses
during preflight while the current services remain running. Move that frontend
manually to a private port first. An existing `tls_fallback`, such as
`127.0.0.1:9443`, is preserved.

Migration changes echo QUIC, VPN QUIC, and mTLS profile endpoints to port 443.
It preserves `public_tls_min`, including optional TLS 1.0 for a legacy Web UI proxy. HTTPS VPN routes independently require TLS 1.3 and a verified client certificate, even on this shared listener. QUIC and VLESS retain their separate TLS policies.
It stops both managed workers, creates a durable identity snapshot, and updates
the authoritative persisted VLESS configuration before restarting. Device UUIDs,
CA keys, users, revoked device flags, and worker tombstones are retained. The
Go reconciler advances the VLESS revision and installer health checks require
an acknowledgement from the worker running as the service's DynamicUser.
A persisted VLESS disable remains authoritative even with `--enable-vless`.
Concurrent VLESS changes during certificate preparation cause migration to
abort rather than overwrite newer authentication settings. Existing REALITY
security, private keys, permitted names, and flow remain unchanged; the selected
migration SNI must already be allowed by its REALITY configuration.

Refresh or reimport existing phone profiles after port or SNI migration. Older
profiles continue using their old addresses until refreshed. Migration restarts
active sessions. The installer prints the required public ports and leaves
host/cloud firewall rules unchanged.

If health fails after new services can mutate state, identities and VLESS
revocations remain intact and both services stay stopped behind persistent
recovery gates. Inspect `/etc/quic-lab/recovery-required` and the unit drop-ins;
install a compatible release and reconcile the current state before restarting.
Never restore an old identity or worker-state backup after that point.

## Split ports and manual certificates

```sh
sudo ./install-server.py quic.example.org --ingress split --frontend direct \
  --echo-quic-port 4433 --vpn-quic-port 4434 --mtls-port 8443
```

`--frontend nginx --ingress split` retains the older managed nginx layout.
An installed layout and custom ports are preserved by subsequent ordinary
updates. If an old nginx site already uses a requested domain, integrate it
manually instead of overwriting an unmanaged site.

```sh
sudo ./install-server.py quic.example.org --enable-vless \
  --vless-server-name vless.quic.example.org \
  --cert /absolute/path/fullchain.pem --key /absolute/path/privkey.pem
```

The manual certificate's SAN must cover both configured TLS names. Manual paths
and external renewal remain supported. `--acme` explicitly switches a saved
manual-certificate installation to the managed Let's Encrypt lineage; it cannot
be combined with `--cert`/`--key`.

## ACME operation and renewal

Without a custom pair, certbot obtains one SAN certificate for the main HTTPS
name and TLS VLESS name, using noninteractive HTTP validation. If the owned
lineage exists but lacks the VLESS SAN, the installer expands it while preserving
its additional DNS names. An expired certificate with matching SANs is renewed.
Wildcard lineages require external DNS challenge management and should remain
manual.

On a free HTTP/80 port, acquisition uses certbot standalone. If an existing HTTP
server owns port 80, configure its challenge location for every name and pass
`--acme-webroot /absolute/existing/directory`. That path is saved for later
installer runs. The selected challenge method is retained by certbot for
renewals. DNS preflight requires usable IPv4 records with a common server
address; certbot performs the authoritative public challenge validation.

The installer enables `certbot.timer` and writes a deploy hook that checks
`RENEWED_LINEAGE` before restarting managed QUIC Lab services. In split nginx
mode it also validates and reloads the managed nginx frontend; direct mode
leaves nginx and unrelated Xray/3x-ui services alone. Systemd `LoadCredential`
copies the new certificate pair on service restart.

Verify renewal after installation:

```sh
sudo certbot renew --cert-name quic-demo.vpnc.ru --dry-run
systemctl is-active certbot.timer
```

Certbot's SAN expansion and deploy-hook behavior are documented in its
[official user guide](https://eff-certbot.readthedocs.io/en/stable/using.html).
