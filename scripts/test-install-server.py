#!/usr/bin/env python3
"""Unit tests; run on Linux (installer uses fcntl)."""
import importlib.util
import subprocess
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
    def test_backup_transit_credential_is_readable_by_dynamic_user(self):
        if not Path("/run/systemd/system").exists():
            self.skipTest("requires systemd Linux host")
        with tempfile.TemporaryDirectory() as tmp:
            flags = []
            unit = i.backup_transit_dropin()
            for line in unit.splitlines():
                if line.startswith("LoadCredential="):
                    name = line.split("=", 1)[1].split(":", 1)[0]
                    source = Path(tmp) / name
                    source.write_bytes(b"test-private-material")
                    source.chmod(0o600)
                    flags.extend(["-p", f"LoadCredential={name}:{source}"])
            result = subprocess.run(["systemd-run", "--quiet", "--wait", "--pipe", "-p", "DynamicUser=yes", *flags,
                "/usr/bin/python3", "-c", "import os,pathlib; p=pathlib.Path(os.environ['CREDENTIALS_DIRECTORY'])/'backup-transit.json'; assert p.read_bytes()==b'test-private-material'; print('credential-ok')"],
                check=True, text=True, capture_output=True)
            self.assertEqual(result.stdout.strip(), "credential-ok")

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
            stack.enter_context(patch.object(i, 'validate_acme_dns'))
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
            self.assertEqual(i.resolve_frontend(), "direct")
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
    def test_existing_awg_is_upgraded_without_enable_flag(self):
        self.exercise(awg=True)

    def exercise(self, mode='direct', failure=None, active=True, vless=False, migrate=False, second_name=False, awg=False):
        import json
        import ssl
        import subprocess
        from unittest.mock import MagicMock
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            root = Path(tmp)
            for name in ('CONFIG', 'OPT', 'DATA', 'UNIT', 'VLESS_UNIT', 'HOOK', 'SITE', 'ENABLED'):
                stack.enter_context(patch.object(i, name, root / name.lower(), create=True))
            stack.enter_context(patch.object(i, 'WEBROOT', str(root / 'webroot')))
            for folder in (i.CONFIG, i.OPT, i.DATA): folder.mkdir()
            cfg = i.admin_config('lab.example.org')
            if awg: cfg['awg'] = dict(endpoint='lab.example.org:51820', address='10.77.0.1/24')
            if vless: cfg['vless'] = dict(listen='0.0.0.0:9443', endpoint='lab.example.org:9443', security='tls', server_name='lab.example.org', fingerprint='chrome', flow='', mode='standalone', tls_certificate_file='/run/credentials/quic-lab-vless.service/cert.pem', tls_key_file='/run/credentials/quic-lab-vless.service/key.pem')
            if vless and second_name: cfg['vless'].update(server_name='ui.lab.example.org', endpoint='ui.lab.example.org:9443')
            (i.CONFIG / 'admin.json').write_text(json.dumps(cfg))
            (i.CONFIG / 'server.json').write_text(json.dumps(i.server_config(i.DEFAULT_PORTS, mode, 'lab.example.org')))
            (i.CONFIG / 'install.json').write_text(json.dumps(dict(domain='lab.example.org', frontend=mode, custom_cert=True, cert=str(root / 'cert'), key=str(root / 'key'))))
            for name in ('cert', 'key'): (root / name).touch()
            i.UNIT.write_text(i.MARKER); (i.OPT / 'quic-lab-server').write_bytes(b'old')
            candidate = root / 'candidate'; candidate.write_bytes(b'new')
            initial_identity = json.dumps(dict(vless=cfg['vless'], vless_revision=8)).encode() if vless else b'synthetic-users-devices-key'
            identity = i.DATA / 'identities.json'; identity.write_bytes(initial_identity); identity.chmod(0o600)
            worker_candidate = root / 'quic-lab-vless'; worker_candidate.write_bytes(b'new-worker')
            if vless:
                i.VLESS_UNIT.write_text(i.MARKER)
                (i.OPT / 'quic-lab-vless').write_bytes(b'old-worker')
                (i.DATA / 'vless').mkdir(mode=0o700)
                (i.DATA / 'vless/vless-state.json').write_bytes(b'revision-8-tombstones')
            before = {p.name:p.read_bytes() for p in i.CONFIG.iterdir()}
            if failure == 'snapshot':
                identity.unlink(); identity.symlink_to(root / 'cert')
            calls = []
            def run(*args, **kwargs):
                calls.append(args)
                if '-check-config' in args and failure == 'validation': raise RuntimeError('invalid')
                if '-check-config' in args and 'quic-lab-vless' in args[0] and failure == 'worker-validation': raise subprocess.CalledProcessError(1, args)
                if args == ('systemctl', 'restart', 'quic-lab'):
                    self.assertIn(('systemctl', 'stop', 'quic-lab'), calls)
                    snapshot = next(i.CONFIG.glob('backup-*/identities.json'))
                    self.assertEqual(snapshot.read_bytes(), initial_identity)
                    if migrate:
                        migrated = json.loads(identity.read_text())
                        self.assertEqual(migrated['vless']['listen'], '127.0.0.1:9444')
                        self.assertEqual(migrated['vless']['endpoint'], 'ui.lab.example.org:443')
                        self.assertEqual(migrated['vless_revision'], 8)
                        self.assertNotIn('vless_digest', migrated)
                    identity.write_bytes(b'new-schema'); (i.DATA / 'identities.json.bak').write_bytes(b'new-backup')
                    if vless:
                        self.assertIn(('systemctl', 'stop', 'quic-lab-vless'), calls)
                        self.assertLess(calls.index(('systemctl', 'stop', 'quic-lab-vless')), calls.index(('systemctl', 'stop', 'quic-lab')))
                        (i.DATA / 'vless/vless-state.json').write_bytes(b'revision-9-revoked')
                if args == ('systemctl', 'start', 'quic-lab') and failure != 'snapshot':
                    self.assertEqual(identity.read_bytes(), initial_identity)
                    self.assertFalse((i.DATA / 'identities.json.bak').exists())
                return subprocess.CompletedProcess(args, 0, stdout='')
            stack.enter_context(patch.object(i, 'run', side_effect=run))
            if failure == 'sync': stack.enter_context(patch.object(i, 'sync_directory', side_effect=OSError('disk sync failed')))
            stack.enter_context(patch.object(i, 'missing_packages', return_value=[]))
            applied_check = stack.enter_context(patch.object(i, 'vless_applied', return_value=failure != 'vless-apply', create=True))
            stack.enter_context(patch.object(i.shutil, 'which', return_value='/fake/nginx'))
            stack.enter_context(patch.object(ssl.SSLContext, 'load_cert_chain'))
            stack.enter_context(patch.object(i, 'certificate_names', return_value={'lab.example.org', 'ui.lab.example.org'}))
            nginx_apply = stack.enter_context(patch.object(i, 'apply_nginx'))
            if failure == 'port-conflict':
                blocked_socket = stack.enter_context(patch.object(i.socket, 'socket'))
                blocked_socket.return_value.__enter__.return_value.bind.side_effect = OSError('in use')
            stack.enter_context(patch.object(i.time, 'sleep'))
            stack.enter_context(patch.object(i.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0 if active else 3)))
            http = MagicMock(); http.__enter__.return_value.status = 200
            stack.enter_context(patch.object(i.urllib.request, 'urlopen', return_value=http, side_effect=OSError('health') if failure == 'health' else None))
            args = SimpleNamespace(migrate_unified=migrate, vless_server_name='ui.lab.example.org' if migrate else None, vless_port=9444 if migrate else None, domain='lab.example.org', cert=None, key=None, apk=None, binary=candidate, frontend=None, tls_fallback=None, gateway_allow=None, email=None, enable_awg=False, awg_port=None, awg_address=None, enable_vless=False, vless_binary=worker_candidate)
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                if failure:
                    with self.assertRaises((RuntimeError, OSError)): i.install(args)
                else: i.install(args)
            if awg and not failure:
                self.assertTrue(any(len(c)>1 and str(c[1]).endswith('install-awg.py') for c in calls), 'existing AWG worker must be upgraded without enable flag')
            if failure:
                for name, content in before.items(): self.assertEqual((i.CONFIG / name).read_bytes(), content)
                self.assertEqual((i.OPT / 'quic-lab-server').read_bytes(), b'old')
                if failure != 'snapshot': self.assertEqual(identity.read_bytes(), b'new-schema' if vless and failure in ('health', 'vless-apply') else initial_identity)
                if vless and failure in ('health', 'vless-apply'):
                    self.assertEqual((i.DATA / 'vless/vless-state.json').read_bytes(), b'revision-9-revoked')
                    self.assertEqual((i.DATA / 'identities.json.bak').read_bytes(), b'new-backup')
                    self.assertNotIn(('systemctl', 'start', 'quic-lab'), calls)
                    self.assertNotIn(('systemctl', 'start', 'quic-lab-vless'), calls)
                    self.assertEqual((i.OPT / 'quic-lab-vless').read_bytes(), b'old-worker')
                    self.assertTrue((i.CONFIG / 'recovery-required').is_file())
                    for unit in (i.UNIT, i.VLESS_UNIT):
                        gate = unit.with_name(unit.name + '.d') / '90-quic-lab-recovery.conf'
                        self.assertIn('ConditionPathExists=!' + str(i.CONFIG / 'recovery-required'), gate.read_text())
                if failure in ('snapshot', 'sync'):
                    self.assertEqual(('systemctl', 'start', 'quic-lab') in calls, active if failure == 'sync' else False)
                    self.assertNotIn(('systemctl', 'restart', 'quic-lab'), calls)
                    if vless: self.assertEqual(('systemctl', 'start', 'quic-lab-vless') in calls, active if failure == 'sync' else False)
                if failure in ('validation', 'worker-validation', 'snapshot', 'port-conflict'): self.assertNotIn(('systemctl', 'stop', 'quic-lab'), calls)
            else:
                if second_name:
                    self.assertIn('server_name lab.example.org ui.lab.example.org;', nginx_apply.call_args.args[0])
                if vless:
                    self.assertEqual((i.OPT / 'quic-lab-vless').read_bytes(), b'new-worker')
                    applied_check.assert_called()
                    self.assertIn(('systemctl', 'restart', 'quic-lab-vless'), calls)
                self.assertEqual(json.loads((i.CONFIG / 'admin.json').read_text())['password'], cfg['password'])
                self.assertEqual(json.loads((i.CONFIG / 'install.json').read_text())['frontend'], mode)
                self.assertEqual(next(i.CONFIG.glob('backup-*/identities.json')).read_bytes(), initial_identity)
    def test_nginx_final_configuration_retains_both_san_challenge_names(self): self.exercise(mode='nginx', vless=True, second_name=True)
    def test_migration_rejects_busy_public_port_before_stopping_old_workers(self): self.exercise(mode='nginx', vless=True, migrate=True, failure='port-conflict')
    def test_explicit_migration_updates_authoritative_settings_before_restart(self): self.exercise(vless=True, migrate=True)
    def test_vless_unapplied_listener_triggers_recovery(self): self.exercise(failure='vless-apply', vless=True)
    def test_vless_failed_health_preserves_revocations_and_stops_services(self): self.exercise(failure='health', vless=True)
    def test_vless_validation_precedes_all_mutations(self): self.exercise(failure='worker-validation', vless=True)
    def test_vless_upgrade_updates_managed_worker(self): self.exercise(vless=True)
    def test_symlinked_identity_preflight_preserves_active_vless_workers(self): self.exercise(failure='snapshot', vless=True)
    def test_symlinked_identity_preflight_preserves_inactive_vless_workers(self): self.exercise(failure='snapshot', active=False, vless=True)
    def test_durability_failure_never_starts_new_server(self): self.exercise(failure='sync')
    def test_symlinked_identity_preflight_preserves_active_service(self): self.exercise(failure='snapshot')
    def test_symlinked_identity_preflight_preserves_stopped_service(self): self.exercise(failure='snapshot', active=False)
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

