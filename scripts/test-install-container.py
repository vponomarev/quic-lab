#!/usr/bin/env python3
"""Disposable Docker smoke test. Real apt/nginx/openssl/server, simulated systemctl.
Docker lacks systemd: validate the generated unit separately with systemd-analyze.
"""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import urllib.request

assert Path("/.dockerenv").exists(), "Use only inside a disposable Docker container"
spec = importlib.util.spec_from_file_location("installer", "/test/install-server.py")
i = importlib.util.module_from_spec(spec)
spec.loader.exec_module(i)
frontend = os.environ.get("TEST_FRONTEND", "nginx")
real_run = i.run
server = None
nginx_started = False
fail_next_start = False


def run(*args, **kwargs):
    global server, nginx_started, fail_next_start
    if frontend == "direct" and args[0] == "nginx":
        raise AssertionError("Direct mode must never invoke nginx")
    if args[0] != "systemctl":
        return real_run(*args, **kwargs)
    if "nginx" in args:
        if not nginx_started:
            real_run("nginx")
            nginx_started = True
        elif "reload" in args:
            real_run("nginx", "-s", "reload")
    if "quic-lab" in args:
        if args[1] in ("restart", "stop") and server:
            server.terminate()
            server.wait(timeout=10)
            server = None
        if args[1] in ("restart", "start"):
            if fail_next_start:
                fail_next_start = False
                raise subprocess.CalledProcessError(1, args)
            state = json.loads((i.CONFIG / "install.json").read_text())
            Path("/var/lib/quic-lab").mkdir(exist_ok=True)
            cmd = ["/opt/quic-lab/quic-lab-server", "-listen", f"0.0.0.0:{state['ports']['echo_quic_port']}", "-web-listen", "127.0.0.1:8081",
                   "-gateway-quic", f"0.0.0.0:{state['ports']['vpn_quic_port']}", "-gateway-https", f"0.0.0.0:{state['ports']['mtls_port']}", "-gateway-allow", "0.0.0.0/0",
                   "-demo-listen", "127.0.0.1:8082", "-cert", state["cert"], "-key", state["key"],
                   "-admin-config", "/etc/quic-lab/admin.json"]
            if state["frontend"] == "direct":
                cmd += ["-https-listen", "0.0.0.0:443"]
            server = subprocess.Popen(cmd, stdout=open("/tmp/server.log", "a"), stderr=subprocess.STDOUT)
            time.sleep(0.3)
            if server.poll() is not None:
                raise RuntimeError("Server failed to start")
    return subprocess.CompletedProcess(args, 0)


i.run = run
# Only the is-active health check invokes subprocess.run directly.
real_subprocess_run = subprocess.run

def direct_run(args, **kwargs):
    if args[0] == "systemctl":
        return subprocess.CompletedProcess(args, 0 if server and server.poll() is None else 3)
    return real_subprocess_run(args, **kwargs)

subprocess.run = direct_run
Path("/run/systemd/system").mkdir(parents=True, exist_ok=True)
Path("/test/tls").mkdir(exist_ok=True)
real_run("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2", "-subj", "/CN=lab.example.org",
         "-addext", "subjectAltName=DNS:lab.example.org", "-keyout", "/test/tls/key.pem", "-out", "/test/tls/cert.pem",
         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
# A second site must survive untouched.
other = Path("/etc/nginx/sites-available/other")
if frontend == "nginx":
    other.write_text("server { listen 80; server_name other.example.org; return 200 'other'; }\n")
    Path("/etc/nginx/sites-enabled/other").symlink_to(other)
    original_other = other.read_bytes()
sys.argv = ["install-server.py", "lab.example.org", "--binary", "/test/quic-lab-server", "--cert", "/test/tls/cert.pem", "--key", "/test/tls/key.pem", "--echo-quic-port", "14433", "--vpn-quic-port", "14434", "--mtls-port", "443" if frontend == "direct" else "18443"]
try:
    with contextlib.redirect_stdout(io.StringIO()) as output:
        i.main()
    cfg = json.loads((i.CONFIG / "admin.json").read_text())
    assert cfg["password"] in output.getvalue() and cfg["username"] in output.getvalue()
    assert (i.CONFIG / "admin.json").stat().st_mode & 0o777 == 0o600
    assert (i.CONFIG / "admin-credentials.txt").stat().st_mode & 0o777 == 0o600
    assert cfg["echo"]["endpoint"].endswith(":14433")
    assert cfg["vpn"]["quic"].endswith(":14434")
    assert cfg["vpn"]["https"].endswith(":443" if frontend == "direct" else ":18443")
    assert json.loads((i.CONFIG / "install.json").read_text())["frontend"] == frontend
    state_before = (i.CONFIG / "install.json").read_bytes()
    identity = Path("/var/lib/quic-lab/identities.json").read_bytes()
    with urllib.request.urlopen("http://127.0.0.1:8083/", timeout=3) as response:
        assert response.status == 200
    real_run("curl", "--fail", "--silent", "--insecure", "--resolve", "lab.example.org:443:127.0.0.1", "https://lab.example.org/lab/", stdout=subprocess.DEVNULL)
    real_run("systemd-analyze", "verify", str(i.UNIT))
    sys.argv = ["install-server.py", "lab.example.org", "--binary", "/test/quic-lab-server"]
    before = (i.CONFIG / "admin.json").read_bytes()
    with contextlib.redirect_stdout(io.StringIO()) as output:
        i.main()
    assert cfg["password"] not in output.getvalue()
    assert (i.CONFIG / "admin.json").read_bytes() == before
    assert Path("/var/lib/quic-lab/identities.json").read_bytes() == identity
    if frontend == "nginx":
        assert other.read_bytes() == original_other
    else:
        assert not Path("/etc/nginx/sites-available/quic-lab").exists()
    # Roll back a failed service restart, keeping identity/config.
    sys.argv += ["--vpn-quic-port", "24434", "--mtls-port", "28443"]
    fail_next_start = True
    try:
        with contextlib.redirect_stdout(io.StringIO()):
            i.main()
    except subprocess.CalledProcessError:
        pass
    else:
        raise AssertionError("Expected a simulated restart failure")
    assert server and server.poll() is None
    assert (i.CONFIG / "install.json").read_bytes() == state_before
    assert (i.CONFIG / "admin.json").read_bytes() == before
    assert Path("/var/lib/quic-lab/identities.json").read_bytes() == identity
    print("PASS " + frontend + ": fresh install, secrets/modes, HTTPS site, unit syntax, repeat install, preserved CA/config/ports/other site, port-change rollback")
finally:
    if server:
        server.terminate()
        server.wait(timeout=10)
