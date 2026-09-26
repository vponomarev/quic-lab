#!/usr/bin/env python3
"""Enable the optional AmneziaWG worker on an installed QUIC Lab server."""
import argparse
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import sys
import socket

spec = importlib.util.spec_from_file_location("installer", Path(__file__).with_name("install-server.py"))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)
atomic, run = installer.atomic, installer.run
MARKER = installer.MARKER
UNIT = Path("/etc/systemd/system/quic-lab-awg.service")

def unit_config():
    return f"""{MARKER}
[Unit]
Description=QUIC Lab AmneziaWG transport
After=network-online.target quic-lab.service
Wants=network-online.target quic-lab.service

[Service]
Type=simple
User=quic-lab
DynamicUser=yes
StateDirectory=quic-lab
StateDirectoryMode=0700
UMask=0077
LoadCredential=awg.json:/etc/quic-lab/awg.json
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
ExecStartPre=+/usr/bin/python3 /opt/quic-lab/awg-network.py start
ExecStart=/opt/quic-lab/quic-lab-awg -config ${{CREDENTIALS_DIRECTORY}}/awg.json
ExecStopPost=+/usr/bin/python3 /opt/quic-lab/awg-network.py stop
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/etc/quic-lab /proc/sys/net/ipv4/ip_forward

[Install]
WantedBy=multi-user.target
"""

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--binary", type=Path, default=Path(__file__).with_name("quic-lab-awg"))
    p.add_argument("--port", type=installer.port_number)
    p.add_argument("--address", help="Server tunnel IPv4/prefix; initial default 10.77.0.1/24")
    p.add_argument("--allowed-ips", type=installer.cidrs, help="Initial destination CIDRs; inherits GATEWAY_ALLOW where available")
    p.add_argument("--dns", help="Client IPv4 DNS; initial default 1.1.1.1")
    if len(sys.argv) == 1:
        p.print_help()
        return
    a = p.parse_args()
    if os.geteuid() != 0:
        p.error("Run as root")
    if not a.binary.is_file():
        p.error("AWG worker binary missing")
    path = Path("/etc/quic-lab/admin.json")
    previous = path.read_bytes()
    cfg = json.loads(previous)
    installer.owned(UNIT)
    if cfg.get("transit") and not cfg.get("awg"):
        p.error("Disable transit before first enabling ingress AWG, then re-enable transit to include its subnet")
    awg = dict(cfg.get("awg", dict(endpoint=cfg["vpn"]["hostname"]+":51820", address="10.77.0.1/24", dns="1.1.1.1", allowed_ips=["0.0.0.0/0"], interface="ql-awg0", mtu=1280)))
    if cfg.get("awg") and a.address and a.address != cfg["awg"]["address"]:
        p.error("Changing the address pool requires reissuing existing AWG profiles; choose it on first installation")
    if a.port:
        awg["endpoint"] = awg["endpoint"].rsplit(":", 1)[0]+":"+str(a.port)
    if a.address:
        awg["address"] = a.address
    if not cfg.get("awg"):
        policy = Path("/etc/quic-lab/server.env")
        inherited = "0.0.0.0/0"
        if policy.exists():
            import shlex
            for line in policy.read_text().splitlines():
                if line.startswith("GATEWAY_ALLOW="):
                    inherited = shlex.split(line.split("=", 1)[1])[0]
        awg["allowed_ips"] = installer.cidrs(a.allowed_ips or inherited).split(",")
    elif a.allowed_ips:
        p.error("AWG policy already exists; edit both admin.json and awg.json explicitly")
    if a.dns:
        awg["dns"] = a.dns
    subnet = ipaddress.IPv4Interface(awg["address"])
    if not 16 <= subnet.network.prefixlen <= 29 or subnet.ip == subnet.network.network_address or subnet.ip == subnet.network.broadcast_address:
        p.error("Address must be a host IPv4 /16../29")
    ipaddress.IPv4Address(awg["dns"])
    port = int(awg["endpoint"].rsplit(":", 1)[1])
    occupied = [int(cfg[s][k].rsplit(":", 1)[1]) for s,k in [("echo","endpoint"),("vpn","quic")]]
    if port in occupied:
        p.error("AWG UDP port conflicts with QUIC")
    if awg["interface"] != "ql-awg0":
        p.error("Interface must be ql-awg0")
    for cidr in awg["allowed_ips"]:
        ipaddress.IPv4Network(cidr)
    packages = [pkg for cmd,pkg in [("ip","iproute2"),("iptables","iptables"),("sysctl","procps")] if not shutil.which(cmd)]
    if packages:
        run("apt-get","update")
        run("apt-get","install","--no-upgrade","-y",*packages)
    routes = json.loads(subprocess.check_output(["ip","-j","-4","route","show","table","all"]))
    for route in routes:
        dst = route.get("dst", "default")
        if dst == "default" or route.get("dev") == "ql-awg0":
            continue
        if ipaddress.IPv4Network(dst, strict=False).overlaps(subnet.network):
            p.error("AWG subnet overlaps an existing host route; choose --address")
    if not UNIT.exists() and subprocess.run(["ip", "link", "show", "ql-awg0"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        p.error("Unmanaged ql-awg0 interface exists")
    active = UNIT.exists() and subprocess.run(["systemctl", "is-active", "--quiet", "quic-lab-awg"]).returncode == 0
    if not active or cfg.get("awg", {}).get("endpoint") != awg["endpoint"]:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
            probe.bind(("0.0.0.0", port))
    cfg["awg"] = awg
    binary = Path("/opt/quic-lab/quic-lab-awg")
    old_binary = binary.read_bytes() if binary.exists() else None
    old_unit = UNIT.read_bytes() if UNIT.exists() else None
    worker_cfg = Path("/etc/quic-lab/awg.json")
    old_worker_cfg = worker_cfg.read_bytes() if worker_cfg.exists() else None
    atomic(Path("/opt/quic-lab/awg-network.py"),Path(__file__).with_name("awg-network.py").read_bytes(),0o755)
    atomic(binary,a.binary.read_bytes(),0o755)
    atomic(UNIT,unit_config())
    atomic(worker_cfg,json.dumps(dict(data_dir=cfg["data_dir"],awg=awg),indent=2)+"\n",0o600)
    atomic(path,json.dumps(cfg,indent=2)+"\n",0o600)
    try:
        run("systemctl","daemon-reload")
        run("systemctl","restart","quic-lab")
        for _ in range(30):
            time.sleep(0.2)
            store = Path(cfg["data_dir"])/"identities.json"
            if store.exists() and json.loads(store.read_text()).get("awg"):
                break
        else:
            raise RuntimeError("Main server did not provision AWG identity")
        run("systemctl","enable","quic-lab-awg")
        started = time.time()
        run("systemctl","restart","quic-lab-awg")
        for _ in range(30):
            time.sleep(0.2)
            status = Path(cfg["data_dir"])/"awg-status.json"
            if status.exists() and status.stat().st_mtime >= started and subprocess.run(["systemctl","is-active","--quiet","quic-lab-awg"]).returncode == 0:
                break
        else:
            raise RuntimeError("AWG did not become healthy; inspect journalctl -u quic-lab-awg")
    except BaseException:
        subprocess.run(["systemctl","stop","quic-lab-awg"])
        atomic(path,previous,0o600)
        if old_binary is not None and old_unit is not None:
            atomic(binary,old_binary,0o755);atomic(UNIT,old_unit)
            if old_worker_cfg is not None: atomic(worker_cfg,old_worker_cfg,0o600)
            run("systemctl","daemon-reload");run("systemctl","restart","quic-lab");run("systemctl","start","quic-lab-awg")
        else:
            subprocess.run(["systemctl","disable","quic-lab-awg"])
            run("systemctl","restart","quic-lab")
        raise
    print(f"AWG ready: UDP {port}, {awg['address']}. Allow this UDP port in the cloud firewall. Dedicated QL_AWG_* firewall rules installed; IPv4 forwarding enabled.")

if __name__ == "__main__":
    main()
