# Public HTTPS compatibility and diagnostics

The separate public `https_listen` endpoint defaults to TLS 1.3. An explicit
`public_tls_min` setting accepts `1.0`, `1.2`, or `1.3` (empty means `1.3`).
Example server.json additions for a legacy TLS inspection proxy:

```json
{
  "public_tls_min": "1.0",
  "public_tls_diagnostics": true
}
```

Merge these fields into the existing server configuration and restart the service.
This restarts active connections. Raising the minimum to `1.2` disables TLS 1.0/1.1.
Diagnostics can be disabled independently. Equivalent CLI flags:
`-public-tls-min 1.0 -public-tls-diagnostics`.

Legacy mode permits TLS 1.0 through 1.3 with ECDHE and AES-CBC/SHA1 for legacy peers;
it does not enable static RSA, RC4 or 3DES. The configured certificate still needs
to be compatible with the client. Modern peers can negotiate TLS 1.3 normally.
TLS 1.0 is obsolete: enable only when compatibility with a known intermediary
requires it, and raise the minimum when that intermediary supports newer TLS.

QUIC and the separate HTTPS/mTLS VPN listener are unchanged. Configuration rejects
a relaxed minimum when public HTTPS and the VPN listener use the same address;
deploy a separate public listener first. Other-SNI TLS passthrough remains unchanged.

With diagnostics enabled, journalctl contains:

- `public_tls_client_hello`: remote IP:port, SNI, offered versions and cipher suite
  names/IDs, ALPN, signature schemes and groups.
- `public_tls_connection`: HTTP connection state, handshake completion, selected
  TLS version/suite, ALPN and resumption. Failed handshakes also retain Go's TLS
  error message with the remote IP:port. Connections rejected before ClientHello
  parsing do not produce a client-hello event.
- `public_https_request`: method, remote IP:port, TLS version/suite and a restricted
  route label. Dynamic paths are redacted; query strings, authorization headers,
  cookies and request/response bodies are never logged. This is request metadata,
  not a complete HTTP access log or response-status audit.

Use remote IP **and port**, timestamps and handshake outcome to correlate retries.
An IP alone cannot identify the originating browser or distinguish proxy probes.
The SNI routing parser still runs before these diagnostics; failed inspection there
is not covered. Detailed diagnostics are opt-in and should be disabled after the
investigation to limit journal volume.
