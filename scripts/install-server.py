#!/usr/bin/env python3
"""Install the bundled QUIC Lab server on Debian/Ubuntu with systemd."""
import argparse
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request

MARKER = "# Managed by quic-lab installer"
CONFIG = Path("/etc/quic-lab")
OPT = Path("/opt/quic-lab")
SITE = Path("/etc/nginx/sites-available/quic-lab")
ENABLED = Path("/etc/nginx/sites-enabled/quic-lab")
UNIT = Path("/etc/systemd/system/quic-lab.service")
HOOK = Path("/etc/letsencrypt/renewal-hooks/deploy/quic-lab")
WEBROOT = "/var/www/quic-lab"


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def atomic(path, data, mode=0o644):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".quic-lab-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data.encode() if isinstance(data, str) else data)
            f.flush()
            os.fsync(f.fileno())
            os.fchmod(f.fileno(), mode)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def domain_name(value):
    value = value.lower().rstrip(".")
    if len(value) > 253 or "." not in value or not all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", s) for s in value.split(".")):
        raise argparse.ArgumentTypeError("Use a DNS hostname, without scheme, port or path (IDN: punycode).")
    try:
        ipaddress.ip_address(value)
    except ValueError:
        return value
    raise argparse.ArgumentTypeError("A DNS hostname is required, not an IP address.")


def cidrs(value):
    try:
        nets = [ipaddress.IPv4Network(s.strip()) for s in value.split(",")]
    except ValueError as e:
        raise argparse.ArgumentTypeError(str(e)) from e
    return ",".join(map(str, nets))


def certificate_path(value):
    # These paths are also rendered into nginx and systemd configuration.
    if not re.fullmatch(r"/[A-Za-z0-9_./-]+", value) or ".." in Path(value).parts:
        raise ValueError("Certificate paths must be absolute, without whitespace or shell metacharacters.")
    return value


DEFAULT_PORTS = dict(echo_quic_port=4433, vpn_quic_port=4434, mtls_port=8443)


def port_number(value):
    try:
        number = int(value)
    except (ValueError, TypeError):
        raise argparse.ArgumentTypeError("Port must be an integer in 1..65535")
    if not 1 <= number <= 65535:
        raise argparse.ArgumentTypeError("Port must be in 1..65535")
    return number


def resolve_frontend(state=None, cfg=None):
    if state:
        mode = state.get("frontend", "nginx")
    elif cfg:
        mode = "nginx"  # legacy installations used nginx
    else:
        mode = "nginx" if shutil.which("nginx") else "direct"
    if mode not in ("nginx", "direct"):
        raise ValueError("frontend must be nginx or direct in install.json")
    return mode


def resolve_ports(args, state=None, cfg=None, frontend="nginx"):
    ports = dict(DEFAULT_PORTS)
    if frontend == "direct":
        ports["mtls_port"] = 443
    # Older installations did not record ports in install.json.
    if cfg:
        for name, section, field in (("echo_quic_port", "echo", "endpoint"), ("vpn_quic_port", "vpn", "quic"), ("mtls_port", "vpn", "https")):
            ports[name] = port_number(cfg[section][field].rsplit(":", 1)[1])
    ports.update((state or {}).get("ports", {}))
    for name in DEFAULT_PORTS:
        if getattr(args, name, None) is not None:
            ports[name] = getattr(args, name)
        ports[name] = port_number(ports[name])
    if ports["echo_quic_port"] == ports["vpn_quic_port"]:
        raise ValueError("Echo QUIC and VPN QUIC must use different UDP ports")
    if ports["mtls_port"] in ((80, 443, 8081, 8082, 8083) if frontend == "nginx" else (80, 8081, 8082, 8083)):
        raise ValueError("mTLS TCP port conflicts with nginx or a loopback backend (80, 443, 8081-8083)")
    return ports


