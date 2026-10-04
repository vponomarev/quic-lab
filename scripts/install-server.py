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
import socket
import stat
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request

MARKER = "# Managed by quic-lab installer"
CONFIG = Path("/etc/quic-lab")
OPT = Path("/opt/quic-lab")
DATA = Path("/var/lib/quic-lab")
SITE = Path("/etc/nginx/sites-available/quic-lab")
ENABLED = Path("/etc/nginx/sites-enabled/quic-lab")
UNIT = Path("/etc/systemd/system/quic-lab.service")
VLESS_UNIT = Path("/etc/systemd/system/quic-lab-vless.service")
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


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def snapshot_identities(data, backup):
    # StateDirectory may itself be a systemd symlink; never follow file symlinks.
    manifest = {}
    for name in ("identities.json", "identities.json.bak"):
        source = data / name
        try:
            metadata = source.lstat()
        except FileNotFoundError:
            manifest[name] = None
            continue
        if not stat.S_ISREG(metadata.st_mode):
            raise RuntimeError(f"Refusing nonregular identity file: {source}")
        atomic(backup / name, source.read_bytes(), 0o600)
        manifest[name] = dict(uid=metadata.st_uid, gid=metadata.st_gid,
                              mode=stat.S_IMODE(metadata.st_mode))
    # Only a completed snapshot is eligible for automatic or operator rollback.
    atomic(backup / "state-manifest.json", json.dumps(manifest, indent=2) + "\n", 0o600)
    # Persist payload/manifest entries, then the newly created backup directory.
    sync_directory(backup)
    sync_directory(backup.parent)


def restore_identities(data, backup):
    manifest = json.loads((backup / "state-manifest.json").read_text())
    for name in ("identities.json", "identities.json.bak"):
        destination = data / name
        if destination.is_symlink() or (destination.exists() and not destination.is_file()):
            raise RuntimeError(f"Refusing nonregular identity file: {destination}")
        metadata = manifest[name]
        if metadata is None:
            destination.unlink(missing_ok=True)
        else:
            atomic(destination, (backup / name).read_bytes(), 0o600)
            os.chown(destination, metadata["uid"], metadata["gid"])
            destination.chmod(metadata["mode"])
    sync_directory(data)


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
UNIFIED_PORTS = dict(echo_quic_port=443, vpn_quic_port=443, mtls_port=443)


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
        mode = "direct"  # fresh installations use the Go-owned unified frontend
    if mode not in ("nginx", "direct"):
        raise ValueError("frontend must be nginx or direct in install.json")
    return mode


def resolve_ingress(args, state=None, cfg=None, frontend="direct"):
    requested = getattr(args, "ingress", None)
    migrate = getattr(args, "migrate_unified", False)
    if migrate:
        if requested == "split" or getattr(args, "frontend", None) == "nginx":
            raise ValueError("--migrate-unified requires unified ingress with the direct frontend")
        return "unified"
    existing = bool(state or cfg)
    saved = (state or {}).get("ingress")
    if saved not in (None, "unified", "split"):
        raise ValueError("Invalid saved ingress mode")
    if saved is None and existing:
        p = (state or {}).get("ports", {})
        if cfg:
            p = {name: port_number(cfg[section][field].rsplit(":", 1)[1]) for name, section, field in
                 (("echo_quic_port", "echo", "endpoint"), ("vpn_quic_port", "vpn", "quic"), ("mtls_port", "vpn", "https"))} | p
        saved = "unified" if frontend == "direct" and p == UNIFIED_PORTS else "split"
    if existing and requested == "unified" and saved != "unified":
        raise ValueError("Existing split installation requires --migrate-unified")
    # Explicit custom ports retain the traditional split configuration.
    custom_split = any(getattr(args, name, None) is not None and getattr(args, name) != 443 for name in DEFAULT_PORTS)
    mode = requested or saved or ("split" if frontend == "nginx" or custom_split else "unified")
    if mode == "unified" and frontend != "direct":
        raise ValueError("Unified ingress requires --frontend direct; existing nginx is never moved automatically")
    return mode


def resolve_ports(args, state=None, cfg=None, frontend="nginx"):
    mode = resolve_ingress(args, state, cfg, frontend)
    change_layout = getattr(args, "change_layout", False) or getattr(args, "migrate_unified", False) or (getattr(args, "ingress", None) == "split" and (state or {}).get("ingress") == "unified")
    ports = dict(UNIFIED_PORTS if mode == "unified" else DEFAULT_PORTS)
    if frontend == "direct" and mode == "split" and getattr(args, "ingress", None) != "split":
        ports["mtls_port"] = 443
    if cfg and not change_layout:
        for name, section, field in (("echo_quic_port", "echo", "endpoint"), ("vpn_quic_port", "vpn", "quic"), ("mtls_port", "vpn", "https")):
            ports[name] = port_number(cfg[section][field].rsplit(":", 1)[1])
    if not change_layout:
        ports.update((state or {}).get("ports", {}))
    for name in DEFAULT_PORTS:
        if getattr(args, name, None) is not None:
            ports[name] = getattr(args, name)
        ports[name] = port_number(ports[name])
    if mode == "unified" and ports != UNIFIED_PORTS:
        raise ValueError("Unified ingress uses TCP/443 and UDP/443; choose --ingress split for custom ports")
    if mode == "split" and ports["echo_quic_port"] == ports["vpn_quic_port"]:
        raise ValueError("Split ingress requires different echo and VPN UDP ports")
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


