#!/usr/bin/env python3
"""Unit tests; run on Linux (installer uses fcntl)."""
import importlib.util
import tempfile
import contextlib
import io
from types import SimpleNamespace
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("installer", Path(__file__).with_name("install-server.py"))
i = importlib.util.module_from_spec(spec)
spec.loader.exec_module(i)


class InstallerTests(unittest.TestCase):
    def test_upgrade_preserves_phase_one_server_metadata(self):
        previous = i.server_config(i.DEFAULT_PORTS, "direct", "lab.example.org")
        self.assertEqual(previous["capabilities"]["control_version"], 1)
        self.assertEqual(previous["capabilities"]["data_version"], 1)
        previous.update(
            vpn_sni_names=["cover.example.org"],
            capabilities={
                "control_version": 1,
                "data_version": 1,
                "min_android_version_code": 45,
                "apk_url": "https://lab.example.org/quic-lab.apk",
                "apk_sha256": "abc123",
            },
        )
        upgraded = i.server_config(
            i.DEFAULT_PORTS,
            "direct",
            "lab.example.org",
            previous=previous,
            args=SimpleNamespace(),
        )
        self.assertEqual(upgraded["vpn_sni_names"], ["cover.example.org"])
        self.assertEqual(upgraded["capabilities"], previous["capabilities"])
        self.assertNotIn("accepted_vpn_sni", upgraded)
        self.assertNotIn("update_base_url", upgraded)

    def test_nginx_forwards_root_control_and_device_routes(self):
        content = i.nginx_config("lab.example.org", "/cert.pem", "/key.pem")
        self.assertIn(
            "location = /api/v1/capabilities {\n"
            "        proxy_pass http://127.0.0.1:8083/api/v1/capabilities;",
            content,
        )
        self.assertIn(
            "location ^~ /api/v1/devices/ {\n"
            "        proxy_pass http://127.0.0.1:8083;",
            content,
        )
        self.assertNotIn("location /api/", content)

    def test_existing_nginx_is_never_installed_or_upgraded(self):
        with patch.object(i.shutil, "which", side_effect=lambda c: "/usr/bin/" + c if c in ("nginx", "openssl", "kill") else None), patch.object(i.Path, "is_file", return_value=True):
            self.assertEqual(i.missing_packages(False), ["certbot"])
            self.assertEqual(i.missing_packages(True), [])
        with patch.object(i.shutil, "which", return_value=None), patch.object(i.Path, "is_file", return_value=False):
            self.assertEqual(set(i.missing_packages(False)), {"nginx", "openssl", "procps", "certbot", "ca-certificates"})

    def test_running_nginx_only_validated_and_reloaded(self):
        with tempfile.TemporaryDirectory() as tmp:
            site, enabled = Path(tmp) / "site", Path(tmp) / "enabled"
            with patch.object(i, "SITE", site), patch.object(i, "ENABLED", enabled), patch.object(i, "run") as run, patch.object(i.subprocess, "run") as status:
                status.return_value.returncode = 0
                i.apply_nginx("# test")
                self.assertEqual([c.args for c in run.call_args_list], [("nginx", "-t"), ("systemctl", "reload", "nginx")])
                self.assertTrue(enabled.is_symlink())

    def test_help_without_arguments_does_not_require_root_or_install(self):
        with patch.object(i.sys, "argv", ["install-server.py"]), patch.object(i.os, "geteuid", side_effect=AssertionError("must not check root")), patch.object(i, "install") as install, contextlib.redirect_stdout(io.StringIO()) as out:
            i.main()
            self.assertIn("--vpn-quic-port", out.getvalue())
            self.assertIn("--mtls-port", out.getvalue())
            install.assert_not_called()

    def test_custom_ports_profiles_unit_and_persistence(self):
        ports = i.resolve_ports(SimpleNamespace(vpn_quic_port=443, mtls_port=9443, echo_quic_port=14433))
        cfg = i.admin_config("lab.example.org", ports)
        self.assertEqual(cfg["echo"]["endpoint"], "lab.example.org:14433")
        self.assertEqual(cfg["vpn"]["quic"], "lab.example.org:443")
        self.assertEqual(cfg["vpn"]["https"], "lab.example.org:9443")
        unit = i.unit_config("/cert.pem", "/key.pem", ports)
        self.assertEqual(i.server_config(ports, "nginx", "lab.example.org")["gateway_quic"], "0.0.0.0:443")
        self.assertIn("-config ${CREDENTIALS_DIRECTORY}/server.json", unit)
        self.assertIn("AmbientCapabilities=CAP_NET_BIND_SERVICE", unit)
        self.assertEqual(i.resolve_ports(SimpleNamespace(), {"ports": ports}, cfg), ports)
        self.assertEqual(i.resolve_ports(SimpleNamespace(), None, cfg), ports)
        self.assertEqual(i.resolve_ports(SimpleNamespace()), i.DEFAULT_PORTS)
        self.assertNotIn("AmbientCapabilities", i.unit_config("/cert", "/key"))
        self.assertEqual(i.resolve_ports(SimpleNamespace(mtls_port=10443), {"ports": ports}, cfg)["mtls_port"], 10443)

    def test_server_config_preserves_custom_bindings_and_fallback(self):
        saved = i.server_config(i.DEFAULT_PORTS, "direct", "lab.example.org")
        saved.update(tls_fallback="192.0.2.20:9443", cert="/custom/cert.pem", key="/custom/key.pem", gateway_allow="10.0.0.0/8", listen="192.0.2.10:24433")
        self.assertEqual(i.server_config(i.DEFAULT_PORTS, "direct", "lab.example.org", previous=saved, args=SimpleNamespace()), saved)
        changed = i.server_config(i.DEFAULT_PORTS, "direct", "lab.example.org", previous=saved, args=SimpleNamespace(echo_quic_port=4433))
        self.assertEqual(changed["listen"], "192.0.2.10:4433")
        self.assertEqual(changed["tls_fallback"], "192.0.2.20:9443")
        self.assertEqual(changed["gateway_allow"], "10.0.0.0/8")

    def test_legacy_policy_migration(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "server.env"
            p.write_text('GATEWAY_ALLOW="10.0.0.0/8,1.1.1.1/32"\n')
            self.assertEqual(i.legacy_policy(p), "10.0.0.0/8,1.1.1.1/32")

    def test_failed_acme_retry_preserves_restricted_acl(self):
        import subprocess
        import json
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            root = Path(tmp)
            for name, target in (("CONFIG", "etc"), ("OPT", "opt"), ("UNIT", "unit"), ("HOOK", "hook"), ("SITE", "site")):
                stack.enter_context(patch.object(i, name, root / target))
            stack.enter_context(patch.object(i, "missing_packages", return_value=[]))
            def run(*args, **kwargs):
                if args[0] == "certbot": raise subprocess.CalledProcessError(1, args)
                return subprocess.CompletedProcess(args, 0)
            stack.enter_context(patch.object(i, "run", side_effect=run))
            args = SimpleNamespace(domain="acl-retry.example.invalid", cert=None, key=None, apk=None, binary=root / "binary", frontend="direct", tls_fallback=None, gateway_allow="10.0.0.0/8", email=None)
            for attempt in range(2):
                with self.assertRaises(subprocess.CalledProcessError), contextlib.redirect_stdout(io.StringIO()):
                    i.install(args)
                self.assertEqual(json.loads((root / "etc/server.json").read_text())["gateway_allow"], "10.0.0.0/8")
                args.gateway_allow = None

    def test_port_validation(self):
        for value in ("0", "65536", "-1", "https", "443;false"):
            with self.assertRaises(Exception):
                i.port_number(value)
        for args in (SimpleNamespace(vpn_quic_port=4433), SimpleNamespace(mtls_port=443), SimpleNamespace(mtls_port=8083)):
            with self.assertRaises(ValueError):
                i.resolve_ports(args)
        # TCP and UDP can share a numeric port.
        self.assertEqual(i.resolve_ports(SimpleNamespace(mtls_port=4434))["mtls_port"], 4434)

    def test_frontend_detection_and_saved_mode(self):
        with patch.object(i.shutil, "which", return_value=None):
            self.assertEqual(i.resolve_frontend(), "direct")
            self.assertEqual(i.resolve_frontend({"domain": "old.example.org"}), "nginx")
        with patch.object(i.shutil, "which", return_value="/usr/sbin/nginx"):
            self.assertEqual(i.resolve_frontend(), "nginx")
            self.assertEqual(i.resolve_frontend({"frontend": "direct"}), "direct")
        ports = i.resolve_ports(SimpleNamespace(), frontend="direct")
        self.assertEqual(ports["mtls_port"], 443)
        self.assertEqual(i.server_config(ports, "direct", "lab.example.org")["https_listen"], "0.0.0.0:443")
        self.assertNotIn("-https-listen", i.unit_config("/cert", "/key"))
        with patch.object(i.shutil, "which", return_value=None), patch.object(i.Path, "is_file", return_value=False):
            self.assertNotIn("nginx", i.missing_packages(False, "direct"))

    def test_domain_validation(self):
        self.assertEqual(i.domain_name("Lab.Example.org."), "lab.example.org")
        for name in ("127.0.0.1", "https://example.org", "x.example/evil", "x.example;foo", "-a.example", "*.example.org", "foo..org", "a" * 64 + ".org"):
            with self.assertRaises(Exception):
                i.domain_name(name)

    def test_secrets_are_unique(self):
        a, b = i.admin_config("lab.example.org"), i.admin_config("lab.example.org")
        self.assertNotEqual(a["username"], b["username"])
        self.assertNotEqual(a["password"], b["password"])
        self.assertGreaterEqual(len(a["password"]), 24)
        self.assertEqual(a["public_url"], "https://lab.example.org/lab/")

    def test_owned_and_atomic_permissions(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "file"
            i.atomic(p, "#!/bin/sh\n" + i.MARKER + "\n", 0o600)
            i.owned(p)
            self.assertEqual(p.stat().st_mode & 0o777, 0o600)
            i.atomic(p, "foreign config")
            with self.assertRaises(RuntimeError):
                i.owned(p)
            p.unlink()
            p.symlink_to(Path(tmp) / "missing")
            with self.assertRaises(RuntimeError):
                i.owned(p)

    def test_invalid_routing_and_paths(self):
        for value in ("::/0", "192.168.1.1/24", "0.0.0.0/0;echo"):
            with self.assertRaises(Exception):
                i.cidrs(value)
        self.assertEqual(i.cidrs("10.0.0.0/8, 1.1.1.1/32"), "10.0.0.0/8,1.1.1.1/32")
        for value in ("relative.pem", "/tmp/a b", "/tmp/a;bad", "/tmp/../key"):
            with self.assertRaises(ValueError):
                i.certificate_path(value)



class UpgradeStateTests(unittest.TestCase):
    def test_snapshot_restores_primary_and_absent_backup(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); data = root / 'data'; data.mkdir(); backup = root / 'backup'; backup.mkdir()
            primary = data / 'identities.json'
            primary.write_bytes(b'synthetic-v1-users-and-key'); primary.chmod(0o600)
            original = primary.stat()
            i.snapshot_identities(data, backup)
            primary.write_bytes(b'migrated-v2')
            (data / 'identities.json.bak').write_bytes(b'overwritten-backup')
            i.restore_identities(data, backup)
            self.assertEqual(primary.read_bytes(), b'synthetic-v1-users-and-key')
            self.assertFalse((data / 'identities.json.bak').exists())
            self.assertEqual(primary.stat().st_uid, original.st_uid)
            self.assertEqual(primary.stat().st_gid, original.st_gid)
            self.assertEqual(primary.stat().st_mode & 0o777, 0o600)
            self.assertEqual((backup / 'identities.json').stat().st_mode & 0o777, 0o600)

    def test_snapshot_preserves_original_backup_and_rejects_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); data = root / 'data'; data.mkdir(); backup = root / 'backup'; backup.mkdir()
            (data / 'identities.json').write_bytes(b'primary')
            (data / 'identities.json.bak').write_bytes(b'prior')
            i.snapshot_identities(data, backup)
            (data / 'identities.json.bak').write_bytes(b'new')
            i.restore_identities(data, backup)
            self.assertEqual((data / 'identities.json.bak').read_bytes(), b'prior')
            (data / 'identities.json').unlink(); (data / 'identities.json').symlink_to(data / 'identities.json.bak')
            with self.assertRaises(RuntimeError): i.snapshot_identities(data, backup)



class UpgradeInstallTests(unittest.TestCase):
    def exercise(self, mode='direct', failure=None, active=True):
        import json
        import ssl
        import subprocess
        from unittest.mock import MagicMock
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            root = Path(tmp)
            for name in ('CONFIG', 'OPT', 'DATA', 'UNIT', 'HOOK', 'SITE', 'ENABLED'):
                stack.enter_context(patch.object(i, name, root / name.lower()))
            stack.enter_context(patch.object(i, 'WEBROOT', str(root / 'webroot')))
            for folder in (i.CONFIG, i.OPT, i.DATA): folder.mkdir()
            cfg = i.admin_config('lab.example.org')
            (i.CONFIG / 'admin.json').write_text(json.dumps(cfg))
            (i.CONFIG / 'server.json').write_text(json.dumps(i.server_config(i.DEFAULT_PORTS, mode, 'lab.example.org')))
            (i.CONFIG / 'install.json').write_text(json.dumps(dict(domain='lab.example.org', frontend=mode, custom_cert=True, cert=str(root / 'cert'), key=str(root / 'key'))))
            for name in ('cert', 'key'): (root / name).touch()
            i.UNIT.write_text(i.MARKER); (i.OPT / 'quic-lab-server').write_bytes(b'old')
            candidate = root / 'candidate'; candidate.write_bytes(b'new')
            identity = i.DATA / 'identities.json'; identity.write_bytes(b'synthetic-users-devices-key'); identity.chmod(0o600)
            before = {p.name:p.read_bytes() for p in i.CONFIG.iterdir()}
            if failure == 'snapshot':
                identity.unlink(); identity.symlink_to(root / 'cert')
            calls = []
            def run(*args, **kwargs):
                calls.append(args)
                if '-check-config' in args and failure == 'validation': raise RuntimeError('invalid')
                if args == ('systemctl', 'restart', 'quic-lab'):
                    self.assertIn(('systemctl', 'stop', 'quic-lab'), calls)
                    snapshot = next(i.CONFIG.glob('backup-*/identities.json'))
                    self.assertEqual(snapshot.read_bytes(), b'synthetic-users-devices-key')
                    identity.write_bytes(b'new-schema'); (i.DATA / 'identities.json.bak').write_bytes(b'new-backup')
                if args == ('systemctl', 'start', 'quic-lab') and failure != 'snapshot':
                    self.assertEqual(identity.read_bytes(), b'synthetic-users-devices-key')
                    self.assertFalse((i.DATA / 'identities.json.bak').exists())
                return subprocess.CompletedProcess(args, 0, stdout='')
            stack.enter_context(patch.object(i, 'run', side_effect=run))
            if failure == 'sync': stack.enter_context(patch.object(i, 'sync_directory', side_effect=OSError('disk sync failed')))
            stack.enter_context(patch.object(i, 'missing_packages', return_value=[]))
            stack.enter_context(patch.object(i.shutil, 'which', return_value='/fake/nginx'))
            stack.enter_context(patch.object(ssl.SSLContext, 'load_cert_chain'))
            stack.enter_context(patch.object(i, 'apply_nginx'))
            stack.enter_context(patch.object(i.time, 'sleep'))
            stack.enter_context(patch.object(i.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0 if active else 3)))
            http = MagicMock(); http.__enter__.return_value.status = 200
            stack.enter_context(patch.object(i.urllib.request, 'urlopen', return_value=http, side_effect=OSError('health') if failure == 'health' else None))
            args = SimpleNamespace(domain='lab.example.org', cert=None, key=None, apk=None, binary=candidate, frontend=None, tls_fallback=None, gateway_allow=None, email=None, enable_awg=False)
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                if failure:
                    with self.assertRaises((RuntimeError, OSError)): i.install(args)
                else: i.install(args)
            if failure:
                for name, content in before.items(): self.assertEqual((i.CONFIG / name).read_bytes(), content)
                self.assertEqual((i.OPT / 'quic-lab-server').read_bytes(), b'old')
                if failure != 'snapshot': self.assertEqual(identity.read_bytes(), b'synthetic-users-devices-key')
                if failure in ('snapshot', 'sync'):
                    self.assertEqual(('systemctl', 'start', 'quic-lab') in calls, active)
                    self.assertNotIn(('systemctl', 'restart', 'quic-lab'), calls)
                if failure == 'validation': self.assertNotIn(('systemctl', 'stop', 'quic-lab'), calls)
            else:
                self.assertEqual(json.loads((i.CONFIG / 'admin.json').read_text())['password'], cfg['password'])
                self.assertEqual(json.loads((i.CONFIG / 'install.json').read_text())['frontend'], mode)
                self.assertEqual(next(i.CONFIG.glob('backup-*/identities.json')).read_bytes(), b'synthetic-users-devices-key')
    def test_durability_failure_never_starts_new_server(self): self.exercise(failure='sync')
    def test_snapshot_failure_restarts_previously_active_service(self): self.exercise(failure='snapshot')
    def test_snapshot_failure_keeps_stopped_service_stopped(self): self.exercise(failure='snapshot', active=False)
    def test_upgrade_keeps_device_state(self): self.exercise()
    def test_nginx_mode_preserved(self): self.exercise('nginx')
    def test_failed_validation_no_mutations(self): self.exercise(failure='validation')
    def test_failed_health_restores_old_state_config_and_binary(self): self.exercise(failure='health')





class DurableUpgradeTests(unittest.TestCase):
    def test_snapshot_directory_sync_failure_aborts_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); data = root / 'data'; data.mkdir(); backup = root / 'backup'; backup.mkdir()
            (data / 'identities.json').write_bytes(b'old-state')
            with patch.object(i, 'sync_directory', side_effect=OSError('disk sync failed')):
                with self.assertRaises(OSError): i.snapshot_identities(data, backup)
            self.assertEqual((data / 'identities.json').read_bytes(), b'old-state')

if __name__ == '__main__':
    unittest.main()
