#!/bin/bash
# Root-only acceptance in private network namespaces; no host routes changed.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo 'SKIP: requires root / CAP_NET_ADMIN'; exit 77; }
command -v ip >/dev/null && command -v python3 >/dev/null
[ -c /dev/net/tun ] || { echo 'SKIP: /dev/net/tun unavailable'; exit 77; }
cd "$(dirname "$0")/.."
work=$(mktemp -d /tmp/quic-linux-app.XXXXXX)
client=qla-$$; server=qlb-$$; cmade=0; smade=0; cp=; sp=; ep=; ap=
peer=${SSH_CONNECTION:-}; peer=${peer%% *}; peer=${peer:-127.0.0.1}
before=$(ip route get "$peer")
cleanup(){
 result=$?
 for p in "$ap" "$cp" "$sp" "$ep"; do
  [ -z "$p" ] || { kill -CONT "$p" 2>/dev/null || true; kill "$p" 2>/dev/null || true; wait "$p" 2>/dev/null || true; }
 done
 if [ "$result" != 0 ]; then
  for log in "$work"/*.log; do [ ! -f "$log" ] || cat "$log"; done
 fi
 [ "$cmade" = 0 ] || ip netns del "$client"
 [ "$smade" = 0 ] || ip netns del "$server"
 [ "$before" = "$(ip route get "$peer")" ] || { echo 'FAIL: management route changed'; return 1; }
 rm -rf -- "$work"
}
trap cleanup EXIT
go build -o "$work/client" ./cmd/linux-vpn-client
go test -c -o "$work/app-vpn" ./internal/linuxclient
cp "$work/app-vpn" "$work/app-direct"
go test -c -o "$work/fixture" ./internal/vpnclienttest
go test -c -o "$work/standalone-fixture" ./internal/vless
ip netns add "$client"; cmade=1
ip netns add "$server"; smade=1
ip -n "$client" link add ql0 type veth peer name ql1
ip -n "$client" link set ql1 netns "$server"
ip -n "$client" address add 198.18.0.1/30 dev ql0
ip -n "$server" address add 198.18.0.2/30 dev ql1
ip -n "$server" address add 192.0.2.2/32 dev lo
ip -n "$client" link set lo up; ip -n "$client" link set ql0 up
ip -n "$server" link set lo up; ip -n "$server" link set ql1 up
# The direct exit uses this namespace's underlay, even while a more specific TUN route exists.
ip -n "$client" route add default via 198.18.0.2 dev ql0
ip netns exec "$server" python3 -u - "$work" > "$work/echo.log" 2>&1 <<'PY' &
import socket,threading,sys,pathlib
def tcp(s):
 while True:
  c,peer=s.accept()
  def reply(c,peer):
   with c:
    while True:
     payload=b''
     while len(payload)<750:
      part=c.recv(750-len(payload))
      if not part:return
      payload+=part
     c.sendall(peer[0].encode()+b'|'+payload)
  threading.Thread(target=reply,args=(c,peer),daemon=True).start()
def udp(s):
 while True:
  payload,peer=s.recvfrom(4096)
  if s.getsockname()[1]==53 and 12<len(payload)<512 and payload[2:12]==b'\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00':
   s.sendto(payload[:2]+b'\x81\x83'+payload[4:],peer)
  else:s.sendto(peer[0].encode()+b'|'+payload,peer)
for kind,port in [(socket.SOCK_STREAM,9011),(socket.SOCK_DGRAM,9012),(socket.SOCK_DGRAM,53)]:
 s=socket.socket(socket.AF_INET,kind);s.bind(('192.0.2.2',port))
 if kind==socket.SOCK_STREAM:s.listen(16)
 threading.Thread(target=tcp if kind==socket.SOCK_STREAM else udp,args=(s,),daemon=True).start()
pathlib.Path(sys.argv[1]+'/echo.ready').touch()
threading.Event().wait()
PY
ep=$!
for i in $(seq 1 100); do [ -f "$work/echo.ready" ] && break; sleep .1; done
[ -f "$work/echo.ready" ]
configure(){
 python3 - "$work" "$1" "$2" <<'PY'
import json,sys,os
w,mode,unknown=sys.argv[1:];d=json.load(open(w+'/base.json'))
d['targets']=['192.0.2.2/32']
if os.getenv('VPN_PROBE_FULL')=='1':
 d['targets']=['0.0.0.0/0']
 if 'awg_config' in d:d['awg_config']=d['awg_config'].replace('AllowedIPs=192.0.2.2/32','AllowedIPs=0.0.0.0/0')
d['app_routing']={'version':2,'enabled':True,'mode':mode,'only':[w+'/app-vpn'],'except':[w+'/app-direct'],'unknown':unknown}
if os.getenv('VPN_PROBE_HEALTH')=='1' and d['transport'] in ('awg','vless'):d['health_dns']='192.0.2.2'
json.dump(d,open(w+'/client.json','w'))
PY
}
start_client(){
 ip netns exec "$client" "$work/client" -validate -config "$work/client.json"
 ip netns exec "$client" "$work/client" -config "$work/client.json" > "$work/client.log" 2>&1 & cp=$!
 for i in $(seq 1 100); do grep -q '"state":"running"' "$work/client.log" && break; kill -0 "$cp" || break; sleep .1; done
 grep -q '"state":"running"' "$work/client.log"
}
stop_client(){
 kill -TERM "$cp"; wait "$cp"; cp=
 grep -q '"state":"stopped"' "$work/client.log"
 if ip -n "$client" -o link show | grep -Eq ': ql[0-9a-f]{10}[:@]'; then echo 'FAIL: leaked TUN'; exit 1; fi
 [ "$(ip -n "$client" -4 route show | wc -l)" = 2 ]
}
probe(){
 QUIC_APP_PROBE_EXPECT="$2" ip netns exec "$client" "$work/$1" -test.v -test.run '^TestLinuxPerAppProbe$' -test.timeout=15s
}
for transport in ${VPN_PROBE_TRANSPORTS:-quic https awg vless vless-ws}; do
 case "$transport" in quic|https|awg|vless|vless-ws) ;; *) echo 'Unsupported fixture transport'; exit 1;; esac
 rm -f "$work/client.json"
 fixture="$work/fixture"; testname=TestNamespaceFixture
 case "$transport" in awg|vless|vless-ws) fixture="$work/standalone-fixture"; testname=TestDesktopNamespaceFixture;; esac
 VPN_PROBE_SOURCE_ECHO=1 VPN_PROBE_FIXTURE_DIR="$work" VPN_PROBE_TRANSPORT="$transport" ip netns exec "$server" "$fixture" -test.v -test.run "^${testname}$" > "$work/fixture.log" 2>&1 & sp=$!
 for i in $(seq 1 100); do [ -f "$work/client.json" ] && break; sleep .1; done
 [ -f "$work/client.json" ]; cp "$work/client.json" "$work/base.json"
 vpn_source=192.0.2.2; [ "$transport" != awg ] || vpn_source=10.55.0.2
 if [ "${VPN_PROBE_LIVE:-0}" = 1 ]; then
  configure only_selected block; start_client
  if [ "${VPN_PROBE_HEALTH:-0}" = 1 ]; then
   for i in $(seq 1 100); do grep -q '"vpn_dns_ready"' "$work/client.log" && break; sleep .05; done
   grep -q '"vpn_dns_ready"' "$work/client.log"
  fi
  tun_before=$(ip -n "$client" -o link show | sed -n 's/.*: \(ql[0-9a-f]\{10\}\):.*/\1/p')
  rm -f "$work/hold.count" "$work/hold.stop"
  QUIC_APP_HOLD_DIR="$work" ip netns exec "$client" "$work/app-direct" -test.v -test.run '^TestLinuxPersistentDirectProbe$' -test.timeout=210s > "$work/hold.log" 2>&1 & ap=$!
  for i in $(seq 1 100); do [ -s "$work/hold.count" ] && break; sleep .05; done
  [ -s "$work/hold.count" ]
  # New app-direct flows switch to VPN while its two old sockets stay direct.
  python3 - "$work/client.json" "$work/app-direct" <<'PY'
import json,sys
p=sys.argv[1];d=json.load(open(p));d['app_routing']['only']=[sys.argv[2]];json.dump(d,open(p,'w'))
PY
  kill -HUP "$cp"
  for i in $(seq 1 100); do grep -q '"status":"applied"' "$work/client.log" && break; sleep .05; done
  grep -q '"status":"applied"' "$work/client.log"
  probe app-direct "$vpn_source"
  python3 - "$work/client.json" <<'PY'
import json,sys
p=sys.argv[1];d=json.load(open(p));d['targets']=['192.0.2.3/32'];json.dump(d,open(p,'w'))
PY
  kill -HUP "$cp"
  for i in $(seq 1 100); do grep -q '"status":"rejected"' "$work/client.log" && break; sleep .05; done
  grep -q '"status":"rejected"' "$work/client.log"
  probe app-direct "$vpn_source"
  applied=$(grep -c '"status":"applied"' "$work/client.log")
  configure only_selected block; kill -HUP "$cp"
  for i in $(seq 1 100); do [ "$(grep -c '"status":"applied"' "$work/client.log")" -gt "$applied" ] && break; sleep .05; done
  [ "$(grep -c '"status":"applied"' "$work/client.log")" -gt "$applied" ]
  if [[ "$transport" == quic || "$transport" == https || "${VPN_PROBE_HEALTH:-0}" == 1 ]]; then
   kill -STOP "$sp"
   for i in $(seq 1 2200); do grep -q '"event":"network","state":"reconnecting"' "$work/client.log" && break; kill -0 "$ap"; sleep .05; done
   grep -q '"event":"network","state":"reconnecting"' "$work/client.log"
   before_count=$(cat "$work/hold.count");sleep .5
   [ "$(cat "$work/hold.count")" -gt "$before_count" ]
   probe app-vpn block
   if [ "${VPN_PROBE_HEALTH:-0}" = 1 ]; then
    grep -q '"event":"readiness","state":"unavailable"' "$work/client.log"
    # Wait through at least one failed replacement; local engine start cannot publish ready.
    sleep 3
    ! grep -q '"event":"network","state":"ready"' "$work/client.log"
   fi
   if [ "${VPN_PROBE_CANCEL_RECOVERY:-0}" = 1 ]; then
    touch "$work/hold.stop";wait "$ap";ap=
    stop_started=$SECONDS
    stop_client
    [ "$((SECONDS-stop_started))" -le 5 ]
    echo "$transport: Stop during failed VPN recovery bounded, TUN/routes cleaned PASS"
    kill -CONT "$sp";kill -TERM "$sp";wait "$sp";sp=
    continue
   fi
   kill -CONT "$sp"
   for i in $(seq 1 1200); do grep -q '"event":"network","state":"ready"' "$work/client.log" && break; kill -0 "$ap"; sleep .05; done
   grep -q '"event":"network","state":"ready"' "$work/client.log"
   probe app-vpn "$vpn_source"
   echo "$transport: same direct TCP/UDP sockets survived VPN outage/recovery PASS"
  fi
  [ "$tun_before" = "$(ip -n "$client" -o link show | sed -n 's/.*: \(ql[0-9a-f]\{10\}\):.*/\1/p')" ]
  touch "$work/hold.stop";wait "$ap";ap=
  stop_client
  echo "$transport: hot policy, rejected non-policy reload, pinned flows and unchanged TUN PASS"
  if [ "${VPN_PROBE_HEALTH:-0}" = 1 ]; then
   # Health recovery must also work without per-app routing.
   configure only_selected block
   python3 - "$work/client.json" <<'PY'
import json,sys
p=sys.argv[1];d=json.load(open(p));d.pop('app_routing');json.dump(d,open(p,'w'))
PY
   start_client
   for i in $(seq 1 100); do grep -q '"vpn_dns_ready"' "$work/client.log" && break; sleep .05; done
   grep -q '"vpn_dns_ready"' "$work/client.log"
   kill -STOP "$sp"
   for i in $(seq 1 600); do grep -q '"event":"network","state":"reconnecting"' "$work/client.log" && break; sleep .05; done
   grep -q '"event":"network","state":"reconnecting"' "$work/client.log"
   kill -CONT "$sp"
   for i in $(seq 1 600); do grep -q '"event":"network","state":"ready"' "$work/client.log" && break; sleep .05; done
   grep -q '"event":"network","state":"ready"' "$work/client.log"
   probe app-vpn "$vpn_source"
   stop_client
   echo "$transport: opt-in DNS recovery without per-app routing PASS"
  fi
  kill -TERM "$sp";wait "$sp";sp=
  continue
 fi
 for mode in only_selected all_except_selected; do
  configure "$mode" block
  start_client
  probe app-vpn "$vpn_source"
  probe app-direct 198.18.0.1
  # Ordinary captured IPv4 can originate from AF_INET6 mapped sockets too.
  QUIC_APP_PROBE_DUALSTACK=1 probe app-vpn "$vpn_source"
  QUIC_APP_PROBE_DUALSTACK=1 probe app-direct 198.18.0.1
  # Even an excluded application's captured DNS remains in the VPN.
  QUIC_APP_PROBE_DNS=1 probe app-direct "$vpn_source"
  stop_client
  python3 - "$work/client.log" <<'PY'
import json,sys
events=[json.loads(x) for x in open(sys.argv[1]) if x.startswith('{')]
s=[x['counters'] for x in events if x.get('event')=='app_routing'][-1]
assert s['resolved']>=8 and s['unknown']==0,s
assert s['direct_tx_bytes']>0 and s['direct_rx_bytes']>0 and s['dns']>=1,s
PY
  echo "$transport $mode: distinct IPv4/dual-stack TCP/UDP source IPs, captured DNS, counters and cleanup PASS"
 done
 # An unlinked running executable must use the unknown policy, never its former list entry.
 for unknown in tunnel direct block; do
  configure only_selected "$unknown"; start_client
  cp "$work/app-vpn" "$work/app-deleted"
  rm -f "$work/gate" "$work/gate.ready"
  expected=$vpn_source; [ "$unknown" != direct ] || expected=198.18.0.1
  [ "$unknown" != block ] || expected=block
  QUIC_APP_PROBE_GATE="$work/gate" QUIC_APP_PROBE_EXPECT="$expected" ip netns exec "$client" "$work/app-deleted" -test.v -test.run '^TestLinuxPerAppProbe$' -test.timeout=15s > "$work/deleted.log" 2>&1 & ap=$!
  for i in $(seq 1 100); do [ -f "$work/gate.ready" ] && break; sleep .02; done
  [ -f "$work/gate.ready" ]; rm "$work/app-deleted"; touch "$work/gate"
  wait "$ap"; ap=
  stop_client
  python3 - "$work/client.log" "$unknown" <<'PY'
import json,sys
events=[json.loads(x) for x in open(sys.argv[1]) if x.startswith('{')]
s=[x['counters'] for x in events if x.get('event')=='app_routing'][-1]
assert s['unknown']>=2,s
if sys.argv[2]=='block':assert s['blocked']>=2,s
PY
  echo "$transport unknown=$unknown: deleted executable TCP/UDP and cleanup PASS"
 done
 kill -TERM "$sp"; wait "$sp"; sp=
done
echo 'Host management route unchanged: PASS'