def nginx_config(domain, cert=None, key=None, names=None):
    challenge_names = ' '.join(names or [domain])
    http = f"""{MARKER}
server {{
    listen 80;
    listen [::]:80;
    server_name {challenge_names};
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
    server_name {challenge_names};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_certificate {cert};
    ssl_certificate_key {key};
    location = / {{ return 302 /lab/; }}
    location = /lab {{ return 302 /lab/; }}
    location = /api/v1/capabilities {{
        proxy_pass http://127.0.0.1:8083/api/v1/capabilities;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_buffering off;
    }}
    location ^~ /api/v1/devices/ {{
        proxy_pass http://127.0.0.1:8083;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_buffering off;
    }}
    location /lab/ {{
        proxy_pass http://127.0.0.1:8083/;
        proxy_set_header X-Quic-Lab-Peer "$remote_addr:$remote_port";
        proxy_set_header X-Portal-TLS-Version $ssl_protocol;
        proxy_set_header X-Portal-TLS-Cipher $ssl_cipher;
        proxy_set_header X-Portal-TLS-ALPN $ssl_alpn_protocol;
        proxy_set_header X-Portal-TLS-SNI $ssl_server_name;
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


def server_config(ports, frontend, domain, allow="0.0.0.0/0", previous=None, args=None):
    if previous is not None:
        result = dict(previous)
    else:
        result = dict(listen=f"0.0.0.0:{ports['echo_quic_port']}",
                      web_listen="127.0.0.1:8081", demo_listen="127.0.0.1:8082",
                      gateway_quic=f"0.0.0.0:{ports['vpn_quic_port']}",
                      gateway_https=f"0.0.0.0:{ports['mtls_port']}", gateway_allow=allow,
                      cert="${CREDENTIALS_DIRECTORY}/cert.pem", key="${CREDENTIALS_DIRECTORY}/key.pem",
                      admin_config="${CREDENTIALS_DIRECTORY}/admin.json",
                      capabilities=dict(control_version=1, data_version=1, min_android_version_code=0),
                      https_listen="0.0.0.0:443" if frontend == "direct" else "",
                      tls_host=domain if frontend == "direct" else "", tls_fallback="")
    for option, field in (("echo_quic_port", "listen"), ("vpn_quic_port", "gateway_quic"), ("mtls_port", "gateway_https")):
        if args and (getattr(args, option, None) is not None or getattr(args, "migrate_unified", False) or getattr(args, "change_layout", False)):
            host = result.get(field, "0.0.0.0:0").rsplit(":", 1)[0]
            result[field] = f"{host}:{ports[option]}"
    if args and (getattr(args, "frontend", None) is not None or getattr(args, "migrate_unified", False)):
        result["https_listen"] = "0.0.0.0:443" if frontend == "direct" else ""
        if frontend == "direct": result["tls_host"] = domain
    if args and getattr(args, "tls_fallback", None) is not None:
        result["tls_fallback"] = args.tls_fallback
        result["tls_host"] = domain
    if args and getattr(args, "migrate_unified", False):
        result["gateway_quic"] = result["listen"]
        result["gateway_https"] = result["https_listen"]
    return result


def legacy_policy(path):
    import shlex
    if path.exists():
        for line in path.read_text().splitlines():
            if line.startswith("GATEWAY_ALLOW="):
                return cidrs(shlex.split(line.split("=", 1)[1])[0])
    return "0.0.0.0/0"


def unit_config(cert, key, ports=None, frontend="nginx"):
    ports = ports or DEFAULT_PORTS
    capability = "AmbientCapabilities=CAP_NET_BIND_SERVICE\n" if frontend == "direct" or min(ports.values()) < 1024 else ""
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
LoadCredential=server.json:/etc/quic-lab/server.json
ExecStart=/opt/quic-lab/quic-lab-server -config ${{CREDENTIALS_DIRECTORY}}/server.json
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
"""


