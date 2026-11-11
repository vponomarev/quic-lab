"""Explicit disposable-host test: refuse every existing installation path.

Creates a synthetic VPN executable/unit, never a TUN or a network route.
"""
import json
import os
from pathlib import Path
import subprocess
import time
import unittest


@unittest.skipUnless(os.getenv('QUIC_GUI_INSTALL_TEST')=='1','explicit empty-host install test')
class InstallTests(unittest.TestCase):
    def test_install_control_and_uninstall_preserve_vpn(self):
        source=Path(__file__).resolve().parents[2]
        config=Path('/etc/quic-lab')
        binary=Path('/usr/local/bin/quic-lab-client')
        unit=Path('/etc/systemd/system/quic-lab-client.service')
        owned=[config,binary,unit,Path('/etc/systemd/system/quic-lab-control.service'),
            Path('/usr/local/lib/quic-lab-gui'),Path('/usr/local/bin/quic-lab-gui'),
            Path('/usr/local/share/applications/quic-lab.desktop')]
        for p in owned:
            self.assertFalse(p.exists() or p.is_symlink(), 'Refusing existing path: '+str(p))
        def run(args,**kw):
            return subprocess.run(args,check=True,text=True,capture_output=True,timeout=160,**kw).stdout
        self.assertIn('not-found',run(['systemctl','show','quic-lab-client.service','--property=LoadState']))
        setup=False
        try:
            config.mkdir(mode=0o700);setup=True
            (config/'profiles').mkdir(mode=0o700)
            for name in ('quic','https','awg','vless'):
                p=config/'profiles'/(name+'.json');p.write_text(json.dumps({'transport':name}));p.chmod(0o600)
            (config/'client.json').symlink_to('profiles/quic.json')
            binary.write_text('#!/usr/bin/python3\nimport sys,json,time\n'+
                'if "-validate" in sys.argv: sys.exit(0)\n'+
                'print(json.dumps({"state":"running","readiness":"engine_ready"}),flush=True)\n'+
                'while True:\n print(json.dumps({"event":"traffic","tx_bytes":100,"rx_bytes":200}),flush=True);time.sleep(1)\n')
            binary.chmod(0o755)
            unit.write_text('[Unit]\nDescription=Synthetic GUI install fixture\n[Service]\nExecStart=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json\n[Install]\nWantedBy=multi-user.target\n')
            run(['systemctl','daemon-reload'])
            run(['bash',str(source/'packaging/linux/install-gui.sh'),'nobody'])
            for _ in range(40):
                if Path('/run/quic-lab-control/control.sock').exists(): break
                time.sleep(.1)
            def call(command):
                code='import sys,json;sys.path.insert(0,"/usr/local/lib/quic-lab-gui");from client import request;print(json.dumps(request('+repr(command)+')))'
                return json.loads(run(['/usr/bin/python3','-I','-c',code],user=65534,group=65534,extra_groups=[]))
            self.assertEqual(call({'op':'status'})['active'],'inactive')
            self.assertEqual(call({'op':'connect','profile':'quic'})['active'],'active')
            self.assertEqual(call({'op':'connect','profile':'https'})['selected'],'https')
            self.assertTrue(call({'op':'autostart','enabled':True})['autostart'])
            self.assertFalse(call({'op':'autostart','enabled':False})['autostart'])
            self.assertEqual(call({'op':'disconnect'})['active'],'inactive')
            self.assertEqual(call({'op':'connect','profile':'quic'})['active'],'active')
            before=binary.read_bytes()
            run(['bash',str(source/'packaging/linux/uninstall-gui.sh')])
            self.assertEqual(run(['systemctl','is-active','quic-lab-client.service']).strip(),'active')
            self.assertEqual(binary.read_bytes(),before)
            self.assertTrue((config/'profiles/quic.json').exists())
            self.assertFalse(Path('/usr/local/bin/quic-lab-gui').exists())
        finally:
            if setup:
                subprocess.run(['bash',str(source/'packaging/linux/uninstall-gui.sh')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
                subprocess.run(['systemctl','disable','--now','quic-lab-client.service'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
                unit.unlink(missing_ok=True);binary.unlink(missing_ok=True)
                (config/'client.json').unlink(missing_ok=True)
                for name in ('quic','https','awg','vless'):
                    (config/'profiles'/(name+'.json')).unlink(missing_ok=True)
                (config/'profiles').rmdir();config.rmdir()
                run(['systemctl','daemon-reload'])


if __name__=='__main__': unittest.main()
