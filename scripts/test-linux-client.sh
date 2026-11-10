#!/bin/bash
# Test-only: two private namespaces; no host routes/interfaces are changed.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo 'SKIP: requires root / CAP_NET_ADMIN'; exit 77; }
command -v ip >/dev/null && command -v python3 >/dev/null
[ -c /dev/net/tun ] || { echo 'SKIP: /dev/net/tun unavailable'; exit 77; }
cd "$(dirname "$0")/.."
work=$(mktemp -d /tmp/quic-linux-client.XXXXXX)
client=qlc-$$; server=qls-$$; router=qlr-$$; cmade=0; smade=0; rmade=0; cp=; sp=
peer=${SSH_CONNECTION:-}; peer=${peer%% *}; peer=${peer:-127.0.0.1}
before=$(ip route get "$peer")
cleanup(){
 result=$?
 [ -z "$cp" ] || { kill "$cp" 2>/dev/null || true; wait "$cp" 2>/dev/null || true; }
 [ -z "$sp" ] || { kill "$sp" 2>/dev/null || true; wait "$sp" 2>/dev/null || true; }
 [ "$cmade" = 0 ] || ip netns del "$client"
 [ "$smade" = 0 ] || ip netns del "$server"
 [ "$rmade" = 0 ] || ip netns del "$router"
 if [ "$result" != 0 ]; then
  for log in "$work/fixture.log" "$work/probe.log" "$work/loss.log"; do [ ! -f "$log" ] || cat "$log"; done
 fi
 after=$(ip route get "$peer")
 if [ "$before" != "$after" ]; then echo 'FAIL: host management route changed' >&2; return 1; fi
 rm -rf -- "$work"
}
trap cleanup EXIT
# Build before creating system resources.
go build -o "$work/probe" ./cmd/linux-vpn-client
go test -c -o "$work/fixture" ./internal/vpnclienttest
go test -c -o "$work/standalone-fixture" ./internal/vless
ip netns add "$client"; cmade=1
ip netns add "$server"; smade=1
ip -n "$client" link add ql0 type veth peer name ql1
ip -n "$client" link set ql1 netns "$server"
prefix=30; [ "${VPN_PROBE_FULL:-0}" != 1 ] || prefix=32
ip -n "$client" address add 198.18.0.1/$prefix dev ql0
ip -n "$server" address add 198.18.0.2/30 dev ql1
ip -n "$server" address add 192.0.2.2/32 dev lo
ip -n "$client" link set lo up; ip -n "$client" link set ql0 up
ip -n "$server" link set lo up; ip -n "$server" link set ql1 up
if [ "${VPN_PROBE_FULL:-0}" = 1 ]; then
 ip -n "$server" address add 198.18.0.6/32 dev ql1
 if [ "${VPN_PROBE_ONLINK:-0}" = 1 ]; then
  ip -n "$client" route add default via 198.18.0.6 dev ql0 onlink
 else
  ip -n "$client" route add 198.18.0.6/32 dev ql0
  ip -n "$client" route add default via 198.18.0.6 dev ql0 metric 600
 fi
 ip -n "$client" address add 192.168.50.1/24 dev ql0
fi
if [ "${VPN_PROBE_ICMP:-0}" = 1 ]; then
 ip netns add "$router"; rmade=1
 ip -n "$server" link add qlr0 type veth peer name qlr1
 ip -n "$server" link set qlr1 netns "$router"
 ip -n "$server" address add 203.0.113.1/30 dev qlr0
 ip -n "$router" address add 203.0.113.2/30 dev qlr1
 ip -n "$server" link set qlr0 up
 ip -n "$router" link set qlr1 up
 ip -n "$router" link set lo up
 ip -n "$router" link add blackhole0 type dummy
 ip -n "$router" link set blackhole0 up
 ip -n "$router" route add 192.0.2.3/32 dev blackhole0
 ip netns exec "$router" sysctl -q -w net.ipv4.ip_forward=1
 ip -n "$server" route add 192.0.2.3/32 via 203.0.113.2
