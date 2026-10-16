#!/bin/bash
# Test-only: two private namespaces; no host routes/interfaces are changed.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo 'SKIP: requires root / CAP_NET_ADMIN'; exit 77; }
command -v ip >/dev/null && command -v python3 >/dev/null
[ -c /dev/net/tun ] || { echo 'SKIP: /dev/net/tun unavailable'; exit 77; }
cd "$(dirname "$0")/.."
work=$(mktemp -d /tmp/quic-linux-client.XXXXXX)
client=qlc-$$; server=qls-$$; cmade=0; smade=0; cp=; sp=
peer=${SSH_CONNECTION:-}; peer=${peer%% *}; peer=${peer:-127.0.0.1}
before=$(ip route get "$peer")
cleanup(){
 [ -z "$cp" ] || { kill "$cp" 2>/dev/null || true; wait "$cp" 2>/dev/null || true; }
 [ -z "$sp" ] || { kill "$sp" 2>/dev/null || true; wait "$sp" 2>/dev/null || true; }
 [ "$cmade" = 0 ] || ip netns del "$client"
 [ "$smade" = 0 ] || ip netns del "$server"
 after=$(ip route get "$peer")
 if [ "$before" != "$after" ]; then echo 'FAIL: host management route changed' >&2; return 1; fi
 rm -rf -- "$work"
}
trap cleanup EXIT
# Build before creating system resources.
go build -o "$work/probe" ./cmd/linux-vpn-client
go test -c -o "$work/fixture" ./internal/vpnclienttest
ip netns add "$client"; cmade=1
ip netns add "$server"; smade=1
ip -n "$client" link add ql0 type veth peer name ql1
ip -n "$client" link set ql1 netns "$server"
ip -n "$client" address add 198.18.0.1/30 dev ql0
ip -n "$server" address add 198.18.0.2/30 dev ql1
ip -n "$server" address add 192.0.2.2/32 dev lo
ip -n "$client" link set lo up; ip -n "$client" link set ql0 up
ip -n "$server" link set lo up; ip -n "$server" link set ql1 up
for transport in quic https; do
 rm -f "$work/client.json"
 VPN_PROBE_FIXTURE_DIR="$work" VPN_PROBE_TRANSPORT="$transport" ip netns exec "$server" "$work/fixture" -test.run '^TestNamespaceFixture$' > "$work/fixture.log" 2>&1 & sp=$!
 for i in $(seq 1 100); do [ -f "$work/client.json" ] && break; sleep .1; done
 [ -f "$work/client.json" ] || { cat "$work/fixture.log"; exit 1; }
 python3 - "$work/client.json" <<'PYCONFIG'
import json,sys
p=sys.argv[1];d=json.load(open(p));d['targets']=['192.0.2.2/32'];json.dump(d,open(p,'w'))
PYCONFIG
 ip netns exec "$client" "$work/probe" -validate -config "$work/client.json"
 ip netns exec "$client" "$work/probe" -config "$work/client.json" > "$work/probe.log" 2>&1 & cp=$!
 for i in $(seq 1 100); do grep -q '"state":"running"' "$work/probe.log" && break; kill -0 "$cp" || break; sleep .1; done
 grep -q '"state":"running"' "$work/probe.log" || { cat "$work/probe.log"; exit 1; }
 tun=$(python3 - "$work/probe.log" <<'PYTUN'
import json,sys
for line in open(sys.argv[1]):
 if not line.startswith('{'): continue
 d=json.loads(line)
 if d.get('state')=='running': print(d['tun']);break
PYTUN
)
 ip -n "$client" -4 -j address show dev "$tun" | python3 -c 'import json,sys;assert not any(x["addr_info"] for x in json.load(sys.stdin))'
 # Wait beyond the former startup-deadline regression boundary.
 sleep 32
 ip netns exec "$client" python3 - <<'PY'
import socket
for kind,port in [(socket.SOCK_STREAM,9001),(socket.SOCK_DGRAM,9002)]:
 with socket.socket(socket.AF_INET,kind) as s:
  s.settimeout(8);s.connect(('192.0.2.2',port));payload=b'quic-platform-probe'*50;s.sendall(payload)
  result=b''
  while len(result)<len(payload):
   block=s.recv(4096)
   if not block:raise RuntimeError('early EOF')
   result+=block
  assert result==payload
print('TCP and UDP payload round-trip: PASS')
PY
 kill -TERM "$cp"; wait "$cp"; cp=
 grep -q '"state":"stopped"' "$work/probe.log"
 if ip -n "$client" link show "$tun" >/dev/null 2>&1; then echo 'FAIL: TUN leaked'; exit 1; fi
 # Start again, then kill the server and require automatic cleanup.
 ip netns exec "$client" "$work/probe" -config "$work/client.json" > "$work/loss.log" 2>&1 & cp=$!
 for i in $(seq 1 100); do grep -q '"state":"running"' "$work/loss.log" && break; sleep .1; done
 grep -q '"state":"running"' "$work/loss.log"
 kill -TERM "$sp"; wait "$sp"; sp=
 for i in $(seq 1 400); do kill -0 "$cp" 2>/dev/null || break; sleep .1; done
 if kill -0 "$cp" 2>/dev/null; then echo 'FAIL: disconnected client retained routes'; exit 1; fi
 result=0; wait "$cp" || result=$?; cp=
 [ "$result" = 1 ]
 grep -q 'transport disconnected' "$work/loss.log"
 if ip -n "$client" -o link show | grep -Eq ': ql[0-9a-f]{10}[:@]' ; then echo 'FAIL: disconnected TUN leaked'; exit 1; fi
 echo "$transport: transport loss cleanup PASS"
 echo "$transport: real TUN TCP/UDP and Stop cleanup PASS"
done
[ "$before" = "$(ip route get "$peer")" ]
echo 'Host management route unchanged: PASS'