def vless_settings(args, cfg, data, ports, frontend, domain):
    """UI settings remain authoritative except for explicit offline migration."""
    initial = (cfg or {}).get("vless")
    saved = {}
    identity = data / "identities.json"
    if identity.is_symlink():
        raise RuntimeError("Refusing symlinked identity state")
    if identity.is_file():
        try:
            saved = json.loads(identity.read_text())
        except (ValueError, UnicodeError):
            if initial or getattr(args, "enable_vless", False) or (data / "vless").exists():
                raise RuntimeError("Cannot inspect durable VLESS settings; repair identity state first") from None
        if not isinstance(saved, dict):
            saved = {}
    if saved.get("vless_pending"):
        raise RuntimeError("Pending VLESS settings recovery; start the current server to recover before upgrading")
    managed = bool(initial or saved.get("vless") or saved.get("vless_revision") or
                   (data / "vless").exists() or getattr(args, "enable_vless", False))
    durable = bool(saved.get("vless_revision") or "vless" in saved)
    desired = saved.get("vless") if durable else initial
    args.vless_previous = json.loads(json.dumps(saved.get("vless")))
    unified = getattr(args, "migrate_unified", False) or getattr(args, "ingress", None) == "unified"
    provision = not desired and not durable and getattr(args, "enable_vless", False)
    if provision:
        occupied = {80, 443, 8081, 8082, 8083, ports["mtls_port"]}
        chosen = getattr(args, "vless_port", None)
        choices = (9444, 10444, 11444) if unified else (8443, 9443, 10443)
        port = port_number(chosen if chosen is not None else next(p for p in choices if p not in occupied))
        desired = dict(listen=f"0.0.0.0:{port}", endpoint=f"{domain}:{port}", security="tls",
                       server_name=domain, fingerprint="chrome", flow="", mode="standalone",
                       tls_certificate_file="/run/credentials/quic-lab-vless.service/cert.pem",
                       tls_key_file="/run/credentials/quic-lab-vless.service/key.pem")
    migrate = getattr(args, "migrate_unified", False)
    if desired and (provision or migrate):
        desired = dict(desired)
        requested_name = getattr(args, "vless_server_name", None)
        if unified:
            name = requested_name or (desired.get("server_name") if desired.get("server_name") != domain else "vless." + domain)
            name = domain_name(name)
            if desired.get("security") == "reality" and name not in desired.get("reality_server_names", []):
                raise ValueError("Migration SNI is not allowed by existing REALITY authentication; retain its allowed name")
            port = port_number(getattr(args, "vless_port", None) or 9444)
            desired.update(listen=f"127.0.0.1:{port}", endpoint=f"{name}:443", server_name=name, accept_proxy_protocol=True)
        elif requested_name:
            desired["server_name"] = requested_name
            desired["endpoint"] = f"{requested_name}:{port_number(desired['listen'].rsplit(':', 1)[1])}"
    elif desired:
        if getattr(args, "vless_port", None) is not None and port_number(desired["listen"].rsplit(":", 1)[1]) != args.vless_port:
            raise ValueError("VLESS already configured; use --migrate-unified or change its listener in the administrator UI")
        if getattr(args, "vless_server_name", None) and args.vless_server_name != desired.get("server_name"):
            raise ValueError("VLESS already configured; changing SNI requires --migrate-unified or the administrator UI")
    if desired:
        port = port_number(desired["listen"].rsplit(":", 1)[1])
        if port in {80, 443, 8081, 8082, 8083, ports["mtls_port"]}:
            raise ValueError("VLESS TCP listener conflicts with the frontend or another backend")
    return desired, managed


def configure_vless_route(server, config, previous=None):
    names = config.get("reality_server_names", []) if config.get("security") == "reality" else [config["server_name"]]
    names = [domain_name(n) for n in names]
    local = {n.lower().rstrip('.') for n in [server.get("tls_host", "")] + server.get("vpn_sni_names", [])}
    if not names or len(names) != len(set(names)) or any(n in local for n in names):
        raise ValueError("VLESS SNI collides with a local HTTPS/VPN name")
    host = config["listen"].rsplit(":", 1)[0]
    try: private = ipaddress.ip_address(host).is_loopback
    except ValueError: private = False
    if not private or not config.get("accept_proxy_protocol"):
        raise ValueError("Routed VLESS requires a numeric loopback listener with PROXY protocol enabled")
    if config["listen"] == server.get("tls_fallback"):
        raise ValueError("VLESS backend conflicts with the TLS fallback")
    old = previous or config
    old_names = old.get("reality_server_names", []) if old.get("security") == "reality" else [old["server_name"]]
    expected = {n.lower().rstrip('.') for n in old_names}
    legacy = {old["server_name"].lower().rstrip('.')}
    routes = []; removed = False
    for route in server.get("tls_routes", []):
        route_names = {n.lower().rstrip('.') for n in route.get("server_names", [])}
        if route.get("target") == old.get("listen") and old.get("accept_proxy_protocol"):
            if removed or not route.get("proxy_protocol") or route_names not in (expected, legacy):
                raise ValueError("Ambiguous managed VLESS route; preserve and reconcile manual settings")
            removed = True
            continue
        if route_names.intersection(names):
            raise ValueError("VLESS SNI conflicts with an existing manual TLS route")
        routes.append(route)
    routes.append(dict(server_names=names, target=config["listen"], proxy_protocol=True))
    server["tls_routes"] = routes


def migrate_vless_identity(data, desired, expected=None):
    """Called only after both services stop and the identity snapshot is durable."""
    path = data / "identities.json"
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return
    if not stat.S_ISREG(metadata.st_mode):
        raise RuntimeError("Refusing nonregular identity state during VLESS migration")
    saved = json.loads(path.read_text())
    if not isinstance(saved, dict):
        raise RuntimeError("Invalid identity state during VLESS migration")
    if expected is not None and saved.get("vless") != expected['config']:
        raise RuntimeError('VLESS settings changed during installation preflight; retry migration with the current configuration')
    if saved.get("vless") == desired:
        return
    # A durable disable must never be reversed by installer provisioning.
    if (saved.get("vless_revision") or "vless" in saved) and not saved.get("vless"):
        if desired is not None:
            raise RuntimeError("Saved VLESS disable is authoritative")
        return
    saved["vless"] = desired
    saved.pop("vless_digest", None)  # Go reconciliation advances the current revision.
    atomic(path, json.dumps(saved, indent=2) + "\n", stat.S_IMODE(metadata.st_mode))
    os.chown(path, metadata.st_uid, metadata.st_gid)
    sync_directory(data)