def admin_config(domain, ports=None):
    ports = ports or DEFAULT_PORTS
    return dict(listen="127.0.0.1:8083", public_url=f"https://{domain}/lab/",
                username="admin-" + secrets.token_hex(3), password=secrets.token_urlsafe(24),
                data_dir="/var/lib/quic-lab", apk_path="/var/lib/quic-lab/downloads/quic-lab.apk",
                echo=dict(endpoint=f"{domain}:{ports['echo_quic_port']}", hostname=domain),
                vpn=dict(quic=f"{domain}:{ports['vpn_quic_port']}", https=f"{domain}:{ports['mtls_port']}", hostname=domain, dns="1.1.1.1", mode=0))


def nginx_config(domain, cert=None, key=None):
    http = f"""{MARKER}
server {{
    listen 80;
    listen [::]:80;
    server_name {domain};
    root {WEBROOT};
    location /.well-known/acme-challenge/ {{ try_files $uri =404; }}
    location / {{ return 301 https://{domain}$request_uri; }}
}}
"""
    if not cert:
        return http
    return http + f"""
server {{
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name {domain};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_certificate {cert};
    ssl_certificate_key {key};
    location = / {{ return 302 /lab/; }}
    location = /lab {{ return 302 /lab/; }}
    location /lab/ {{
        proxy_pass http://127.0.0.1:8083/;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 60s;
        proxy_buffering off;
    }}
    location = /echo {{
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 95s;
        proxy_send_timeout 95s;
        proxy_buffering off;
    }}
    location /vpn-demo/ {{
        proxy_pass http://127.0.0.1:8082/;
        proxy_set_header Host $host;
        proxy_buffering off;
    }}
}}
"""


def unit_config(cert, key, ports=None, frontend="nginx"):
    ports = ports or DEFAULT_PORTS
    capability = "AmbientCapabilities=CAP_NET_BIND_SERVICE\n" if frontend == "direct" or min(ports.values()) < 1024 else ""
    public = " -https-listen 0.0.0.0:443" if frontend == "direct" else ""
    return f"""{MARKER}
[Unit]
Description=QUIC Lab echo, mTLS gateway and admin
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
DynamicUser=yes
{capability}WorkingDirectory=/opt/quic-lab
StateDirectory=quic-lab
StateDirectoryMode=0700
UMask=0077
LoadCredential=cert.pem:{cert}
LoadCredential=key.pem:{key}
LoadCredential=admin.json:/etc/quic-lab/admin.json
EnvironmentFile=/etc/quic-lab/server.env
ExecStart=/opt/quic-lab/quic-lab-server -listen 0.0.0.0:{ports["echo_quic_port"]} -web-listen 127.0.0.1:8081 -gateway-quic 0.0.0.0:{ports["vpn_quic_port"]} -gateway-https 0.0.0.0:{ports["mtls_port"]} -gateway-allow ${{GATEWAY_ALLOW}} -demo-listen 127.0.0.1:8082 -cert ${{CREDENTIALS_DIRECTORY}}/cert.pem -key ${{CREDENTIALS_DIRECTORY}}/key.pem -admin-config ${{CREDENTIALS_DIRECTORY}}/admin.json{public}
Restart=on-failure
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
"""


def owned(path):
    if path.is_symlink() or (path.exists() and MARKER not in path.read_text().splitlines()[:2]):
        raise RuntimeError(f"Refusing to overwrite unmanaged file: {path}. See migration instructions.")


def apply_nginx(content):
    previous = SITE.read_bytes() if SITE.exists() else None
    linked = ENABLED.is_symlink()
    atomic(SITE, content)
    if not linked:
        ENABLED.symlink_to(SITE)
    try:
        run("nginx", "-t")
        if subprocess.run(["systemctl", "is-active", "--quiet", "nginx"]).returncode != 0:
            run("systemctl", "enable", "--now", "nginx")
        run("systemctl", "reload", "nginx")
    except BaseException:
        if previous is None:
            SITE.unlink(missing_ok=True)
        else:
            atomic(SITE, previous)
        if not linked:
            ENABLED.unlink(missing_ok=True)
        # A failed reload leaves the previous workers serving traffic.
        raise


