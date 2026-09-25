#!/usr/bin/env python3
"""Dedicated fail-closed AWG transit routing. Stop is explicit, never service teardown."""
import argparse
import ipaddress
import json
from pathlib import Path
import subprocess
TABLE="51821"
MARKER=Path("/etc/quic-lab/transit-network-owned")
HOOKS=[("filter","FORWARD","QL_TR_FWD"),("nat","PREROUTING","QL_TR_PRE"),("nat","POSTROUTING","QL_TR_NAT")]
def run(*args,**kwargs):return subprocess.run(args,check=True,**kwargs)
def rules(cfg):
    uplink=cfg["transit"]; source=str(ipaddress.IPv4Address(uplink["source_ip"]))
    subnet=cfg.get("ingress_subnet")
    if subnet:subnet=str(ipaddress.IPv4Network(subnet))
    endpoint=str(ipaddress.IPv4Address(uplink["endpoint"].rsplit(":",1)[0]))
    lines=["*filter",":QL_TR_FWD - [0:0]","-F QL_TR_FWD"]
    if subnet:lines += ["-A QL_TR_FWD -i ql-awg0 ! -o ql-exit0 -j DROP"]
    lines += ["COMMIT","*nat",":QL_TR_PRE - [0:0]",":QL_TR_NAT - [0:0]","-F QL_TR_PRE","-F QL_TR_NAT"]
    if subnet:
        lines += [f"-A QL_TR_NAT -s {subnet} -o ql-exit0 -j SNAT --to-source {source}"]
    lines += [f"-A QL_TR_NAT -s {source}/32 -o ql-exit0 -j SNAT --to-source {source}","COMMIT"]
    return "\n".join(lines)+"\n"
def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument("action",choices=["start","stop"]);p.add_argument("--config",default="/etc/quic-lab/transit.json");a=p.parse_args()
    cfg=json.loads(Path(a.config).read_text());sources=[str(ipaddress.IPv4Address(cfg["transit"]["source_ip"]))+"/32"]
    if cfg.get("ingress_subnet"):sources.append(str(ipaddress.IPv4Network(cfg["ingress_subnet"])))
    if a.action=="stop":
        if not MARKER.exists():return
        for source in sources:
            subprocess.run(["ip","-4","rule","del","pref","0","from",source,"lookup",TABLE],stderr=subprocess.DEVNULL)
        for table,hook,chain in HOOKS:
            if subprocess.run(["iptables","-w","-t",table,"-C",hook,"-j",chain],stderr=subprocess.DEVNULL).returncode==0:run("iptables","-w","-t",table,"-D",hook,"-j",chain)
            subprocess.run(["iptables","-w","-t",table,"-F",chain],stderr=subprocess.DEVNULL);subprocess.run(["iptables","-w","-t",table,"-X",chain],stderr=subprocess.DEVNULL)
        run("ip","-4","route","flush","table",TABLE)
        original=json.loads(MARKER.read_text())["local_protocol"]
        current=json.loads(subprocess.check_output(["ip","-j","-d","-4","rule","show"]))
        local=next(r for r in current if r.get("priority")==0 and r.get("table")=="local")
        if local.get("protocol","kernel")!=original:
            run("ip","-4","rule","add","pref","0","from","all","lookup","local","protocol",original)
            run("ip","-4","rule","del","pref","0","from","all","lookup","local","protocol",local["protocol"])
        MARKER.unlink();return
    current=json.loads(subprocess.check_output(["ip","-j","-d","-4","rule","show"]))
    if not MARKER.exists():
        if any(r.get("priority")==0 and (r.get("table")!="local" or r.get("src")!="all") for r in current):raise RuntimeError("Custom priority-0 rules exist; configure transit routing manually")
        existing=subprocess.run(["ip","-4","route","show","table",TABLE],capture_output=True,text=True)
        if existing.stdout.strip():raise RuntimeError("Transit route table is already in use")
        for table,hook,chain in HOOKS:
            if subprocess.run(["iptables","-w","-t",table,"-S",chain],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:raise RuntimeError("Unmanaged transit firewall chain exists")
        MARKER.write_text(json.dumps({"sources":sources,"local_protocol":next(r.get("protocol","kernel") for r in current if r.get("priority")==0 and r.get("table")=="local")}));MARKER.chmod(0o600)
    elif json.loads(MARKER.read_text())["sources"]!=sources:raise RuntimeError("Disable transit before changing its source/subnet")
    run("ip","-4","route","replace","blackhole","default","table",TABLE,"metric","32767")
    existing=json.loads(subprocess.check_output(["ip","-j","-4","route","show","table",TABLE]))
    wanted={str(ipaddress.IPv4Address(ip)) for ip in cfg.get("front_ips",[])}
    for route in existing:
        if route.get("type")=="local" and route.get("dst","").removesuffix("/32") not in wanted:
            run("ip","-4","route","del","local",route["dst"],"table",TABLE)
    # Local VPN gateway remains directly reachable for its own RTT/control plane.
    for address in cfg.get("front_ips",[]):
        address=str(ipaddress.IPv4Address(address))
        run("ip","-4","route","replace","local",address+"/32","dev","lo","table",TABLE)
    for source in sources:
        ip=source.removesuffix("/32")
        if not any(str(r.get("table"))==TABLE and (r.get("src")==source or (r.get("src")==source.split("/")[0] and int(r.get("srclen",32))==int(source.split("/")[1]))) for r in current):run("ip","-4","rule","add","pref","0","from",source,"lookup",TABLE)
    # Reinsert the standard local lookup AFTER our source-only rules. All other
    # traffic retains local-before-main behavior. No interval without local lookup.
    ordered=json.loads(subprocess.check_output(["ip","-j","-d","-4","rule","show"]))
    local_index=next(j for j,r in enumerate(ordered) if r.get("priority")==0 and r.get("src")=="all" and r.get("table")=="local")
    last_owned=max(j for j,r in enumerate(ordered) if str(r.get("table"))==TABLE)
    if local_index<last_owned:
        old=ordered[local_index].get("protocol","kernel")
        new="static" if old!="static" else "boot"
        run("ip","-4","rule","add","pref","0","from","all","lookup","local","protocol",new)
        run("ip","-4","rule","del","pref","0","from","all","lookup","local","protocol",old)
    content=rules(cfg)
    run("iptables-restore","--test","--noflush",input=content,text=True)
    run("iptables-restore","--noflush",input=content,text=True)
    for table,hook,chain in HOOKS:
        if subprocess.run(["iptables","-w","-t",table,"-C",hook,"-j",chain],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:run("iptables","-w","-t",table,"-I",hook,"1","-j",chain)
    run("sysctl","-w","net.ipv4.ip_forward=1")
if __name__=="__main__":main()
