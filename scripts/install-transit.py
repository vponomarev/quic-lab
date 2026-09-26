#!/usr/bin/env python3
"""Enable/disable global VPN transit through an upstream AmneziaWG .conf."""
import argparse, importlib.util, ipaddress, json, os, shutil, socket, subprocess, sys, tempfile
from pathlib import Path
spec=importlib.util.spec_from_file_location("installer",Path(__file__).with_name("install-server.py"));i=importlib.util.module_from_spec(spec);spec.loader.exec_module(i)
def unit(guard=False):
    head=i.MARKER+"\n[Unit]\nDescription=QUIC Lab AWG transit"+(" route guard" if guard else " worker")+"\nAfter=network-online.target\nWants=network-online.target\n"
    if guard:return head+"\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/usr/bin/python3 /opt/quic-lab/transit-network.py start\n\n[Install]\nWantedBy=multi-user.target\n"
    return head+"Requires=quic-lab-transit-guard.service\nAfter=quic-lab-transit-guard.service\n\n[Service]\nUser=quic-lab\nDynamicUser=yes\nStateDirectory=quic-lab\nStateDirectoryMode=0700\nUMask=0077\nLoadCredential=transit.json:/etc/quic-lab/transit.json\nAmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE\nExecStart=/opt/quic-lab/quic-lab-transit -config ${CREDENTIALS_DIRECTORY}/transit.json\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=yes\nProtectSystem=strict\nProtectHome=yes\nPrivateTmp=yes\n\n[Install]\nWantedBy=multi-user.target\n"
def main():
    p=argparse.ArgumentParser(description=__doc__)
    group=p.add_mutually_exclusive_group(required=True);group.add_argument("--config",type=Path,help="Upstream AWG client .conf with IPv4 default AllowedIPs");group.add_argument("--update",action="store_true",help="Update worker using the saved private config");group.add_argument("--disable",action="store_true",help="Restore direct VPN egress; explicitly removes the route guard")
    p.add_argument("--binary",type=Path,default=Path(__file__).with_name("quic-lab-transit"))
    if len(sys.argv)==1:p.print_help();return
    a=p.parse_args()
    if os.geteuid()!=0:p.error("Run as root")
    admin=Path('/etc/quic-lab/admin.json');cfg=json.loads(admin.read_text())
    drops=[Path('/etc/systemd/system')/(s+'.service.d/quic-lab-transit.conf') for s in ['quic-lab','quic-lab-awg']]
    worker=Path('/etc/systemd/system/quic-lab-transit.service');guard=Path('/etc/systemd/system/quic-lab-transit-guard.service')
    for path in [worker,guard,*drops]:i.owned(path)
    if a.disable:
        if not cfg.get('transit'):p.error('Transit is not enabled')
        i.run('systemctl','stop','quic-lab')
        if cfg.get('awg'):i.run('systemctl','stop','quic-lab-awg')
        i.run('systemctl','stop','quic-lab-transit')
        i.run(sys.executable,'/opt/quic-lab/transit-network.py','stop')
        for path in drops:path.unlink(missing_ok=True)
        cfg.pop('transit',None);i.atomic(admin,json.dumps(cfg,indent=2)+'\n',0o600)
        i.run('systemctl','disable','--now','quic-lab-transit','quic-lab-transit-guard')
        i.run('systemctl','daemon-reload');i.run('systemctl','start','quic-lab')
        if cfg.get('awg'):i.run('systemctl','start','quic-lab-awg')
        Path('/etc/quic-lab/transit.json').unlink(missing_ok=True)
        print('Transit disabled; direct VPN egress restored');return
    if a.update:
        saved=json.loads(Path('/etc/quic-lab/transit.json').read_text())
        with tempfile.NamedTemporaryFile(mode='w',encoding='utf-8',prefix='quic-transit-',suffix='.conf') as temporary:
            temporary.write(saved['awg_config']);temporary.flush()
            a.config=Path(temporary.name)
            return configure(a,p,cfg,admin,drops,worker,guard)
    return configure(a,p,cfg,admin,drops,worker,guard)

