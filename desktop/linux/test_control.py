import json
from pathlib import Path
import tempfile
import unittest

from control import Controller, ControlError, public_events, public_probe


class FakeSystem:
    def __init__(self):
        self.active = 'active'
        self.enabled = 'enabled'
        self.invocation = 'a' * 32
        self.fail_start = False
        self.calls = []

    def __call__(self, args, timeout=8):
        self.calls.append(args)
        if args[0].endswith('journalctl'):
            return json.dumps({'_SYSTEMD_INVOCATION_ID': self.invocation,
                'MESSAGE': json.dumps({'state': 'running'})})
        if args[1] == 'show':
            return f'ActiveState={self.active}\nSubState=running\nUnitFileState={self.enabled}\nInvocationID={self.invocation}\nResult=success\nExecMainStatus=0\nExecStart={{ path=/usr/local/bin/quic-lab-client ; argv[]=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json ; ignore_errors=no ; start_time=n/a ; }}\n'
        if args[1] == 'stop':
            self.active = 'inactive'
        if args[1] == 'start':
            if self.fail_start:
                self.fail_start = False
                raise ControlError('operation_failed')
            self.active = 'active'
        if args[1] in ('enable', 'disable'):
            self.enabled = 'enabled' if args[1] == 'enable' else 'disabled'
        return ''