def missing_packages(custom_cert, frontend="nginx"):
    # An existing nginx (including an independently installed build) is never
    # passed to apt. Only install missing prerequisites, without upgrades.
    commands = {"openssl": "openssl"}
    if frontend == "nginx":
        commands["nginx"] = "nginx"
    if not custom_cert:
        commands["certbot"] = "certbot"
    packages = [package for command, package in commands.items() if not shutil.which(command)]
    if not Path("/etc/ssl/certs/ca-certificates.crt").is_file():
        packages.append("ca-certificates")
    return packages


def main():
    parser = argparse.ArgumentParser(description=__doc__, epilog="Example: sudo ./install-server.py quic.example.org --vpn-quic-port 443 --mtls-port 9443. Omitted ports retain saved settings on updates.")
    parser.add_argument("domain", type=domain_name)
    parser.add_argument("--email", help="Optional Let's Encrypt account email")
    parser.add_argument("--binary", type=Path, default=Path(__file__).resolve().parent / "quic-lab-server")
    parser.add_argument("--apk", type=Path, help="Optional Android APK to publish")
    parser.add_argument("--gateway-allow", type=cidrs, help="Initial allowed IPv4 CIDRs; default 0.0.0.0/0. Existing policy is preserved.")
    parser.add_argument("--cert", help="Existing server PEM chain; use together with --key")
    parser.add_argument("--key", help="Existing server PEM key; skips certificate issuance")
    parser.add_argument("--echo-quic-port", type=port_number, help="Echo UDP port (initial default: 4433)")
    parser.add_argument("--vpn-quic-port", type=port_number, help="VPN QUIC UDP port (initial default: 4434)")
    parser.add_argument("--mtls-port", type=port_number, help="VPN HTTPS/mTLS TCP port (initial default: 443 without nginx, 8443 with nginx)")
    if len(sys.argv) == 1:
        parser.print_help()
        return
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error("Run with sudo/root.")
    if bool(args.cert) != bool(args.key):
        parser.error("--cert and --key must be used together")
    if not args.binary.is_file():
        parser.error("Bundled binary missing; use a server archive or --binary PATH")
    osinfo = dict(line.split("=", 1) for line in Path("/etc/os-release").read_text().splitlines() if "=" in line)
    distro, version = (osinfo.get(k, "").strip('"') for k in ("ID", "VERSION_ID"))
    if (distro, version) not in {( "debian", "12"), ("debian", "13"), ("ubuntu", "22.04"), ("ubuntu", "24.04")}:
        parser.error("Supported systems: Debian 12/13, Ubuntu 22.04/24.04")
    if not Path("/run/systemd/system").is_dir():
        parser.error("A running systemd is required (not an ordinary Docker container).")
    with open("/run/lock/quic-lab-install.lock", "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        install(args)


def install(args):
    for path in (UNIT, HOOK):
        owned(path)
    state_file = CONFIG / "install.json"
    state = json.loads(state_file.read_text()) if state_file.exists() else None
    if state and state["domain"] != args.domain:
        raise RuntimeError("Changing the domain requires a manual migration; existing configuration was not modified.")
    cert = certificate_path(args.cert or (state or {}).get("cert") or f"/etc/letsencrypt/live/{args.domain}/fullchain.pem")
    key = certificate_path(args.key or (state or {}).get("key") or f"/etc/letsencrypt/live/{args.domain}/privkey.pem")
    custom_cert = bool(args.cert) if args.cert else (state or {}).get("custom_cert", False)
    if custom_cert and (not Path(cert).is_file() or not Path(key).is_file()):
        raise RuntimeError("Existing certificate/key not found")
    if args.apk:
        import zipfile
        with zipfile.ZipFile(args.apk) as archive:
            if "AndroidManifest.xml" not in archive.namelist() or archive.testzip():
                raise RuntimeError("Invalid APK")
    # Verify architecture before changing system files. -h prints flags, not credentials.
    run(str(args.binary.resolve()), "-h", stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    existing = CONFIG / "admin.json"
    old_config = existing.read_bytes() if existing.exists() else None
    cfg = None
    if existing.exists():
        cfg = json.loads(existing.read_text())
        if cfg.get("public_url") != f"https://{args.domain}/lab/" or cfg.get("listen") != "127.0.0.1:8083" or cfg.get("data_dir") != "/var/lib/quic-lab":
            raise RuntimeError("Existing admin.json uses a different layout; migrate manually.")
    frontend = resolve_frontend(state, cfg)
    if frontend == "nginx":
        if not shutil.which("nginx"):
            raise RuntimeError("Saved frontend is nginx, but nginx is missing; restore it before updating")
        owned(SITE)
        if ENABLED.exists() or ENABLED.is_symlink():
            if not ENABLED.is_symlink() or ENABLED.resolve() != SITE:
                raise RuntimeError(f"Unmanaged nginx site: {ENABLED}")
    ports = resolve_ports(args, state, cfg, frontend)
    old_state = state_file.read_bytes() if state_file.exists() else None
    env_file = CONFIG / "server.env"
    if args.gateway_allow and env_file.exists():
        raise RuntimeError("Policy already exists; edit /etc/quic-lab/server.env and restart the service instead.")
    if frontend == "nginx":
        active = run("nginx", "-T", capture_output=True, text=True).stdout
        # Exclude only our dedicated file; reject duplicate exact server names elsewhere.
        for section in re.split(r"(?m)^# configuration file ", active):
            if section.startswith(str(SITE) + ":") or section.startswith(str(ENABLED) + ":"):
                continue
            for names in re.findall(r"(?m)^\s*server_name\s+([^;]+);", section):
                if args.domain in [name.strip("\"'").lower() for name in names.split()]:
                    raise RuntimeError("This domain already has an nginx site; see migration instructions.")
    packages = missing_packages(custom_cert, frontend)
    if packages:
        run("apt-get", "update")
        run("apt-get", "install", "--no-upgrade", "-y", *packages,
            env={**os.environ, "DEBIAN_FRONTEND": "noninteractive", "NEEDRESTART_MODE": "l"})
    CONFIG.mkdir(mode=0o700, parents=True, exist_ok=True)
    CONFIG.chmod(0o700)
    OPT.mkdir(mode=0o755, parents=True, exist_ok=True)
    if not existing.exists():
        cfg = admin_config(args.domain, ports)
        atomic(existing, json.dumps(cfg, indent=2) + "\n", 0o600)
        credentials = f"URL: {cfg['public_url']}login\nLogin: {cfg['username']}\nPassword: {cfg['password']}\n"
        atomic(CONFIG / "admin-credentials.txt", credentials, 0o600)
        print("New administrator credentials (saved in /etc/quic-lab/admin-credentials.txt):\n" + credentials, flush=True)
    else:
        print("Existing admin configuration and password preserved.", flush=True)
    if not env_file.exists():
        atomic(env_file, f"GATEWAY_ALLOW={args.gateway_allow or '0.0.0.0/0'}\n", 0o600)
    if old_config is None:
        # Keep initial mode after a failed ACME attempt, even though admin.json
        # already exists when the user retries installation.
        atomic(state_file, json.dumps(dict(domain=args.domain, cert=cert, key=key, custom_cert=custom_cert, ports=ports, frontend=frontend), indent=2) + "\n", 0o600)
    if frontend == "nginx":
        Path(WEBROOT).mkdir(mode=0o755, parents=True, exist_ok=True)
    if not custom_cert:
        if not Path(cert).exists() or not Path(key).exists():
            if frontend == "nginx":
                apply_nginx(nginx_config(args.domain))
            print("Obtaining Let's Encrypt certificate; installation accepts the ACME subscriber agreement.", flush=True)
            email = ["--email", args.email] if args.email else ["--register-unsafely-without-email"]
            method = ["--webroot", "-w", WEBROOT] if frontend == "nginx" else ["--standalone", "--preferred-challenges", "http"]
            run("certbot", "certonly", *method, "--cert-name", args.domain, "-d", args.domain,
                "--non-interactive", "--agree-tos", *email)
        reload_nginx = "nginx -t\nsystemctl reload nginx\n" if frontend == "nginx" else ""
        atomic(HOOK, f"""#!/bin/sh
{MARKER}
set -eu
[ "${{RENEWED_LINEAGE:-}}" = "/etc/letsencrypt/live/{args.domain}" ] || exit 0
{reload_nginx}if systemctl is-active --quiet quic-lab; then systemctl restart quic-lab; fi
""", 0o755)
        run("systemctl", "enable", "--now", "certbot.timer")
    run("openssl", "x509", "-in", cert, "-noout", "-checkhost", args.domain)
    run("openssl", "x509", "-in", cert, "-noout", "-checkend", "0")
    # Validate the pair before replacing the binary/unit, also without nginx.
    import ssl
    ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER).load_cert_chain(cert, key)
    if frontend == "nginx":
        apply_nginx(nginx_config(args.domain, cert, key))
    binary = OPT / "quic-lab-server"
    old_binary = binary.read_bytes() if binary.exists() else None
    old_unit = UNIT.read_bytes() if UNIT.exists() else None
    if old_binary is not None:
        atomic(OPT / "quic-lab-server.previous", old_binary, 0o755)
    atomic(binary, args.binary.read_bytes(), 0o755)
    atomic(UNIT, unit_config(cert, key, ports, frontend))
    try:
        if old_config is not None:
            for name, section, field in (("echo_quic_port", "echo", "endpoint"), ("vpn_quic_port", "vpn", "quic"), ("mtls_port", "vpn", "https")):
                # Keep custom hostnames, account, routing and all other settings.
                host = cfg[section][field].rsplit(":", 1)[0]
                cfg[section][field] = f"{host}:{ports[name]}"
            atomic(existing, json.dumps(cfg, indent=2) + "\n", 0o600)
        atomic(state_file, json.dumps(dict(domain=args.domain, cert=cert, key=key, custom_cert=custom_cert, ports=ports, frontend=frontend), indent=2) + "\n", 0o600)
        run("systemctl", "daemon-reload")
        run("systemctl", "enable", "quic-lab")
        run("systemctl", "restart", "quic-lab")
        ready = False
        for _ in range(20):
            time.sleep(0.5)
            try:
                with urllib.request.urlopen("http://127.0.0.1:8083/", timeout=2) as response:
                    ready = response.status == 200
                ready = ready and subprocess.run(["systemctl", "is-active", "--quiet", "quic-lab"]).returncode == 0
                if ready:
                    break
            except OSError:
                pass
        if not ready:
            raise RuntimeError("Server did not become healthy; inspect journalctl -u quic-lab")
    except BaseException:
        run("systemctl", "stop", "quic-lab")
        if old_config is not None:
            atomic(existing, old_config, 0o600)
        if old_state is not None:
            atomic(state_file, old_state, 0o600)
        elif old_config is not None:
            state_file.unlink(missing_ok=True)
        if old_binary is not None and old_unit is not None:
            atomic(binary, old_binary, 0o755)
            atomic(UNIT, old_unit)
            run("systemctl", "daemon-reload")
            run("systemctl", "start", "quic-lab")
            print("Previous server binary and unit restored.", file=sys.stderr)
        raise
    if args.apk:
        # StateDirectory belongs to DynamicUser; preserve its ownership.
        downloads = Path("/var/lib/quic-lab/downloads")
        downloads.mkdir(mode=0o755, exist_ok=True)
        atomic(downloads / "quic-lab.apk", args.apk.read_bytes())
    tcp_ports = ",".join(str(p) for p in sorted({80, 443, ports["mtls_port"]}))
    print(f"Frontend: {frontend}\nReady: https://{args.domain}/lab/\nConfig: /etc/quic-lab/admin.json\n"
          "Credentials: /etc/quic-lab/admin-credentials.txt (first installation)\n"
          f"Allow inbound TCP {tcp_ports} and UDP {ports['echo_quic_port']},{ports['vpn_quic_port']} in host/cloud firewalls.\n"
          "Firewall rules were not changed. Updates restart active sessions.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Installation failed: {error}", file=sys.stderr)
        sys.exit(1)