class VLESSPackagingTests(unittest.TestCase):
    def test_applied_check_uses_current_revision_without_secrets(self):
        import json
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            (data / 'identities.json').write_text(json.dumps(dict(vless_revision=9)))
            with patch.object(i.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)) as run:
                self.assertTrue(i.vless_applied(Path('/worker'), data))
                self.assertEqual(run.call_args.args[0][-7:], ['--', '/worker', '-data-dir', str(data / 'vless'), '-check-applied', '-revision', '9'])
                self.assertIn('--property=User=quic-lab', run.call_args.args[0])
                self.assertIn('--property=DynamicUser=yes', run.call_args.args[0])
                self.assertIn('--property=StateDirectory=quic-lab', run.call_args.args[0])
            with patch.object(i.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)):
                self.assertFalse(i.vless_applied(Path('/worker'), data))
            (data / 'identities.json').write_text('{}')
            with patch.object(i.subprocess, 'run') as run:
                self.assertFalse(i.vless_applied(Path('/worker'), data))
                run.assert_not_called()

    def test_opt_in_uses_free_dedicated_tls_port_and_worker_credentials(self):
        self.assertTrue(hasattr(i, 'vless_settings'), 'missing VLESS configuration resolver')
        with tempfile.TemporaryDirectory() as tmp:
            config, managed = i.vless_settings(SimpleNamespace(enable_vless=True), None, Path(tmp), i.DEFAULT_PORTS, 'nginx', 'lab.example.org')
            self.assertTrue(managed)
            self.assertEqual(config['listen'], '0.0.0.0:9443')
            self.assertEqual(config['endpoint'], 'lab.example.org:9443')
            self.assertEqual(config['tls_key_file'], '/run/credentials/quic-lab-vless.service/key.pem')
            self.assertEqual(config['tls_certificate_file'], '/run/credentials/quic-lab-vless.service/cert.pem')
            self.assertEqual(config['security'], 'tls')
            self.assertEqual(config['mode'], 'standalone')
            self.assertEqual(i.vless_settings(SimpleNamespace(), None, Path(tmp), i.DEFAULT_PORTS, 'nginx', 'lab.example.org'), (None, False))

    def test_persisted_ui_settings_override_initial_admin_config(self):
        import json
        self.assertTrue(hasattr(i, 'vless_settings'), 'missing VLESS configuration resolver')
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            saved = dict(listen='0.0.0.0:10443', security='reality')
            (data / 'identities.json').write_text(json.dumps(dict(vless=saved, vless_revision=4)))
            config, managed = i.vless_settings(SimpleNamespace(enable_vless=True), dict(vless=dict(listen='0.0.0.0:8443')), data, i.DEFAULT_PORTS, 'nginx', 'lab.example.org')
            self.assertEqual(config, saved)
            self.assertTrue(managed)
            (data / 'identities.json').write_text(json.dumps(dict(vless=None, vless_revision=5)))
            self.assertEqual(i.vless_settings(SimpleNamespace(enable_vless=True), dict(vless=saved), data, i.DEFAULT_PORTS, 'nginx', 'lab.example.org'), (None, True))

    def test_listener_conflict_rejected_before_installation(self):
        self.assertTrue(hasattr(i, 'vless_settings'), 'missing VLESS configuration resolver')
        with tempfile.TemporaryDirectory() as tmp:
            for port in (443, 8443, 8083):
                with self.subTest(port=port), self.assertRaises(ValueError):
                    i.vless_settings(SimpleNamespace(enable_vless=True, vless_port=port), None, Path(tmp), i.DEFAULT_PORTS, 'nginx', 'lab.example.org')

    def test_unit_uses_shared_nonroot_user_private_state_and_credentials(self):
        self.assertTrue(hasattr(i, 'vless_unit_config'), 'missing VLESS service')
        unit = i.vless_unit_config('/cert.pem', '/key.pem', dict(listen='0.0.0.0:9443'))
        self.assertIn('User=quic-lab\n', unit)
        self.assertIn('DynamicUser=yes\n', unit)
        self.assertIn('StateDirectory=quic-lab\n', unit)
        self.assertIn('UMask=0077\n', unit)
        self.assertIn('ExecStartPre=/usr/bin/install -d -m 0700 /var/lib/quic-lab/vless', unit)
        self.assertIn('LoadCredential=key.pem:/key.pem', unit)
        self.assertIn('-data-dir /var/lib/quic-lab/vless -admission-dir /var/lib/quic-lab', unit)
        self.assertNotIn('User=root', unit)
        self.assertNotIn('CAP_NET_ADMIN', unit)
        self.assertNotIn('AmbientCapabilities', unit)