def configure(a,p,cfg,admin,drops,worker,guard):
    metadata=json.loads(subprocess.check_output([str(a.binary.resolve()),'-inspect-awg',str(a.config.resolve())]))
    if '0.0.0.0/0' not in metadata['allowed_ips']:p.error('Upstream must allow 0.0.0.0/0')
    host,port=metadata['endpoint'].rsplit(':',1)
    endpoint=socket.getaddrinfo(host,int(port),socket.AF_INET,socket.SOCK_DGRAM)[0][4][0]+':'+port
    source=str(ipaddress.IPv4Address(metadata['address']))
    subnet=str(ipaddress.IPv4Interface(cfg['awg']['address']).network) if cfg.get('awg') else None
    if subnet and ipaddress.IPv4Address(source) in ipaddress.IPv4Network(subnet):p.error('Ingress AWG subnet overlaps transit address')
    if cfg.get('transit') and cfg['transit']['source_ip']!=source:p.error('Disable transit before changing its source address')
    packages=[pkg for cmd,pkg in [('ip','iproute2'),('iptables','iptables'),('sysctl','procps')] if not shutil.which(cmd)]
    if packages:i.run('apt-get','update');i.run('apt-get','install','--no-upgrade','-y',*packages)
    routes=json.loads(subprocess.check_output(['ip','-j','-4','route','show','table','main']))
    for route in routes:
        if route.get('dst','default')=='default' or route.get('dev')=='ql-exit0':continue
        if ipaddress.IPv4Address(source) in ipaddress.IPv4Network(route['dst'],strict=False):p.error('Transit address overlaps a host network')
    if not worker.exists() and subprocess.run(['ip','link','show','ql-exit0'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:p.error('Unmanaged ql-exit0 exists')
    front=[]
    if cfg.get('awg'):
        front=sorted({v[4][0] for v in socket.getaddrinfo(cfg['awg']['endpoint'].rsplit(':',1)[0],None,socket.AF_INET)})
    uplink=dict(source_ip=source,endpoint=endpoint,dns=metadata['dns'],probe_socket=str(Path(cfg['data_dir'])/'transit.sock'))
    private=dict(transit=uplink,awg_config=a.config.read_text(),ingress_subnet=subnet,front_ips=front)
    i.atomic(Path('/etc/quic-lab/transit.json'),json.dumps(private,indent=2)+'\n',0o600)
    i.atomic(Path('/opt/quic-lab/quic-lab-transit'),a.binary.read_bytes(),0o755)
    i.atomic(Path('/opt/quic-lab/transit-network.py'),Path(__file__).with_name('transit-network.py').read_bytes(),0o755)
    i.atomic(worker,unit());i.atomic(guard,unit(True))
    for path in drops:
        i.atomic(path,i.MARKER+'\n[Unit]\nRequires=quic-lab-transit-guard.service\nAfter=quic-lab-transit-guard.service\n')
    i.run('systemctl','daemon-reload')
    i.run('systemctl','stop','quic-lab')
    if cfg.get('awg'):i.run('systemctl','stop','quic-lab-awg')
    cfg['transit']=uplink;i.atomic(admin,json.dumps(cfg,indent=2)+'\n',0o600)
    i.run('systemctl','daemon-reload')
    # Any subsequent failure retains requested transit and guard. Never fall back to direct.
    i.run('systemctl','enable','quic-lab-transit-guard','quic-lab-transit')
    i.run('systemctl','restart','quic-lab-transit-guard')
    i.run('systemctl','restart','quic-lab-transit')
    i.run('systemctl','restart','quic-lab')
    if cfg.get('awg'):i.run('systemctl','restart','quic-lab-awg')
    i.run('systemctl','is-active','quic-lab-transit','quic-lab')
    print('Transit enabled. VPN uses AWG uplink with NAT; RTT probes upstream Endpoint through the tunnel. Check journalctl -u quic-lab-transit. If unavailable, repair the config or explicitly --disable; no automatic direct fallback.')
if __name__=='__main__':main()
