#!/usr/bin/env python3
"""Firewall lifecycle check. Run only in a disposable Linux container with NET_ADMIN."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
spec=importlib.util.spec_from_file_location("network",Path(__file__).with_name("awg-network.py"))
n=importlib.util.module_from_spec(spec);spec.loader.exec_module(n)
real_run=n.run
# Docker makes /proc/sys read-only. Test forwarding through Docker --sysctl;
# verify the helper's request without attempting to change that mount.
def run(*args,**kw):
    if args[0]=="sysctl":
        assert args==("sysctl","-w","net.ipv4.ip_forward=1")
        assert Path("/proc/sys/net/ipv4/ip_forward").read_text().strip()=="1"
        return
    return real_run(*args,**kw)
n.run=run
with tempfile.TemporaryDirectory() as tmp:
    n.MARKER=Path(tmp)/"owned"
    config=Path(tmp)/"awg.json"
    config.write_text(json.dumps({"awg":{"endpoint":"vpn.example.org:51820","address":"10.77.0.1/24","allowed_ips":["0.0.0.0/0"],"interface":"ql-awg0"}}))
    real_run("iptables","-N","UNRELATED_SERVICE")
    real_run("iptables","-A","UNRELATED_SERVICE","-s","192.0.2.1","-j","RETURN")
    before=subprocess.check_output(["iptables","-S","UNRELATED_SERVICE"])
    for _ in range(2):
        sys.argv=["awg-network.py","start","--config",str(config)];n.main()
    rules=subprocess.check_output(["iptables","-S"]).decode()
    assert rules.count("-A FORWARD -j QL_AWG_FWD")==1
    assert subprocess.check_output(["iptables","-S","UNRELATED_SERVICE"])==before
    sys.argv=["awg-network.py","stop"];n.main();n.main()
    assert "QL_AWG" not in subprocess.check_output(["iptables","-S"]).decode()
    assert subprocess.check_output(["iptables","-S","UNRELATED_SERVICE"])==before
    print("AWG firewall start/restart/stop preserves unrelated rules")