class ReleaseArchiveTests(unittest.TestCase):
    def test_linux_archive_includes_executable_vless_worker_without_text_conversion(self):
        import os
        import subprocess
        import sys
        import tarfile
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'scripts').mkdir()
            builder = Path(__file__).with_name('build-server-release.py')
            (root / 'scripts/build-server-release.py').write_bytes(builder.read_bytes())
            for name in ('install-transit.py', 'transit-network.py', 'install-awg.py', 'awg-network.py', 'install-server.py', 'reload-certificate.py', 'publish-apk.sh'):
                (root / 'scripts' / name).write_text('fixture\n')
            for folder, names in [('docs', ('install-server.md', 'phase1-upgrade.md', 'server-config.md', 'server-backup.md', 'client-diagnostics.md', 'wireshark-capture.md', 'vless-server.md', 'vless-compatibility.md', 'unified-ingress.md')), ('deploy', ('quic-lab.service', 'quic-lab-public.service')), ('examples', ('server.json',))]:
                (root / folder).mkdir()
                for name in names: (root / folder / name).write_text('fixture\n')
            (root / 'THIRD_PARTY_NOTICES.md').write_text('fixture\n')
            fakebin = root / 'fakebin'; fakebin.mkdir()
            go = fakebin / 'go'
            go.write_text('#!' + sys.executable + '\nimport pathlib, sys\npathlib.Path(sys.argv[sys.argv.index("-o")+1]).write_bytes(b"ELF\\r\\n" + sys.argv[-1].encode())\n')
            go.chmod(0o755)
            subprocess.run([sys.executable, str(root / 'scripts/build-server-release.py'), '--version', 'packaging-test', '--arch', 'amd64'], check=True, stdout=subprocess.DEVNULL, env={**os.environ, 'PATH': str(fakebin) + os.pathsep + os.environ['PATH']})
            archive = root / 'artifacts/server-release/quic-lab-server-packaging-test-linux-amd64.tar.gz'
            with tarfile.open(archive) as tar:
                name = 'quic-lab-server-packaging-test-linux-amd64/quic-lab-vless'
                self.assertIn(name, tar.getnames(), 'server archive is missing the managed VLESS worker')
                self.assertIn('quic-lab-server-packaging-test-linux-amd64/unified-ingress.md', tar.getnames())
                self.assertEqual(tar.getmember(name).mode, 0o755)
                self.assertEqual(tar.extractfile(name).read(), b'ELF\r\n./cmd/vless-server')


