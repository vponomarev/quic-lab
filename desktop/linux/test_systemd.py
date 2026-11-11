"""Root test host only: temporary service emits fixture events, no network changes."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
import control


@unittest.skipUnless(os.getenv('QUIC_GUI_SYSTEMD_TEST')=='1', 'explicit isolated-host test')
class SystemdTests(unittest.TestCase):
    def test_real_service_start_stop_switch_and_rollback(self):
        original_unit,original_vpn=control.UNIT,control.VPN
        unit='quic-lab-gui-fixture-'+str(os.getpid())+'.service'
        unit_file=Path('/run/systemd/system')/unit
        def system(*args):
            return subprocess.run(['systemctl',*args],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True).stdout
        try:
            with tempfile.TemporaryDirectory(prefix='quic-gui-systemd-') as temp:
                root=Path(temp);(root/'profiles').mkdir(mode=0o700)
                for name in control.PROFILES:
                    path=root/'profiles'/(name+'.json');path.write_text(json.dumps({'transport':name}));path.chmod(0o600)
                (root/'gui-control.json').write_text('{"allowed_uid":65534}')
                (root/'gui-control.json').chmod(0o600)
                (root/'client.json').symlink_to('profiles/quic.json')
                worker=root/'worker.py'
                worker.write_text('import json,time,pathlib,sys\n'+
                    'p=pathlib.Path('+repr(str(root/'client.json'))+').resolve().stem\n'+
                    'if p=="vless": sys.exit(1)\n'+
                    'print(json.dumps({"state":"running","readiness":"engine_ready"}),flush=True)\n'+
                    'flag=pathlib.Path('+repr(str(root/'transition.json'))+')\n'+
                    'while True:\n'+
                    ' if flag.exists():\n  print(flag.read_text(),flush=True);flag.unlink()\n'+
                    ' print(json.dumps({"event":"traffic","tx_bytes":100,"rx_bytes":200}),flush=True);time.sleep(.1)\n')
                unit_file.write_text('[Unit]\nDescription=Temporary GUI test fixture\n[Service]\nExecStart=/usr/bin/python3 '+str(worker)+'\n[Install]\nWantedBy=multi-user.target\n')
                system('daemon-reload')
                control.UNIT,control.VPN=unit,'/usr/bin/true'
                class FixtureController(control.Controller):
                    def _managed(self, props):
                        # This test intentionally runs a synthetic worker, not the VPN executable.
                        assert str(worker) in props['ExecStart']
                c=FixtureController(root)
                self.assertEqual(c.handle(65534,{'op':'status'})['active'],'inactive')
                connected=c.handle(65534,{'op':'connect','profile':'quic'})
                self.assertEqual(connected['active'],'active')
                self.assertEqual(connected['readiness'],'engine_ready')
                for event,key,expected in [({'event':'readiness','state':'degraded'},'readiness','degraded'),
                        ({'event':'network','state':'reconnecting'},'session_state','reconnecting')]:
                    (root/'transition.json').write_text(json.dumps(event))
                    until=time.monotonic()+3
                    while time.monotonic()<until:
                        observed=c.handle(65534,{'op':'status'})
                        if observed[key]==expected: break
                        time.sleep(.1)
                    self.assertEqual(observed[key],expected)
                before=connected['invocation']
                switched=c.handle(65534,{'op':'connect','profile':'https'})
                self.assertEqual(switched['selected'],'https')
                self.assertNotEqual(switched['invocation'],before)
                with self.assertRaisesRegex(control.ControlError,'switch_failed_rolled_back'):
                    c.handle(65534,{'op':'connect','profile':'vless'})
                restored=c.handle(65534,{'op':'status'})
                self.assertEqual(restored['selected'],'https')
                self.assertEqual(restored['active'],'active')
                self.assertTrue(c.handle(65534,{'op':'autostart','enabled':True})['autostart'])
                self.assertFalse(c.handle(65534,{'op':'autostart','enabled':False})['autostart'])
                self.assertEqual(c.handle(65534,{'op':'disconnect'})['active'],'inactive')
        finally:
            subprocess.run(['systemctl','disable','--now',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            unit_file.unlink(missing_ok=True)
            subprocess.run(['systemctl','reset-failed',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            system('daemon-reload')
            control.UNIT,control.VPN=original_unit,original_vpn


if __name__=='__main__': unittest.main()
