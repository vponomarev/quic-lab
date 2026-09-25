#!/usr/bin/env python3
"""Manage only QUIC Lab's named AWG firewall chains; never flush host rules."""
import argparse
import ipaddress
import json
import subprocess
from pathlib import Path

CHAINS = {"filter": ["QL_AWG_IN", "QL_AWG_FWD"], "nat": ["QL_AWG_NAT"]}
HOOKS = [("filter", "INPUT", "QL_AWG_IN"), ("filter", "FORWARD", "QL_AWG_FWD"), ("nat", "POSTROUTING", "QL_AWG_NAT")]
MARKER = Path("/etc/quic-lab/awg-network-owned")

def run(*args, **kw):
    return subprocess.run(args, check=True, **kw)

def rules(c):
    iface = c["interface"]
    if iface != "ql-awg0":
        raise ValueError("Unexpected AWG interface")
    subnet = str(ipaddress.IPv4Interface(c["address"]).network)
    port = int(c["endpoint"].rsplit(":", 1)[1])
    if not 1 <= port <= 65535:
        raise ValueError("Invalid UDP port")
    allowed = [str(ipaddress.IPv4Network(v)) for v in c["allowed_ips"]]
    lines = ["*filter", ":QL_AWG_IN - [0:0]", ":QL_AWG_FWD - [0:0]", "-F QL_AWG_IN", "-F QL_AWG_FWD",
        f"-A QL_AWG_IN ! -i {iface} -p udp --dport {port} -j ACCEPT",
        f"-A QL_AWG_IN -i {iface} ! -s {subnet} -j DROP",
        *[f"-A QL_AWG_IN -i {iface} -d {cidr} -j ACCEPT" for cidr in allowed],
        f"-A QL_AWG_IN -i {iface} -j DROP",
        f"-A QL_AWG_FWD -i {iface} ! -s {subnet} -j DROP",
        f"-A QL_AWG_FWD -i {iface} -o {iface} -j DROP"]
    lines += [f"-A QL_AWG_FWD -i {iface} -d {cidr} -j ACCEPT" for cidr in allowed]
    lines += [f"-A QL_AWG_FWD -i {iface} -j DROP",
        f"-A QL_AWG_FWD -o {iface} -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
        f"-A QL_AWG_FWD -o {iface} -j DROP", "COMMIT", "*nat", ":QL_AWG_NAT - [0:0]", "-F QL_AWG_NAT",
        f"-A QL_AWG_NAT -s {subnet} ! -o {iface} -j MASQUERADE", "COMMIT"]
    return "\n".join(lines) + "\n"

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("action", choices=["start", "stop"])
    p.add_argument("--config", default="/etc/quic-lab/awg.json")
    a = p.parse_args()
    if a.action == "stop":
        if not MARKER.exists():
            return
        for table, hook, chain in HOOKS:
            while subprocess.run(["iptables", "-w", "-t", table, "-C", hook, "-j", chain], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                run("iptables", "-w", "-t", table, "-D", hook, "-j", chain)
        for table, chains in CHAINS.items():
            for chain in chains:
                if subprocess.run(["iptables", "-w", "-t", table, "-S", chain], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                    run("iptables", "-w", "-t", table, "-F", chain)
                    run("iptables", "-w", "-t", table, "-X", chain)
        return
    cfg = json.loads(Path(a.config).read_text())["awg"]
    content = rules(cfg)
    if not MARKER.exists():
        for table, chains in CHAINS.items():
            for chain in chains:
                if subprocess.run(["iptables", "-w", "-t", table, "-S", chain], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                    raise RuntimeError("Unmanaged AWG chain already exists")
        MARKER.write_text("Managed by quic-lab AWG installer\n")
        MARKER.chmod(0o600)
    run("iptables-restore", "--test", "--noflush", input=content, text=True)
    run("iptables-restore", "--noflush", input=content, text=True)
    for table, hook, chain in HOOKS:
        if subprocess.run(["iptables", "-w", "-t", table, "-C", hook, "-j", chain], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
            run("iptables", "-w", "-t", table, "-I", hook, "1", "-j", chain)
    run("sysctl", "-w", "net.ipv4.ip_forward=1")

if __name__ == "__main__":
    main()
