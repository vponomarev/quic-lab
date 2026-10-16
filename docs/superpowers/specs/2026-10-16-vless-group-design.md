# VLESS subscription group

User authorized autonomous implementation, release, testing on Note9 and adding the provided private subscription as a second MDM profile for Note12. Preserve existing primary profile, application rules and MDM rights; do not remotely start Note12 VPN before user updates/enables MDM.

One ordinary VLESS profile owns 1–32 validated URI candidates in encrypted identity.vless_uris, with vless_uri as first candidate for compatibility. Subscription UI offers import as one group; refresh updates same group ID, retaining local routing. Full Xray routing/balancers are not imported. Provider URL remains encrypted subscription metadata; updates are manual in this iteration.

Android serialized VpnSession owns failover on its selected physical network. Two failed verified HTTPS heartbeat probes trigger next candidate, with 30-second per-candidate cooldown, bounded retry when all fail, no parallel probe storm. DNS uses selected Android Network and all sockets use existing protection/budget binder. No TUN recreation. Existing streams can end on engine rotation. Network change retries selected candidate and maintains normal LTE policy. Stop cancels gateway probes; epochs discard stale callbacks. Standalone single profiles retain behavior.

Gateway gains atomic SwitchVLESSProfile(canonical,resolvedEndpoint,binder) with strict validation before old engine closes. Only VLESS. Rotation events contain index/count/reason/time, never credential or URI. Bounded recent group events join MDM report under VPN permission, and server device page renders them. Existing diagnostic journal also gets events.

Release coordinated Android/server 0.9.5 after tests; test fixture deliberately unreachable first candidate + live allowed second, actual HTTPS response, deterministic cooldown/stop tests. Publish APK using existing signed update metadata workflow; stage Note12 group alongside existing primary through validated MDM configuration revision. No secret URL in Git.
