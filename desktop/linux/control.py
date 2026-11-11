#!/usr/bin/python3
"""Privileged, local-only control of one installed VPN service. No profile editing."""
import json
import math
import os
from pathlib import Path
import re
import signal
import socket
import socketserver
import stat
import struct
import subprocess
import threading
import time
import uuid

UNIT = 'quic-lab-client.service'
SYSTEMCTL = '/usr/bin/systemctl'
JOURNALCTL = '/usr/bin/journalctl'
VPN = '/usr/local/bin/quic-lab-client'
SOCKET = '/run/quic-lab-control/control.sock'
PROFILES = {'quic': 'QUIC', 'https': 'HTTPS', 'awg': 'AmneziaWG', 'vless': 'VLESS'}
STATES = {'running', 'stopped', 'ready', 'reconnecting'}
READINESS = {'engine_ready', 'vpn_dns_ready', 'checking', 'reconnecting', 'unavailable', 'degraded'}


class ControlError(Exception):
    pass


def run_command(args, timeout=8):
    try:
        result = subprocess.run(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, timeout=timeout, check=False,
            env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C', 'SYSTEMD_PAGER': ''})
        if result.returncode and not (args[0] == JOURNALCTL and result.returncode == 1 and not result.stdout):
            raise ControlError('operation_failed')
        if len(result.stdout) > 2**20:
            raise ControlError('response_too_large')
        return result.stdout.decode('utf-8', errors='replace')
    except (OSError, subprocess.SubprocessError):
        raise ControlError('operation_failed') from None


def private_json(path):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
        with os.fdopen(fd, 'rb') as f:
            s = os.fstat(f.fileno())
            if not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_mode & 0o077:
                raise ControlError('unsafe_configuration')
            raw = f.read(2**20 + 1)
            if len(raw) > 2**20:
                raise ControlError('unsafe_configuration')
            value = json.loads(raw)
            if not isinstance(value, dict):
                raise ControlError('unsafe_configuration')
            return value
    except (OSError, ValueError):
        raise ControlError('unsafe_configuration') from None


def trusted_directory(path):
    try:
        s = path.lstat()
        if not stat.S_ISDIR(s.st_mode) or s.st_uid != 0 or s.st_mode & 0o022:
            raise ControlError('unsafe_configuration')
    except OSError:
        raise ControlError('unsafe_configuration') from None


def public_events(raw, invocation):
    result = {'tx_bytes': 0, 'rx_bytes': 0, 'started': False,
              'readiness': 'unknown', 'session_state': 'unknown', 'events': []}
    if not re.fullmatch('[0-9a-f]{32}', invocation or ''):
        return result
    for line in raw.splitlines()[-100:]:
        try:
            entry = json.loads(line)
            if not isinstance(entry, dict) or entry.get('_SYSTEMD_INVOCATION_ID') != invocation:
                continue
            event = json.loads(entry.get('MESSAGE', ''))
            if not isinstance(event, dict):
                continue
        except (ValueError, TypeError):
            continue
        kind = event.get('event')
        if kind == 'traffic':
            for key in ('tx_bytes', 'rx_bytes'):
                n = event.get(key)
                if type(n) is int and 0 <= n <= 2**63-1:
                    result[key] = n
        state = event.get('state')
        if isinstance(state, str) and state in STATES and kind in (None, 'network'):
            result['session_state'] = state
            result['started'] = state in ('running', 'ready')
            result['events'].append(state)
        ready = event.get('readiness') if kind != 'readiness' else state
        if isinstance(ready, str) and ready in READINESS:
            result['readiness'] = ready
            result['events'].append(ready)
        if kind == 'flow_failed':
            result['events'].append('flow_failed')
        if kind == 'reload' and event.get('status') in ('applied', 'rejected'):
            result['events'].append('reload_' + event['status'])
    result['events'] = result['events'][-20:]
    return result


def public_probe(raw, invocation, now=None):
    now = time.monotonic() if now is None else now
    empty = {'transport_state': 'unknown', 'rtt_ms': None, 'probe_at': None,
             'probe_interval_ms': 1000, 'probe': 'echo'}
    result = dict(empty)
    if not re.fullmatch('[0-9a-f]{32}', invocation or ''):
        return result
    states = {'echo': 'ready', 'health': 'ready', 'connected': 'checking',
              'disconnected': 'unavailable', 'probe_unavailable': 'unavailable'}
    for line in raw.splitlines()[-100:]:
        try:
            entry = json.loads(line)
            if not isinstance(entry, dict) or entry.get('_SYSTEMD_INVOCATION_ID') != invocation:
                continue
            event = json.loads(entry.get('MESSAGE', ''))
            if not isinstance(event, dict) or event.get('event') not in states:
                continue
            # A newer failure/connection replaces any previous successful RTT.
            result = dict(empty)
            at = int(entry.get('__MONOTONIC_TIMESTAMP', '')) / 1000000
            if not math.isfinite(at) or not 0 <= at <= now:
                continue
            interval = event.get('probe_interval_ms', 1000)
            if type(interval) is not int or interval not in (1000, 5000):
                continue
            kind = event['event']
            rtt = event.get('rtt_ms') if kind == 'echo' else None
            if kind == 'echo' and (type(rtt) not in (int, float) or not math.isfinite(rtt) or not 0 <= rtt <= 60000):
                continue
            result.update(transport_state=states[kind], probe_at=at, probe_interval_ms=interval,
                          probe=event.get('probe') if event.get('probe') in ('echo', 'icmp', 'https') else 'echo',
                          rtt_ms=rtt)
            if now - at > max(3 * interval / 1000, 5) and states[kind] in ('ready', 'checking'):
                result.update(transport_state='stale', rtt_ms=None)
        except (ValueError, TypeError, OverflowError):
            continue
    return result


class Controller:
    def __init__(self, root=Path('/etc/quic-lab'), run=run_command):
        self.root, self.run = Path(root), run
        trusted_directory(self.root)
        trusted_directory(self.root / 'profiles')
        cfg = private_json(self.root / 'gui-control.json')
        self.uid = cfg.get('allowed_uid')
        if type(self.uid) is not int or self.uid <= 0:
            raise ControlError('unsafe_configuration')
        self.lock = threading.Lock()

    def _profile(self, name):
        if not isinstance(name, str) or name not in PROFILES:
            raise ControlError('invalid_request')
        trusted_directory(self.root)
        trusted_directory(self.root / 'profiles')
        path = self.root / 'profiles' / (name + '.json')
        cfg = private_json(path)
        if cfg.get('transport') != name:
            raise ControlError('unsafe_configuration')
        return path

    def _selected(self):
        path = self.root / 'client.json'
        try:
            if not path.is_symlink() or path.lstat().st_uid != 0:
                return None
            resolved = path.resolve(strict=True)
            for name in PROFILES:
                if resolved == self.root / 'profiles' / (name + '.json'):
                    return name
        except (OSError, RuntimeError):
            pass
        return None

    def _system(self):
        raw = self.run([SYSTEMCTL, 'show', UNIT, '--no-pager',
            '--property=ActiveState,SubState,UnitFileState,InvocationID,Result,ExecMainStatus,ExecStart'])
        props = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
        if props.get('ActiveState') not in ('active', 'inactive', 'failed', 'activating', 'deactivating', 'reloading'):
            raise ControlError('service_unavailable')
        return props

    def _managed(self, system):
        expected = '{ path=/usr/local/bin/quic-lab-client ; argv[]=/usr/local/bin/quic-lab-client -config /etc/quic-lab/client.json ; ignore_errors=no ;'
        if not system.get('ExecStart', '').startswith(expected) or system['ExecStart'].count('argv[]=') != 1:
            raise ControlError('unmanaged_service')

    def _status(self):
        system = self._system()
        self._managed(system)
        invocation = system.get('InvocationID', '')
        raw = self.run([JOURNALCTL, '-u', UNIT, '-n', '80', '-o', 'json', '--no-pager',
            '--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID'])
        events = public_events(raw, invocation)
        probe = public_probe('', '')
        if re.fullmatch('[0-9a-f]{32}', invocation or ''):
            transitions = self.run([JOURNALCTL, '-u', UNIT, '-n', '30', '-o', 'json', '--no-pager',
                '_SYSTEMD_INVOCATION_ID=' + invocation,
                '--grep="(state|readiness)"', '--reverse',
                '--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID'])
            # journalctl -n with --grep is newest-first; parser applies oldest first.
            state = public_events('\n'.join(reversed(transitions.splitlines())), invocation)
            errors = [e for e in events['events'] if e.startswith(('flow_', 'reload_'))]
            for key in ('started', 'session_state', 'readiness', 'events'):
                events[key] = state[key]
            events['events'] = (events['events'] + errors)[-20:]
            diagnostic = self.run([JOURNALCTL, '-u', UNIT, '-n', '1', '-o', 'json', '--no-pager',
                '_SYSTEMD_INVOCATION_ID=' + invocation, '--reverse',
                '--grep="event"[[:space:]]*:[[:space:]]*"(echo|health|connected|disconnected|probe_unavailable)"',
                '--output-fields=MESSAGE,_SYSTEMD_INVOCATION_ID,__MONOTONIC_TIMESTAMP'])
            probe = public_probe(diagnostic, invocation)
        # Check again: a restart between show and journal must not mix sessions.
        after = self._system()
        if after.get('InvocationID', '') != invocation:
            events = public_events('', '')
            probe = public_probe('', '')
        system, invocation = after, after.get('InvocationID', '')
        if system['ActiveState'] != 'active':
            probe = public_probe('', '')
        profiles = []
        for name, label in PROFILES.items():
            try:
                self._profile(name)
            except ControlError:
                continue
            profiles.append({'id': name, 'label': label})
        return {'selected': self._selected(), 'profiles': profiles,
            'active': system['ActiveState'], 'autostart': system.get('UnitFileState') == 'enabled',
            'invocation': invocation if re.fullmatch('[0-9a-f]{32}', invocation) else '',
            'failed': system.get('Result', 'success') != 'success', **events, **probe}

    def _replace(self, target):
        temporary = self.root / ('.gui-link-' + uuid.uuid4().hex)
        try:
            temporary.symlink_to(target)
            temporary.replace(self.root / 'client.json')
        finally:
            temporary.unlink(missing_ok=True)

    def _start(self):
        state = self._system()
        if state['ActiveState'] == 'failed' or state.get('Result', 'success') != 'success':
            self.run([SYSTEMCTL, 'reset-failed', UNIT])
        self.run([SYSTEMCTL, 'start', UNIT], timeout=35)
        until = time.monotonic() + 30
        while time.monotonic() < until:
            current = self._status()
            if current['active'] == 'active' and current['started']:
                return
            if current['active'] in ('inactive', 'failed'):
                break
            time.sleep(.25)
        raise ControlError('start_not_ready')

    def _connect(self, name, force=False):
        path = self._profile(name)
        old = self._selected()
        if old is None:
            raise ControlError('unmanaged_profile')
        self._profile(old)
        system = self._system()
        if system['ActiveState'] not in ('active', 'inactive', 'failed'):
            raise ControlError('service_busy')
        running = system['ActiveState'] == 'active'
        if running and name == old and not force:
            return
        self.run([VPN, '-validate', '-config', str(path)], timeout=15)
        previous = (self.root / 'client.json').readlink()
        try:
            self.run([SYSTEMCTL, 'stop', UNIT], timeout=35)
            self._replace(path)
            self._start()
        except (ControlError, OSError):
            try:
                self.run([SYSTEMCTL, 'stop', UNIT], timeout=35)
                self._replace(previous)
                if running:
                    self._start()
            except (ControlError, OSError):
                raise ControlError('rollback_failed') from None
            raise ControlError('switch_failed_rolled_back') from None

    def handle(self, uid, request):
        if uid not in (0, self.uid):
            raise ControlError('access_denied')
        if not isinstance(request, dict):
            raise ControlError('invalid_request')
        op = request.get('op')
        fields = {'status': {'op'}, 'connect': {'op', 'profile'},
            'disconnect': {'op'}, 'reconnect': {'op'}, 'autostart': {'op', 'enabled'}}
        if not isinstance(op, str) or op not in fields or set(request) != fields[op]:
            raise ControlError('invalid_request')
        if op == 'connect' and (not isinstance(request['profile'], str) or request['profile'] not in PROFILES):
            raise ControlError('invalid_request')
        if op == 'autostart' and type(request['enabled']) is not bool:
            raise ControlError('invalid_request')
        if not self.lock.acquire(timeout=1):
            raise ControlError('service_busy')
        try:
            if op != 'status':
                self._managed(self._system())
            if op == 'connect':
                self._connect(request['profile'])
            elif op == 'reconnect':
                self._connect(self._selected(), force=True)
            elif op == 'disconnect':
                self.run([SYSTEMCTL, 'stop', UNIT], timeout=35)
            elif op == 'autostart':
                self.run([SYSTEMCTL, 'enable' if request['enabled'] else 'disable', UNIT])
            return self._status()
        finally:
            self.lock.release()


class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        self.request.settimeout(3)
        uid = struct.unpack('3i', self.request.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))[1]
        try:
            if uid not in (0, self.server.controller.uid):
                raise ControlError('access_denied')
            raw = self.rfile.readline(4097)
            if len(raw) > 4096 or not raw.endswith(b'\n'):
                raise ControlError('invalid_request')
            reply = {'ok': True, 'data': self.server.controller.handle(uid, json.loads(raw))}
        except ControlError as e:
            reply = {'ok': False, 'error': str(e)}
        except (ValueError, TypeError, RecursionError):
            reply = {'ok': False, 'error': 'invalid_request'}
        except (OSError, RuntimeError):
            reply = {'ok': False, 'error': 'service_unavailable'}
        try:
            self.wfile.write(json.dumps(reply).encode() + b'\n')
        except OSError:
            pass


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True
    allow_reuse_address = False
    def __init__(self, path, controller):
        self.controller = controller
        self.slots = threading.BoundedSemaphore(8)
        super().__init__(path, Handler)

    def process_request(self, request, address):
        if not self.slots.acquire(blocking=False):
            request.close()
            return
        try:
            super().process_request(request, address)
        except Exception:
            self.slots.release()
            raise

    def process_request_thread(self, request, address):
        try:
            super().process_request_thread(request, address)
        finally:
            self.slots.release()


def main():
    if os.geteuid() != 0:
        raise SystemExit('Control service requires root')
    controller = Controller()
    path = Path(SOCKET)
    trusted_directory(path.parent)
    path.unlink(missing_ok=True)  # dedicated root-owned RuntimeDirectory
    with Server(str(path), controller) as server:
        os.chmod(path, 0o666)  # kernel peer UID authorizes every request
        def stop(*_):
            threading.Thread(target=server.shutdown, daemon=True).start()
        signal.signal(signal.SIGTERM, stop)
        signal.signal(signal.SIGINT, stop)
        server.serve_forever(poll_interval=.25)
    path.unlink(missing_ok=True)


if __name__ == '__main__':
    main()