def vless_applied(binary, data):
    """Read the backend's desired revision and ask its private worker for an ACK."""
    try:
        state = json.loads((data / "identities.json").read_text())
        revision = state.get("vless_revision")
        if not isinstance(revision, int) or isinstance(revision, bool) or revision <= 0:
            return False
        result = subprocess.run(["systemd-run", "--quiet", "--pipe", "--wait", "--collect",
                                 "--property=User=quic-lab", "--property=DynamicUser=yes",
                                 "--property=StateDirectory=quic-lab", "--property=StateDirectoryMode=0700",
                                 "--property=NoNewPrivileges=yes", "--property=ProtectSystem=strict",
                                 "--property=ProtectHome=yes", "--", str(binary),
                                 "-data-dir", str(data / "vless"), "-check-applied",
                                 "-revision", str(revision)],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        return result.returncode == 0
    except (OSError, ValueError, subprocess.TimeoutExpired):
        return False


def validate_vless(binary, config):
    if not binary.is_file():
        raise RuntimeError("Bundled VLESS worker missing; use a current server archive or --vless-binary PATH")
    try:
        run(str(binary.resolve()), "-h", stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if config:
            with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as pending:
                json.dump(config, pending); pending.flush()
                run(str(binary.resolve()), "-check-config", pending.name,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError:
        raise RuntimeError("VLESS worker/configuration preflight failed; existing installation unchanged") from None


def vless_unit_config(cert, key, config):
    port = port_number(config["listen"].rsplit(":", 1)[1]) if config else 8443
    capability = "AmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE\n" if port < 1024 else "CapabilityBoundingSet=\n"
    return f"""{MARKER}
[Unit]
Description=QUIC Lab managed VLESS transport
After=network-online.target quic-lab.service
Wants=network-online.target quic-lab.service

[Service]
Type=simple
User=quic-lab
DynamicUser=yes
StateDirectory=quic-lab
StateDirectoryMode=0700
UMask=0077
{capability}LoadCredential=cert.pem:{cert}
LoadCredential=key.pem:{key}
ExecStartPre=/usr/bin/install -d -m 0700 /var/lib/quic-lab/vless
ExecStart=/opt/quic-lab/quic-lab-vless -data-dir /var/lib/quic-lab/vless -admission-dir /var/lib/quic-lab
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
    commands = {"openssl": "openssl", "kill": "procps"}
    if frontend == "nginx":
        commands["nginx"] = "nginx"
    if not custom_cert:
        commands["certbot"] = "certbot"
    packages = [package for command, package in commands.items() if not shutil.which(command)]
    if not Path("/etc/ssl/certs/ca-certificates.crt").is_file():
        packages.append("ca-certificates")
    return packages


def preflight_direct_listener(previous, old_frontend):
    # An already configured direct listener is released by stopping our own unit.
    if old_frontend == "direct" and previous and any(
            previous.get(field, "").rsplit(":", 1)[-1] == "443"
            for field in ("https_listen", "gateway_https")):
        return
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as public:
            public.bind(("0.0.0.0", 443))
    except OSError:
        raise RuntimeError("Direct ingress requires free TCP/443. Move the existing frontend to a private backend manually before --migrate-unified; unrelated nginx services are never changed") from None


def certificate_usable(cert):
    try:
        result = subprocess.run(["openssl", "x509", "-in", str(cert), "-noout", "-checkend", "0"],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        return result.returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def certificate_names(cert):
    result = run("openssl", "x509", "-in", str(cert), "-noout", "-ext", "subjectAltName", capture_output=True, text=True)
    return {name.lower().rstrip(".") for name in re.findall(r"DNS:([^,\s]+)", result.stdout)}


def certificate_covers(names, domain):
    if domain in names:
        return True
    return any(name.startswith("*.") and domain.endswith(name[1:]) and domain.count(".") == name.count(".") for name in names)


def validate_certificate_names(cert, required):
    names = certificate_names(cert)
    if any(not certificate_covers(names, name) for name in required):
        raise RuntimeError("Certificate SAN does not cover all configured TLS names; provide a SAN certificate or use --acme")


def validate_acme_dns(names):
    common = None
    for name in names:
        try:
            addresses = {entry[4][0] for entry in socket.getaddrinfo(name, 80, family=socket.AF_INET, type=socket.SOCK_STREAM)}
        except socket.gaierror:
            raise RuntimeError(f"ACME name has no usable IPv4 DNS record: {name}") from None
        if not addresses:
            raise RuntimeError(f"ACME name has no usable IPv4 DNS record: {name}")
        common = addresses if common is None else common & addresses
    if not common:
        raise RuntimeError("ACME names must resolve to a common IPv4 address of this server")


def obtain_certificate(args, cert, key, names, frontend):
    existing = certificate_names(cert) if Path(cert).is_file() else set()
    if Path(key).is_file() and set(names).issubset(existing) and certificate_usable(cert):
        return
    validate_acme_dns(names)
    # Preserve any additional names in the owned lineage when expanding its SAN.
    requested = list(dict.fromkeys(names + sorted(existing - set(names))))
    if any(name.startswith("*.") for name in requested):
        raise RuntimeError("Wildcard ACME lineages require DNS challenges; keep manual certificate management")
    webroot = getattr(args, "acme_webroot", None)
    if webroot:
        webroot = certificate_path(str(webroot))
        if not Path(webroot).is_dir():
            raise RuntimeError("ACME webroot must be an existing directory served on HTTP/80 for every configured name")
    elif frontend == "nginx":
        webroot = WEBROOT
        apply_nginx(nginx_config(args.domain, names=names))
    else:
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as challenge:
                challenge.bind(("0.0.0.0", 80))
        except OSError:
            raise RuntimeError("HTTP/80 is occupied; configure the existing HTTP server's challenge path and pass --acme-webroot PATH") from None
    method = ["--webroot", "-w", webroot] if webroot else ["--standalone", "--preferred-challenges", "http"]
    email = ["--email", args.email] if args.email else ["--register-unsafely-without-email"]
    domains = [item for name in requested for item in ("-d", name)]
    print("Obtaining the configured SAN certificate with unattended ACME validation.", flush=True)
    run("certbot", "certonly", *method, "--cert-name", args.domain, *domains,
        "--expand", "--non-interactive", "--agree-tos", *email)


def renewal_hook(domain, frontend):
    reload_nginx = "nginx -t\nsystemctl reload nginx\n" if frontend == "nginx" else ""
    return f"""#!/bin/sh
{MARKER}
set -eu
[ "${{RENEWED_LINEAGE:-}}" = "/etc/letsencrypt/live/{domain}" ] || exit 0
{reload_nginx}if systemctl is-active --quiet quic-lab; then systemctl restart quic-lab; fi
if systemctl is-active --quiet quic-lab-vless; then systemctl restart quic-lab-vless; fi
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__, epilog="Example: sudo ./install-server.py quic.example.org --enable-vless --vless-server-name vless.quic.example.org. Existing installations retain saved settings; use --migrate-unified explicitly.")
    parser.add_argument("domain", type=domain_name)
    parser.add_argument("--ingress", choices=("unified", "split"), help="Fresh default: unified TCP/443 and UDP/443. Existing installations retain their layout")
    parser.add_argument("--migrate-unified", action="store_true", help="Explicitly migrate an existing installation and its exported profiles to unified ingress")
    parser.add_argument("--vless-server-name", type=domain_name, help="Exact VLESS SNI; unified default: vless.DOMAIN (must differ from local HTTPS/VPN names)")
    parser.add_argument("--acme", action="store_true", help="Switch saved manual certificates to the managed Let's Encrypt SAN lineage")
    parser.add_argument("--acme-webroot", type=Path, help="Existing HTTP/80 challenge webroot for every TLS hostname; nginx is not changed in direct mode")
    parser.add_argument("--frontend", choices=("nginx", "direct"), help="Explicit frontend; direct requires TCP/443 to be available (nginx is never moved automatically)")
    parser.add_argument("--tls-fallback", help="Direct frontend: pass other TLS SNI names to this host:port, including remote hosts")
    parser.add_argument("--email", help="Optional Let's Encrypt account email")
    parser.add_argument("--binary", type=Path, default=Path(__file__).resolve().parent / "quic-lab-server")
    parser.add_argument("--enable-vless", action="store_true", help="Provision optional managed VLESS: exact-SNI TCP/443 routing in unified mode, dedicated TCP in split mode")
    parser.add_argument("--vless-port", type=port_number, help="VLESS backend port: unified default 9444 on loopback; split chooses 8443/9443/10443")
    parser.add_argument("--vless-binary", type=Path, default=Path(__file__).resolve().parent / "quic-lab-vless")
    parser.add_argument("--enable-awg", action="store_true", help="Enable optional AmneziaWG worker (dedicated UDP port and firewall chains)")
    parser.add_argument("--awg-address", help="Initial AWG server IPv4/prefix, e.g. 10.77.0.1/24")
    parser.add_argument("--awg-port", type=port_number, help="AWG UDP port; initial default 51820")
    parser.add_argument("--apk", type=Path, help="Optional Android APK to publish")
    parser.add_argument("--gateway-allow", type=cidrs, help="Initial allowed IPv4 CIDRs; default 0.0.0.0/0. Existing policy is preserved.")
    parser.add_argument("--cert", help="Existing server PEM chain; use together with --key")
    parser.add_argument("--key", help="Existing server PEM key; skips certificate issuance")
    parser.add_argument("--echo-quic-port", type=port_number, help="Echo UDP port (unified: 443; split: 4433)")
    parser.add_argument("--vpn-quic-port", type=port_number, help="VPN QUIC UDP port (unified: 443; split: 4434)")
    parser.add_argument("--mtls-port", type=port_number, help="VPN HTTPS/mTLS TCP port (unified: 443; split default: 8443)")
    if len(sys.argv) == 1:
        parser.print_help()
        return
    args = parser.parse_args()
    if (args.awg_port or args.awg_address) and not args.enable_awg:
        parser.error("--awg-port/--awg-address require --enable-awg")
    if args.vless_port and not (args.enable_vless or args.migrate_unified):
        parser.error("--vless-port requires --enable-vless or --migrate-unified")
    if os.geteuid() != 0:
        parser.error("Run with sudo/root.")
    if args.acme and (args.cert or args.key):
        parser.error('--acme cannot be combined with --cert/--key')
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
    if getattr(args, "acme", False):
        custom_cert = False
        cert = f"/etc/letsencrypt/live/{args.domain}/fullchain.pem"
        key = f"/etc/letsencrypt/live/{args.domain}/privkey.pem"
    if getattr(args, "acme_webroot", None) is None and (state or {}).get("acme_webroot"):
        args.acme_webroot = Path(state["acme_webroot"])
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
    server_file = CONFIG / "server.json"
    old_server = server_file.read_bytes() if server_file.exists() else None
    previous_server = json.loads(old_server) if old_server is not None else None
    frontend = "direct" if getattr(args, "migrate_unified", False) else (getattr(args, "frontend", None) or resolve_frontend(state, cfg))
    prior_ingress = resolve_ingress(argparse.Namespace(), state, cfg, resolve_frontend(state, cfg)) if state or cfg else None
    args.ingress = resolve_ingress(args, state, cfg, frontend)
    args.change_layout = bool(prior_ingress and args.ingress != prior_ingress)
    if frontend == "direct" and (old_config is None or getattr(args, "migrate_unified", False) or getattr(args, "frontend", None) == "direct"):
        preflight_direct_listener(previous_server, resolve_frontend(state, cfg))
    if getattr(args, "tls_fallback", None) and frontend != "direct":
        raise RuntimeError("--tls-fallback requires --frontend direct")
    # Do not silently override a customized legacy ExecStart with a new base unit.
    for dropin in UNIT.with_name(UNIT.name + ".d").glob("*.conf"):
        if re.search(r"(?m)^\s*ExecStart=", dropin.read_text()):
            raise RuntimeError(f"Legacy ExecStart override in {dropin}; migrate its values to server.json and archive the override first.")
    if frontend == "nginx":
        if not shutil.which("nginx"):
            raise RuntimeError("Saved frontend is nginx, but nginx is missing; restore it before updating")
        owned(SITE)
        if ENABLED.exists() or ENABLED.is_symlink():
            if not ENABLED.is_symlink() or ENABLED.resolve() != SITE:
                raise RuntimeError(f"Unmanaged nginx site: {ENABLED}")
    ports = resolve_ports(args, state, cfg, frontend)
    if previous_server and not getattr(args, "migrate_unified", False) and not args.change_layout:
        for option, field in (("echo_quic_port", "listen"), ("vpn_quic_port", "gateway_quic"), ("mtls_port", "gateway_https")):
            if getattr(args, option, None) is None and previous_server.get(field):
                ports[option] = port_number(previous_server[field].rsplit(":", 1)[1])
    old_state = state_file.read_bytes() if state_file.exists() else None
    env_file = CONFIG / "server.env"
    if args.gateway_allow and (env_file.exists() or server_file.exists()):
        raise RuntimeError("Policy already exists; edit gateway_allow in /etc/quic-lab/server.json and restart the service instead.")
    desired_vless, vless_managed = vless_settings(args, cfg, DATA, ports, frontend, args.domain)
    vless_managed = vless_managed or VLESS_UNIT.exists()
    vless_binary_source = getattr(args, "vless_binary", Path(__file__).with_name("quic-lab-vless"))
    if vless_managed:
        owned(VLESS_UNIT)
        for dropin in VLESS_UNIT.with_name(VLESS_UNIT.name + ".d").glob("*.conf"):
            raise RuntimeError("VLESS unit has local overrides; review and migrate them before updating")

    desired_server = server_config(ports, frontend, args.domain, args.gateway_allow or legacy_policy(env_file), previous_server, args)
    if getattr(args, "acme", False):
        desired_server.update(cert="${CREDENTIALS_DIRECTORY}/cert.pem", key="${CREDENTIALS_DIRECTORY}/key.pem")
        if desired_vless and desired_vless.get("security") == "tls":
            desired_vless.update(tls_certificate_file="/run/credentials/quic-lab-vless.service/cert.pem", tls_key_file="/run/credentials/quic-lab-vless.service/key.pem")
    if vless_managed:
        validate_vless(vless_binary_source, desired_vless)
    if args.ingress == "unified" and desired_vless:
        configure_vless_route(desired_server, desired_vless, previous=(cfg or {}).get("vless"))
    certificate_domains = [args.domain]
    if desired_vless and desired_vless.get("security") == "tls":
        certificate_domains = list(dict.fromkeys(certificate_domains + [domain_name(desired_vless["server_name"])]))
    if custom_cert:
        validate_certificate_names(cert, certificate_domains)
    install_state = dict(domain=args.domain, cert=cert, key=key, custom_cert=custom_cert, ports=ports, frontend=frontend, ingress=args.ingress)
    if getattr(args, "acme_webroot", None):
        install_state["acme_webroot"] = certificate_path(str(args.acme_webroot))
    # Validate before changing packages, configs, nginx or a running service.
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as pending:
        json.dump(desired_server, pending); pending.flush()
        run(str(args.binary.resolve()), "-config", pending.name, "-check-config",
            env={**os.environ, "CREDENTIALS_DIRECTORY": "/run/credentials/quic-lab.service"})
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
    backup = None
    if old_config is not None:
        backup = CONFIG / ("backup-" + time.strftime("%Y%m%d-%H%M%S") + "-" + secrets.token_hex(3))
        backup.mkdir(mode=0o700)
        for source in (existing, server_file, state_file, env_file, UNIT, VLESS_UNIT, HOOK, SITE):
            if source.is_file(): atomic(backup / (source.parent.name + "-" + source.name), source.read_bytes(), 0o600)
        print(f"Previous configuration saved in {backup}", flush=True)
    if not existing.exists():
        cfg = admin_config(args.domain, ports)
        if desired_vless: cfg["vless"] = desired_vless
        atomic(existing, json.dumps(cfg, indent=2) + "\n", 0o600)
        credentials = f"URL: {cfg['public_url']}login\nLogin: {cfg['username']}\nPassword: {cfg['password']}\n"
        atomic(CONFIG / "admin-credentials.txt", credentials, 0o600)
        print("New administrator credentials (saved in /etc/quic-lab/admin-credentials.txt):\n" + credentials, flush=True)
    else:
        print("Existing admin configuration and password preserved.", flush=True)
    if old_config is None:
        # Keep initial mode after a failed ACME attempt, even though admin.json
        # already exists when the user retries installation.
        atomic(state_file, json.dumps(install_state, indent=2) + "\n", 0o600)
        # Preserve a restricted ACL if ACME fails and installation is retried.
        atomic(server_file, json.dumps(desired_server, indent=2) + "\n", 0o600)
    if frontend == "nginx":
        Path(WEBROOT).mkdir(mode=0o755, parents=True, exist_ok=True)
    if not custom_cert:
        obtain_certificate(args, cert, key, certificate_domains, frontend)
        validate_certificate_names(cert, certificate_domains)
        atomic(HOOK, renewal_hook(args.domain, frontend), 0o755)
        run("systemctl", "enable", "--now", "certbot.timer")
    run("openssl", "x509", "-in", cert, "-noout", "-checkend", "0")
    # Validate the pair before replacing the binary/unit, also without nginx.
    import ssl
    ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER).load_cert_chain(cert, key)
    if frontend == "nginx":
        apply_nginx(nginx_config(args.domain, cert, key, names=certificate_domains))
    binary = OPT / "quic-lab-server"
    old_binary = binary.read_bytes() if binary.exists() else None
    old_unit = UNIT.read_bytes() if UNIT.exists() else None
    was_active = subprocess.run(["systemctl", "is-active", "--quiet", "quic-lab"]).returncode == 0
    worker_binary = OPT / "quic-lab-vless"
    old_worker_binary = worker_binary.read_bytes() if worker_binary.exists() else None
    old_worker_unit = VLESS_UNIT.read_bytes() if VLESS_UNIT.exists() else None
    worker_was_active = vless_managed and subprocess.run(["systemctl", "is-active", "--quiet", "quic-lab-vless"]).returncode == 0
    worker_stopped = False
    new_started = False
    snapshot_complete = False
    stopped = False
    replaced = False
    try:
        if vless_managed and old_worker_unit is not None:
            run("systemctl", "stop", "quic-lab-vless")
            worker_stopped = True
        if old_config is not None:
            run("systemctl", "stop", "quic-lab")
            stopped = True
            snapshot_identities(DATA, backup)
            snapshot_complete = True
        if old_binary is not None:
            atomic(OPT / "quic-lab-server.previous", old_binary, 0o755)
        replaced = True
        atomic(binary, args.binary.read_bytes(), 0o755)
        atomic(UNIT, unit_config(cert, key, ports, frontend))
        if vless_managed:
            if old_worker_binary is not None:
                atomic(OPT / "quic-lab-vless.previous", old_worker_binary, 0o755)
            atomic(worker_binary, vless_binary_source.read_bytes(), 0o755)
            atomic(VLESS_UNIT, vless_unit_config(cert, key, desired_vless))
        if vless_managed and (getattr(args, "migrate_unified", False) or getattr(args, "acme", False)):
            migrate_vless_identity(DATA, desired_vless, expected=dict(config=args.vless_previous))
        atomic(server_file, json.dumps(desired_server, indent=2) + "\n", 0o600)
        if old_config is not None:
            for name, section, field in (("echo_quic_port", "echo", "endpoint"), ("vpn_quic_port", "vpn", "quic"), ("mtls_port", "vpn", "https")):
                # Keep custom hostnames, account, routing and all other settings.
                host = cfg[section][field].rsplit(":", 1)[0]
                cfg[section][field] = f"{host}:{ports[name]}"
            if desired_vless:
                cfg["vless"] = desired_vless
            elif vless_managed:
                cfg.pop("vless", None)
            atomic(existing, json.dumps(cfg, indent=2) + "\n", 0o600)
        atomic(state_file, json.dumps(install_state, indent=2) + "\n", 0o600)
        run("systemctl", "daemon-reload")
        run("systemctl", "enable", "quic-lab")
        new_started = True  # Even a failing restart can briefly accept mutations.
        run("systemctl", "restart", "quic-lab")
        if vless_managed:
            run("systemctl", "enable", "quic-lab-vless")
            run("systemctl", "restart", "quic-lab-vless")
        ready = False
        for _ in range(20):
            time.sleep(0.5)
            try:
                with urllib.request.urlopen("http://127.0.0.1:8083/", timeout=2) as response:
                    ready = response.status == 200
                ready = ready and subprocess.run(["systemctl", "is-active", "--quiet", "quic-lab"]).returncode == 0
                if vless_managed:
                    ready = ready and subprocess.run(["systemctl", "is-active", "--quiet", "quic-lab-vless"]).returncode == 0
                    if desired_vless: ready = ready and vless_applied(worker_binary, DATA)
                if ready:
                    break
            except OSError:
                pass
        if not ready:
            raise RuntimeError("Server did not become healthy; inspect journalctl -u quic-lab")
    except BaseException:
        if not replaced:
            if stopped and was_active:
                run("systemctl", "start", "quic-lab")
            if worker_stopped and worker_was_active:
                run("systemctl", "start", "quic-lab-vless")
            raise
        if vless_managed:
            run("systemctl", "stop", "quic-lab-vless")
        run("systemctl", "stop", "quic-lab")
        preserve_revocations = vless_managed and new_started
        if preserve_revocations:
            # Keep recovery fail-closed across reboot and dependency activation.
            marker = CONFIG / "recovery-required"
            atomic(marker, "Inspect current identities and VLESS tombstones before resuming.\n", 0o600)
            for guarded_unit in (UNIT, VLESS_UNIT):
                dropins = guarded_unit.with_name(guarded_unit.name + ".d")
                dropins.mkdir(mode=0o755, parents=True, exist_ok=True)
                atomic(dropins / "90-quic-lab-recovery.conf",
                       f"{MARKER}\n[Unit]\nConditionPathExists=!{marker}\n", 0o644)
            run("systemctl", "daemon-reload")
        if snapshot_complete and not preserve_revocations:
            restore_identities(DATA, backup)
        if old_server is not None:
            atomic(server_file, old_server, 0o600)
        elif old_config is not None:
            server_file.unlink(missing_ok=True)
        if old_config is not None:
            atomic(existing, old_config, 0o600)
        if old_state is not None:
            atomic(state_file, old_state, 0o600)
        elif old_config is not None:
            state_file.unlink(missing_ok=True)
        if vless_managed:
            if old_worker_binary is not None: atomic(worker_binary, old_worker_binary, 0o755)
            else: worker_binary.unlink(missing_ok=True)
            if old_worker_unit is not None: atomic(VLESS_UNIT, old_worker_unit)
            else: VLESS_UNIT.unlink(missing_ok=True)
        if old_binary is not None and old_unit is not None:
            atomic(binary, old_binary, 0o755)
            atomic(UNIT, old_unit)
            run("systemctl", "daemon-reload")
            if was_active and not preserve_revocations:
                run("systemctl", "start", "quic-lab")
            if worker_was_active and not preserve_revocations:
                run("systemctl", "start", "quic-lab-vless")
            print("Previous server binary and unit restored.", file=sys.stderr)
        if preserve_revocations:
            raise RuntimeError("Upgrade failed after services started. Identities and VLESS tombstones preserved; quic-lab and quic-lab-vless left stopped with persistent recovery gates. Inspect /etc/quic-lab/recovery-required and unit drop-ins. Install a compatible release and inspect durable state before restarting; never restore identity/worker-state backups.") from None
        raise
    if args.apk:
        # StateDirectory belongs to DynamicUser; preserve its ownership.
        downloads = Path("/var/lib/quic-lab/downloads")
        downloads.mkdir(mode=0o755, exist_ok=True)
        atomic(downloads / "quic-lab.apk", args.apk.read_bytes())
    # An enabled transport must be upgraded with the server, even without opt-in flags.
    if args.enable_awg or (cfg and cfg.get("awg")):
        command = [sys.executable, str(Path(__file__).with_name("install-awg.py")), "--binary", str(Path(__file__).with_name("quic-lab-awg"))]
        if args.awg_port:
            command += ["--port", str(args.awg_port)]
        if args.awg_address:
            command += ["--address", args.awg_address]
        run(*command)
    exposed_tcp = {80, 443, ports["mtls_port"]}
    if desired_vless and not desired_vless.get("accept_proxy_protocol"): exposed_tcp.add(port_number(desired_vless["listen"].rsplit(":", 1)[1]))
    tcp_ports = ",".join(str(p) for p in sorted(exposed_tcp))
    print(f"Frontend: {frontend}\nReady: https://{args.domain}/lab/\nConfig: /etc/quic-lab/server.json and admin.json\n"
          "Credentials: /etc/quic-lab/admin-credentials.txt (first installation)\n"
          f"Allow inbound TCP {tcp_ports} and UDP {ports['echo_quic_port']},{ports['vpn_quic_port']} in host/cloud firewalls.\n"
          "Firewall rules were not changed. Updates restart active sessions.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Installation failed: {error}", file=sys.stderr)
        sys.exit(1)