class ControlTests(unittest.TestCase):
    def test_probe_freshness_failures_and_invocation(self):
        def line(event, at=100, token='a'*32):
            return json.dumps({'_SYSTEMD_INVOCATION_ID': token,
                '__MONOTONIC_TIMESTAMP': str(int(at*1000000)), 'MESSAGE': json.dumps(event)})
        sample = {'event':'echo','rtt_ms':12.5,'probe':'echo','probe_interval_ms':1000,'key':'PRIVATE'}
        raw = line(sample)
        value = public_probe(raw, 'a'*32, now=104)
        self.assertEqual(value['transport_state'], 'ready')
        self.assertEqual(value['rtt_ms'], 12.5)
        self.assertNotIn('PRIVATE', json.dumps(value))
        self.assertEqual(public_probe(raw, 'a'*32, now=106)['transport_state'], 'stale')
        self.assertIsNone(public_probe(raw, 'a'*32, now=106)['rtt_ms'])
        self.assertIsNone(public_probe(raw, 'b'*32, now=104)['rtt_ms'])
        self.assertIsNone(public_probe(raw, 'a'*32, now=99)['rtt_ms'])
        for event, expected in [('connected','checking'), ('disconnected','unavailable'), ('probe_unavailable','unavailable')]:
            value = public_probe(raw+'\n'+line({'event':event}, at=101), 'a'*32, now=102)
            self.assertEqual(value['transport_state'], expected)
            self.assertIsNone(value['rtt_ms'])
        sample.update(probe='https', probe_interval_ms=5000)
        self.assertEqual(public_probe(line(sample), 'a'*32, now=114)['transport_state'], 'ready')
        self.assertEqual(public_probe(line(sample), 'a'*32, now=116)['transport_state'], 'stale')
        for invalid in (-1, float('nan'), float('inf'), True, '3', None):
            sample['rtt_ms'] = invalid
            self.assertIsNone(public_probe(line(sample), 'a'*32, now=101)['rtt_ms'])

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'profiles').mkdir(mode=0o700)
        (self.root / 'gui-control.json').write_text('{"allowed_uid":1000}')
        (self.root / 'gui-control.json').chmod(0o600)
        for kind in ('quic', 'https', 'awg', 'vless'):
            p = self.root / 'profiles' / (kind + '.json')
            p.write_text(json.dumps({'transport': kind, 'key': 'DO-NOT-LEAK'}))
            p.chmod(0o600)
        (self.root / 'client.json').symlink_to('profiles/quic.json')
        self.system = FakeSystem()
        self.c = Controller(self.root, run=self.system)

    def test_identity_and_schema(self):
        for request in ({'op': 'shell'}, {'op': 'connect', 'profile': '../../etc/passwd'},
                        {'op': 'status', 'path': '/etc/shadow'}, {'op': 'autostart', 'enabled': 'yes'}, []):
            with self.assertRaises(ControlError):
                self.c.handle(1000, request)
        with self.assertRaises(ControlError):
            self.c.handle(1001, {'op': 'status'})
        self.assertEqual(self.system.calls, [])

    def test_status_never_contains_keys(self):
        value = self.c.handle(1000, {'op': 'status'})
        self.assertEqual(value['selected'], 'quic')
        self.assertEqual(len(value['profiles']), 4)
        self.assertNotIn('DO-NOT-LEAK', json.dumps(value))

    def test_failed_start_restores_previous_running_profile(self):
        self.system.fail_start = True
        with self.assertRaisesRegex(ControlError, 'switch_failed_rolled_back'):
            self.c.handle(1000, {'op': 'connect', 'profile': 'https'})
        self.assertEqual((self.root / 'client.json').readlink(), Path('profiles/quic.json'))
        self.assertEqual(self.system.active, 'active')

    def test_switch_and_autostart(self):
        self.c.handle(1000, {'op': 'connect', 'profile': 'https'})
        self.assertEqual((self.root / 'client.json').resolve(), self.root / 'profiles/https.json')
        self.c.handle(1000, {'op': 'autostart', 'enabled': False})
        self.assertEqual(self.system.active, 'active')
        self.assertEqual(self.system.enabled, 'disabled')
        self.c.handle(1000, {'op': 'disconnect'})
        self.assertEqual(self.system.active, 'inactive')

    def test_unsafe_profile_rejected_before_network_changes(self):
        (self.root / 'profiles/https.json').chmod(0o666)
        with self.assertRaises(ControlError):
            self.c.handle(1000, {'op': 'connect', 'profile': 'https'})
        self.assertFalse(any(a[1] == 'stop' for a in self.system.calls))

    def test_unknown_active_profile_not_overwritten(self):
        p = self.root / 'client.json'
        p.unlink()
        p.write_text('{"key":"DO-NOT-LEAK"}')
        with self.assertRaises(ControlError):
            self.c.handle(1000, {'op': 'connect', 'profile': 'https'})
        self.assertTrue(p.is_file() and not p.is_symlink())

    def test_journal_is_current_and_sanitized(self):
        invocation = 'a' * 32
        def line(event, token=invocation):
            return json.dumps({'MESSAGE': json.dumps(event), '_SYSTEMD_INVOCATION_ID': token})
        raw = '\n'.join([line({'event': 'traffic', 'tx_bytes': 9999, 'rx_bytes': 8888}, 'b'*32),
            line({'event': 'traffic', 'tx_bytes': 10, 'rx_bytes': 20}),
            line({'state': 'running', 'readiness': 'vpn_dns_ready'}),
            line({'event': 'flow_failed', 'error': 'DO-NOT-LEAK'}),
            json.dumps({'MESSAGE': 'PRIVATE KEY DO-NOT-LEAK'})])
        data = public_events(raw, invocation)
        self.assertEqual(data['tx_bytes'], 10)
        self.assertEqual(data['rx_bytes'], 20)
        self.assertTrue(data['started'])
        self.assertNotIn('DO-NOT-LEAK', json.dumps(data))
        self.assertEqual(public_events(raw, '')['tx_bytes'], 0)

    def test_state_survives_traffic_log_rotation(self):
        def runner(args, timeout=8):
            if args[0].endswith('journalctl'):
                event = {'event':'network','state':'reconnecting'} if any(a.startswith('--grep=') for a in args) else {'event':'traffic','tx_bytes':123,'rx_bytes':456}
                return json.dumps({'_SYSTEMD_INVOCATION_ID':'a'*32,'MESSAGE':json.dumps(event)})
            return self.system(args,timeout)
        self.c.run=runner
        self.assertEqual(self.c.handle(1000,{'op':'status'})['session_state'],'reconnecting')

    def test_unmanaged_unit_rejected_before_mutation(self):
        def runner(args,timeout=8):
            return self.system(args,timeout).replace('-config /etc/quic-lab/client.json','-config /etc/other.json')
        self.c.run=runner
        with self.assertRaisesRegex(ControlError,'unmanaged_service'):
            self.c.handle(1000,{'op':'connect','profile':'https'})
        self.assertFalse(any(a[1]=='stop' for a in self.system.calls))

    def test_stop_timeout_attempts_restore(self):
        failed=[False]
        def runner(args,timeout=8):
            value=self.system(args,timeout)
            if args[1]=='stop' and not failed[0]:
                failed[0]=True
                raise ControlError('operation_failed')
            return value
        self.c.run=runner
        with self.assertRaisesRegex(ControlError,'switch_failed_rolled_back'):
            self.c.handle(1000,{'op':'connect','profile':'https'})
        self.assertEqual(self.system.active,'active')
        self.assertEqual((self.root/'client.json').readlink(),Path('profiles/quic.json'))

    def test_degraded_overrides_previous_dns_success(self):
        records=[]
        for state in ('vpn_dns_ready','degraded'):
            records.append(json.dumps({'_SYSTEMD_INVOCATION_ID':'a'*32,
                'MESSAGE':json.dumps({'event':'readiness','state':state})}))
        self.assertEqual(public_events('\n'.join(records),'a'*32)['readiness'],'degraded')


if __name__ == '__main__':
    unittest.main()
