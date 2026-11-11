#!/usr/bin/python3
"""Laptop-specific, reversible DNS/IPv6 setup for the installed systemd service."""
import ipaddress, json, os, pathlib, subprocess, sys
path = pathlib.Path('/run/systemd/resolved.conf.d/90-quic-lab-client.conf')
def run(*args): subprocess.run(args, check=True)

def restart_resolver():
    # Explicit VPN switches may exceed the resolver's normal restart burst.
    run('systemctl','reset-failed','systemd-resolved')
    run('systemctl','restart','systemd-resolved')
if sys.argv[1:] == ['start']:
    cfg = json.loads(pathlib.Path('/etc/quic-lab/client.json').read_text())
    dns = ipaddress.IPv4Address(cfg.get('dns') or '1.1.1.1')
    covered = any(dns in ipaddress.IPv4Network(net) for net in cfg['targets'])
    if covered:
        with path.open('x') as out:
            out.write('[Resolve]\nDNS='+str(dns)+'\nFallbackDNS=\nDomains=~.\n')
        os.chmod(path,0o644)
        restart_resolver()
    run('sysctl','-q','-w','net.ipv6.conf.all.disable_ipv6=1','net.ipv6.conf.default.disable_ipv6=1')
elif sys.argv[1:] == ['stop']:
    # These two original values were inspected before installation on this laptop.
    run('sysctl','-q','-w','net.ipv6.conf.all.disable_ipv6=0','net.ipv6.conf.default.disable_ipv6=0')
    if path.exists():
        path.unlink()
        restart_resolver()
else:
    raise SystemExit('Use start or stop')