class UnifiedIngressTests(unittest.TestCase):
    def test_explicit_unified_migration_preserves_legacy_public_tls_minimum(self):
        previous = dict(listen='0.0.0.0:4433', gateway_quic='0.0.0.0:4434', gateway_https='0.0.0.0:8443', https_listen='0.0.0.0:443', public_tls_min='1.0', public_tls_diagnostics=True)
        result = i.server_config(i.UNIFIED_PORTS, 'direct', 'lab.example.org', previous=previous, args=SimpleNamespace(migrate_unified=True))
        self.assertEqual(result['public_tls_min'], '1.0')
        self.assertTrue(result['public_tls_diagnostics'])
        self.assertEqual(previous['public_tls_min'], '1.0')
    def test_split_upgrade_requires_explicit_migration_and_retains_manual_routes(self):
        state = dict(frontend='direct', ports=i.DEFAULT_PORTS)
        with self.assertRaises(ValueError):
            i.resolve_ports(SimpleNamespace(ingress='unified'), state, frontend='direct')
        with self.assertRaises(ValueError):
            i.resolve_ports(SimpleNamespace(migrate_unified=True, ingress='split'), state, frontend='direct')
        server = dict(tls_host='lab.example.org', tls_routes=[dict(server_names=['manual.example.org'], target='127.0.0.1:10444', proxy_protocol=False)])
        i.configure_vless_route(server, dict(server_name='ui.lab.example.org', listen='127.0.0.1:9444', accept_proxy_protocol=True))
        self.assertEqual(server['tls_routes'][0]['server_names'], ['manual.example.org'])
        with self.assertRaises(ValueError):
            i.configure_vless_route(server, dict(server_name='manual.example.org', listen='127.0.0.1:9444', accept_proxy_protocol=True))

    def test_acme_edits_do_not_mutate_saved_config_preflight_snapshot(self):
        import json
        with tempfile.TemporaryDirectory() as tmp:
            data=Path(tmp)
            saved=dict(listen='0.0.0.0:9443', tls_certificate_file='/manual/cert.pem')
            (data/'identities.json').write_text(json.dumps(dict(vless=saved,vless_revision=8)))
            args=SimpleNamespace()
            desired,_=i.vless_settings(args,None,data,i.DEFAULT_PORTS,'direct','lab.example.org')
            desired['tls_certificate_file']='/run/credentials/quic-lab-vless.service/cert.pem'
            self.assertEqual(args.vless_previous['tls_certificate_file'],'/manual/cert.pem')
    def test_migration_rejects_vless_authentication_changes_during_acme_preflight(self):
        import json
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            newer = dict(vless=dict(security='reality', reality_private_key='synthetic-new-key'), devices={'d': {'vless_uuid': 'same'}})
            identity = data/'identities.json'; identity.write_text(json.dumps(newer))
            before = identity.read_bytes()
            with self.assertRaisesRegex(RuntimeError, 'changed during installation preflight'):
                i.migrate_vless_identity(data, dict(security='tls'), expected=dict(config=dict(security='tls')))
            self.assertEqual(identity.read_bytes(), before)
    def test_dns_preflight_requires_common_resolvable_ipv4(self):
        def record(address): return [(i.socket.AF_INET, i.socket.SOCK_STREAM, 6, '', (address, 80))]
        with patch.object(i.socket, 'getaddrinfo', side_effect=[record('192.0.2.1'), record('192.0.2.2')]):
            with self.assertRaises(RuntimeError): i.validate_acme_dns(['lab.example.org','ui.lab.example.org'])
        with patch.object(i.socket, 'getaddrinfo', side_effect=i.socket.gaierror()):
            with self.assertRaises(RuntimeError): i.validate_acme_dns(['lab.example.org'])
        with patch.object(i.socket, 'getaddrinfo', return_value=record('192.0.2.1')):
            i.validate_acme_dns(['lab.example.org','ui.lab.example.org'])

    def test_real_manual_certificate_san_validation(self):
        with tempfile.TemporaryDirectory() as tmp:
            cert, key = str(Path(tmp)/'cert.pem'), str(Path(tmp)/'key.pem')
            subprocess.run(['openssl','req','-x509','-newkey','ec','-pkeyopt','ec_paramgen_curve:P-256','-nodes','-days','1','-subj','/CN=lab.example.org','-addext','subjectAltName=DNS:lab.example.org,DNS:ui.lab.example.org','-keyout',key,'-out',cert], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            i.validate_certificate_names(cert, ['lab.example.org','ui.lab.example.org'])
            with self.assertRaises(RuntimeError): i.validate_certificate_names(cert, ['unrelated.example.org'])

    def test_standalone_acme_never_stops_an_unrelated_http_server(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = SimpleNamespace(domain='lab.example.org', email=None)
            with patch.object(i,'validate_acme_dns'), patch.object(i.socket,'socket') as sock, patch.object(i,'run') as run:
                sock.return_value.__enter__.return_value.bind.side_effect = OSError('in use')
                with self.assertRaisesRegex(RuntimeError, '--acme-webroot'):
                    i.obtain_certificate(args, str(Path(tmp)/'absent-cert'), str(Path(tmp)/'absent-key'), ['lab.example.org'], 'direct')
                run.assert_not_called()
    def test_fresh_defaults_share_tcp_udp_443_even_with_nginx_installed(self):
        with patch.object(i.shutil, 'which', return_value='/usr/sbin/nginx'):
            self.assertEqual(i.resolve_frontend(), 'direct')
        ports = i.resolve_ports(SimpleNamespace(), frontend='direct')
        self.assertEqual(ports, dict(echo_quic_port=443, vpn_quic_port=443, mtls_port=443))
        cfg = i.server_config(ports, 'direct', 'lab.example.org')
        self.assertEqual(cfg['listen'], cfg['gateway_quic'])
        self.assertEqual(cfg['https_listen'], cfg['gateway_https'])

    def test_upgrade_retains_split_ports_until_explicit_migration(self):
        cfg = i.admin_config('lab.example.org')
        state = dict(frontend='direct', ports=dict(echo_quic_port=4433, vpn_quic_port=4434, mtls_port=8443))
        self.assertEqual(i.resolve_ports(SimpleNamespace(), state, cfg, 'direct'), state['ports'])
        ports = i.resolve_ports(SimpleNamespace(migrate_unified=True), state, cfg, 'direct')
        self.assertEqual(ports, dict(echo_quic_port=443, vpn_quic_port=443, mtls_port=443))
        previous = i.server_config(state['ports'], 'direct', 'lab.example.org')
        previous.update(tls_fallback='127.0.0.1:9443', gateway_allow='10.0.0.0/8')
        changed = i.server_config(ports, 'direct', 'lab.example.org', previous=previous, args=SimpleNamespace(migrate_unified=True))
        self.assertEqual(changed['listen'], '0.0.0.0:443')
        self.assertEqual(changed['gateway_quic'], '0.0.0.0:443')
        self.assertEqual(changed['gateway_https'], '0.0.0.0:443')
        self.assertEqual(changed['tls_fallback'], '127.0.0.1:9443')
        self.assertEqual(changed['gateway_allow'], '10.0.0.0/8')

    def test_unified_vless_uses_private_backend_and_distinct_public_sni(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = SimpleNamespace(enable_vless=True, ingress='unified', vless_server_name='ui.lab.example.org', vless_port=9444)
            cfg, managed = i.vless_settings(args, None, Path(tmp), dict(echo_quic_port=443, vpn_quic_port=443, mtls_port=443), 'direct', 'lab.example.org')
            self.assertTrue(managed)
            self.assertEqual(cfg['listen'], '127.0.0.1:9444')
            self.assertEqual(cfg['endpoint'], 'ui.lab.example.org:443')
            self.assertEqual(cfg['server_name'], 'ui.lab.example.org')
            self.assertTrue(cfg['accept_proxy_protocol'])
            server = i.server_config(dict(echo_quic_port=443, vpn_quic_port=443, mtls_port=443), 'direct', 'lab.example.org')
            i.configure_vless_route(server, cfg)
            self.assertEqual(server['tls_routes'], [dict(server_names=['ui.lab.example.org'], target='127.0.0.1:9444', proxy_protocol=True)])
            server['vpn_sni_names'] = ['ui.lab.example.org']
            with self.assertRaises(ValueError): i.configure_vless_route(server, cfg)

    def test_saved_vless_settings_only_migrate_explicitly_preserving_authentication(self):
        import json
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            saved = dict(listen='0.0.0.0:10443', endpoint='lab.example.org:10443', server_name='lab.example.org', security='tls', flow='xtls-rprx-vision', fingerprint='chrome', mode='standalone')
            (data / 'identities.json').write_text(json.dumps(dict(vless=saved, vless_revision=8)))
            cfg, _ = i.vless_settings(SimpleNamespace(), None, data, i.DEFAULT_PORTS, 'direct', 'lab.example.org')
            self.assertEqual(cfg, saved)
            cfg, _ = i.vless_settings(SimpleNamespace(migrate_unified=True, vless_server_name='ui.lab.example.org', vless_port=9444), None, data, dict(echo_quic_port=443, vpn_quic_port=443, mtls_port=443), 'direct', 'lab.example.org')
            self.assertEqual(cfg['listen'], '127.0.0.1:9444')
            self.assertEqual(cfg['endpoint'], 'ui.lab.example.org:443')
            self.assertEqual(cfg['flow'], saved['flow'])
            self.assertEqual(cfg['security'], saved['security'])
            (data / 'identities.json').write_text(json.dumps(dict(vless=None, vless_revision=9)))
            cfg, managed = i.vless_settings(SimpleNamespace(migrate_unified=True, enable_vless=True, vless_server_name='ui.lab.example.org'), None, data, i.DEFAULT_PORTS, 'direct', 'lab.example.org')
            self.assertIsNone(cfg)
            self.assertTrue(managed)

    def test_offline_vless_migration_changes_authoritative_state_without_rotating_identities(self):
        import json
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            state = dict(version=2, ca='preserved', ca_key='preserved-key', users={'u': {'name': 'unchanged'}}, devices={'d': {'vless_uuid': 'fixed-uuid', 'vless_revoked': True}}, vless=dict(listen='0.0.0.0:10443'), vless_revision=8, vless_digest='old-digest')
            identity = data / 'identities.json'
            identity.write_text(json.dumps(state)); identity.chmod(0o600)
            before = identity.stat()
            worker = data / 'vless'; worker.mkdir()
            tombstones = worker / 'vless-state.json'; tombstones.write_bytes(b'permanent-revocations')
            desired = dict(listen='127.0.0.1:9444', endpoint='ui.lab.example.org:443', accept_proxy_protocol=True)
            i.migrate_vless_identity(data, desired)
            migrated = json.loads(identity.read_text())
            self.assertEqual(migrated['vless'], desired)
            self.assertEqual(migrated['vless_revision'], 8)
            self.assertNotIn('vless_digest', migrated)
            for name in ('ca', 'ca_key', 'users', 'devices'): self.assertEqual(migrated[name], state[name])
            self.assertEqual((identity.stat().st_uid, identity.stat().st_gid), (before.st_uid, before.st_gid))
            self.assertEqual(identity.stat().st_mode & 0o777, 0o600)
            self.assertEqual(tombstones.read_bytes(), b'permanent-revocations')
            identity.unlink(); identity.symlink_to(tombstones)
            with self.assertRaises(RuntimeError): i.migrate_vless_identity(data, desired)

    def test_acme_repairs_expired_matching_san_certificate(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); cert=root/'cert.pem'; cert.touch(); key=root/'key.pem'; key.touch()
            names=['lab.example.org','ui.lab.example.org']
            args=SimpleNamespace(domain='lab.example.org', email=None, acme_webroot=str(root))
            with patch.object(i, 'certificate_names', return_value=set(names)), patch.object(i.subprocess, 'run', return_value=subprocess.CompletedProcess([],1)), patch.object(i,'validate_acme_dns'), patch.object(i,'run') as run:
                i.obtain_certificate(args,str(cert),str(key),names,'direct')
                commands=[call.args for call in run.call_args_list if call.args[0]=='certbot']
                self.assertEqual(len(commands),1)
    def test_acme_san_expands_existing_certificate_without_interaction(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); cert = root / 'cert.pem'; cert.touch()
            key = root / 'key.pem'; key.touch()
            args = SimpleNamespace(domain='lab.example.org', email=None, acme_webroot=str(root))
            with patch.object(i, 'certificate_names', return_value={'lab.example.org'}), patch.object(i, 'validate_acme_dns') as dns, patch.object(i, 'run') as run:
                i.obtain_certificate(args, str(cert), str(key), ['lab.example.org', 'ui.lab.example.org'], 'direct')
                calls = [c.args for c in run.call_args_list if c.args[0] == 'certbot']
                self.assertEqual(len(calls), 1)
                command = calls[0]
                self.assertIn('--expand', command)
                self.assertIn('--non-interactive', command)
                self.assertEqual(command[command.index('--cert-name')+1], 'lab.example.org')
                self.assertEqual([command[n+1] for n, item in enumerate(command) if item == '-d'], ['lab.example.org', 'ui.lab.example.org'])
                self.assertIn('--webroot', command)
                self.assertNotIn('--standalone', command)
                dns.assert_called_once_with(['lab.example.org', 'ui.lab.example.org'])
            with patch.object(i, 'certificate_names', return_value={'lab.example.org', 'ui.lab.example.org'}), patch.object(i,'certificate_usable',return_value=True), patch.object(i, 'run') as run:
                i.obtain_certificate(args, str(cert), str(key), ['lab.example.org', 'ui.lab.example.org'], 'direct')
                run.assert_not_called()

    def test_manual_certificate_missing_vless_san_is_rejected(self):
        with patch.object(i, 'certificate_names', return_value={'lab.example.org'}):
            with self.assertRaises(RuntimeError): i.validate_certificate_names('/cert.pem', ['lab.example.org', 'ui.lab.example.org'])

    def test_unattended_renewal_hook_only_restarts_affected_managed_services(self):
        import os
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); fake = root / 'systemctl'; calls = root / 'calls'
            fake.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$CALLS"\n'); fake.chmod(0o755)
            hook = root / 'hook'; hook.write_text(i.renewal_hook('lab.example.org', 'direct')); hook.chmod(0o755)
            env = {**os.environ, 'PATH': str(root)+os.pathsep+os.environ['PATH'], 'CALLS': str(calls), 'RENEWED_LINEAGE': '/etc/letsencrypt/live/unrelated.example.org'}
            subprocess.run([str(hook)], check=True, env=env)
            self.assertFalse(calls.exists())
            env['RENEWED_LINEAGE'] = '/etc/letsencrypt/live/lab.example.org'
            subprocess.run([str(hook)], check=True, env=env)
            lines = calls.read_text().splitlines()
            self.assertIn('restart quic-lab', lines)
            self.assertIn('restart quic-lab-vless', lines)
            self.assertFalse(any('nginx' in line or 'xray' in line or 'x-ui' in line for line in lines))


class ManagedRouteUpgradeTests(unittest.TestCase):
    def test_changed_sni_preserves_manual_routes(self):
        old=dict(server_name='old.example.org',listen='127.0.0.1:9444',accept_proxy_protocol=True)
        new=dict(old,server_name='one.example.org',security='reality',reality_server_names=['one.example.org','two.example.org'])
        manual=dict(server_names=['manual.example.org'],target='127.0.0.1:9555')
        server=dict(tls_host='web.example.org',tls_routes=[dict(server_names=['old.example.org'],target=old['listen'],proxy_protocol=True),manual])
        i.configure_vless_route(server,new,previous=old)
        self.assertEqual(server['tls_routes'],[manual,dict(server_names=['one.example.org','two.example.org'],target=new['listen'],proxy_protocol=True)])

if __name__ == '__main__':
    unittest.main()
