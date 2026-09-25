Netstack adapter copied from amneziawg-go v1.0.4 (MIT; upstream copyright retained).

Local changes:
- `pkt.IsNil()` becomes `pkt == nil` for the existing tun2socks gVisor revision.
- Leave the device down until its owner configures keys and calls Up.
- Release packet/view references after use.
- Idempotent shutdown signals blocked producers/consumers and closes/waits for the stack.
- Ping reads check queued packets before waiting, avoiding lost notifications.

Cryptography and the protocol device remain the upstream module.
