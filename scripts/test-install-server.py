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
    def test_existing_nginx_is_never_installed_or_upgraded(self):
        with patch.object(i.shutil, "which", side_effect=lambda c: "/usr/bin/" + c if c in ("nginx", "openssl") else None), patch.object(i.Path, "is_file", return_value=True):
            self.assertEqual(i.missing_packages(False), ["certbot"])
            self.assertEqual(i.missing_packages(True), [])
        with patch.object(i.shutil, "which", return_value=None), patch.object(i.Path, "is_file", return_value=False):
            self.assertEqual(set(i.missing_packages(False)), {"nginx", "openssl", "certbot", "ca-certificates"})

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
        self.assertIn("-gateway-quic 0.0.0.0:443", unit)
        self.assertIn("-gateway-https 0.0.0.0:9443", unit)
        self.assertIn("AmbientCapabilities=CAP_NET_BIND_SERVICE", unit)
        self.assertEqual(i.resolve_ports(SimpleNamespace(), {"ports": ports}, cfg), ports)
        self.assertEqual(i.resolve_ports(SimpleNamespace(), None, cfg), ports)
        self.assertEqual(i.resolve_ports(SimpleNamespace()), i.DEFAULT_PORTS)
        self.assertNotIn("AmbientCapabilities", i.unit_config("/cert", "/key"))
        self.assertEqual(i.resolve_ports(SimpleNamespace(mtls_port=10443), {"ports": ports}, cfg)["mtls_port"], 10443)

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
        self.assertIn("-https-listen 0.0.0.0:443", i.unit_config("/cert", "/key", ports, "direct"))
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


if __name__ == "__main__":
    unittest.main()