fi
baseline=$(ip -n "$client" -4 route show)
baseline_default=$(ip -n "$client" -4 route show default)
for transport in ${VPN_PROBE_TRANSPORTS:-quic https awg vless vless-ws}; do
 case "$transport" in quic|https|awg|vless|vless-ws) ;; *) echo 'Unsupported fixture transport'; exit 1;; esac
 rm -f "$work/client.json"
 fixture="$work/fixture"; testname=TestNamespaceFixture
 case "$transport" in awg|vless|vless-ws) fixture="$work/standalone-fixture"; testname=TestDesktopNamespaceFixture;; esac
 VPN_PROBE_FIXTURE_DIR="$work" VPN_PROBE_TRANSPORT="$transport" ip netns exec "$server" "$fixture" -test.v -test.run "^${testname}$" > "$work/fixture.log" 2>&1 & sp=$!
 for i in $(seq 1 100); do [ -f "$work/client.json" ] && break; sleep .1; done
 [ -f "$work/client.json" ] || { cat "$work/fixture.log"; exit 1; }
 python3 - "$work/client.json" <<'PYCONFIG'
import json,sys,os
p=sys.argv[1];d=json.load(open(p));d['targets']=['192.0.2.2/32']
if os.getenv('VPN_PROBE_ICMP')=='1' and d['transport'] in ('quic','https'):d['targets'].append('192.0.2.3/32')
if os.getenv('VPN_PROBE_FULL')=='1':
 d['targets']=['0.0.0.0/0']
 if 'awg_config' in d:d['awg_config']=d['awg_config'].replace('AllowedIPs=192.0.2.2/32','AllowedIPs=0.0.0.0/0')
json.dump(d,open(p,'w'))
PYCONFIG
 ip netns exec "$client" "$work/probe" -validate -config "$work/client.json"
 if [ "${VPN_PROBE_FULL:-0}" = 1 ]; then
  # Fail after the endpoint pin and first /1 were installed; rollback must
  # remove both without touching the physical default or connected routes.
  mkdir -p "$work/fail-bin"
  cat > "$work/fail-bin/ip" <<'FAILIP'
#!/bin/sh
if [ "$1 $2 $3 $4" = '-4 route add 128.0.0.0/1' ]; then exit 42; fi
exec "$QUIC_REAL_IP" "$@"
FAILIP
  chmod 755 "$work/fail-bin/ip"
  if QUIC_REAL_IP="$(command -v ip)" ip netns exec "$client" env PATH="$work/fail-bin:$PATH" "$work/probe" -config "$work/client.json" > "$work/partial.log" 2>&1; then
   echo 'FAIL: injected route error accepted'; exit 1
  fi
  grep -q 'iproute2 operation failed' "$work/partial.log"
  [ "$baseline" = "$(ip -n "$client" -4 route show)" ]
  echo "$transport: partial-start rollback PASS"
 fi
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
 if [ "${VPN_PROBE_FULL:-0}" = 1 ]; then
  ip -n "$client" -4 route get 1.1.1.1 | grep -q "dev $tun"
  ip -n "$client" -4 route get 192.0.2.2 | grep -q "dev $tun"
  ip -n "$client" -4 route get 198.18.0.2 | grep -q 'via 198.18.0.6 dev ql0'
  ip -n "$client" -4 route get 192.168.50.2 | grep -q 'dev ql0'
  [ "$(ip -n "$client" -4 route show dev "$tun" | wc -l)" = 2 ]
  [ "$(ip -n "$client" -4 route show default)" = "$baseline_default" ]
 fi
 # Wait beyond the former startup-deadline regression boundary.
 sleep 32
 if [ "${VPN_PROBE_ICMP:-0}" = 1 ]; then
  ip netns exec "$client" python3 scripts/probe-linux-icmp.py "$transport"
 fi
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
  print(('TCP' if kind==socket.SOCK_STREAM else 'UDP')+' payload: PASS',flush=True)
print('TCP and UDP payload round-trip: PASS')
PY
 kill -TERM "$cp"; wait "$cp"; cp=
 grep -q '"state":"stopped"' "$work/probe.log"
 if ip -n "$client" link show "$tun" >/dev/null 2>&1; then echo 'FAIL: TUN leaked'; exit 1; fi
 [ "$baseline" = "$(ip -n "$client" -4 route show)" ]
 case "$transport" in awg|vless|vless-ws)
  kill -TERM "$sp"; wait "$sp"; sp=
  echo "$transport: real TUN TCP/UDP and Stop cleanup PASS (engine readiness, no server-loss detection claim)"
  continue;;
 esac
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
 [ "$baseline" = "$(ip -n "$client" -4 route show)" ]
 echo "$transport: transport loss cleanup PASS"
 echo "$transport: real TUN TCP/UDP and Stop cleanup PASS"
done
[ "$before" = "$(ip route get "$peer")" ]
echo 'Host management route unchanged: PASS'
