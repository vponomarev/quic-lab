"""Run as an unprivileged user under Xvfb; uses only a fake controller."""
import os
import threading
import time
import unittest
from unittest.mock import patch
import app
from gi.repository import Gtk, Gio, Gdk


application = Gtk.Application(application_id='ru.vpnc.QuicLab.Test', flags=Gio.ApplicationFlags.NON_UNIQUE)
application.register(None)


class GuiTests(unittest.TestCase):
    def test_window_actions_close_and_offline(self):
        calls = []
        data = {'active':'active','selected':'quic','profiles':[{'id':'quic','label':'QUIC'},
            {'id':'https','label':'HTTPS'},{'id':'awg','label':'AmneziaWG'},{'id':'vless','label':'VLESS'}],
            'autostart':True,'tx_bytes':2048,'rx_bytes':32768,'invocation':'a'*32,
            'session_state':'running','readiness':'vpn_dns_ready','events':['running','vpn_dns_ready']}
        offline = [False]
        def fake(command):
            calls.append(command)
            if offline[0]: raise app.ClientError('service_unavailable')
            if command['op']=='disconnect': data['active']='inactive'
            if command['op']=='connect': data['selected']=command['profile'];data['active']='active'
            if command['op']=='autostart': data['autostart']=command['enabled']
            return dict(data)
        app.request = fake
        window = app.Window(application);window.show_all()
        self.addCleanup(window.destroy)
        def settled():
            end=time.monotonic()+3
            while time.monotonic()<end:
                while Gtk.events_pending(): Gtk.main_iteration_do(False)
                if not window.inflight: return
                time.sleep(.01)
            self.fail('GUI worker did not finish')
        settled()
        self.assertEqual(window.toggle.get_label(),'Отключить')
        self.assertTrue(window.toggle.get_sensitive())
        self.assertIn('32.0 КиБ',window.traffic.get_text())
        if os.getenv('GUI_SCREENSHOT'):
            until=time.monotonic()+.15
            while time.monotonic()<until:
                while Gtk.events_pending(): Gtk.main_iteration_do(False)
                time.sleep(.01)
            display=window.get_window()
            Gdk.pixbuf_get_from_window(display,0,0,window.get_allocated_width(),window.get_allocated_height()).savev(os.environ['GUI_SCREENSHOT'],'png',[],[])
        window.toggle.clicked();settled()
        self.assertEqual(data['active'],'inactive')
        window.profiles.set_active_id('https')
        window.toggle.clicked();settled()
        self.assertEqual(data['selected'],'https')
        window.reconnect.clicked();settled()
        self.assertIn({'op':'reconnect'},calls)
        # A confirmation dialog runs its own main loop. Polling must not race it.
        window.confirming=True
        count=len(calls)
        window.poll()
        self.assertEqual(len(calls),count)
        window.confirming=False
        window.autostart.set_active(False);settled()
        self.assertFalse(data['autostart'])
        data['selected']=None
        window.poll();settled()
        self.assertEqual(window.toggle.get_label(),'Отключить')
        offline[0]=True
        window.poll();settled()
        self.assertFalse(window.toggle.get_sensitive())
        self.assertIn('неизвестно',window.title.get_text())
        offline[0]=False
        window.poll();settled()
        count=len(calls)
        window.destroy()
        self.assertEqual(len(calls),count,'closing GUI must not send a command')


class PollInteractionTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.gate = threading.Event()
        self.gate.set()
        self.entered = threading.Event()
        self.fail_poll = False
        self.data = {'active': 'active', 'selected': 'quic',
                     'profiles': [{'id': 'quic', 'label': 'QUIC'}],
                     'autostart': True}
        self.mock = patch.object(app, 'request', self.request)
        self.mock.start()
        self.window = app.Window(application)
        self.window.show_all()
        self.addCleanup(self.cleanup_window)
        self.wait_for(lambda: not self.window.inflight)

    def request(self, command):
        self.calls.append(dict(command))
        if command['op'] == 'status':
            # Return the snapshot from before the user's click.
            result = dict(self.data)
            self.entered.set()
            if not self.gate.wait(3):
                raise app.ClientError('service_unavailable')
            if self.fail_poll:
                raise app.ClientError('service_unavailable')
            return result
        if command['op'] == 'disconnect':
            self.data['active'] = 'inactive'
        if command['op'] == 'autostart':
            self.data['autostart'] = command['enabled']
        return dict(self.data)

    def wait_for(self, condition):
        end = time.monotonic() + 3
        while time.monotonic() < end:
            while Gtk.events_pending():
                Gtk.main_iteration_do(False)
            if condition():
                return
            time.sleep(.01)
        self.fail('GUI did not reach the expected state')

    def cleanup_window(self):
        self.gate.set()
        if not self.window.closed:
            self.wait_for(lambda: not self.window.inflight)
            self.window.destroy()
        self.mock.stop()

    def hold_poll(self):
        self.entered.clear()
        self.gate.clear()
        self.window.poll()
        self.wait_for(self.entered.is_set)

    def test_background_poll_keeps_controls_enabled(self):
        disabled = []
        for widget in (self.window.toggle, self.window.autostart,
                       self.window.profiles, self.window.reconnect):
            widget.connect('notify::sensitive',
                           lambda widget, _: disabled.append(widget) if not widget.get_sensitive() else None)
        for _ in range(3):
            self.hold_poll()
            self.assertTrue(self.window.toggle.get_sensitive())
            self.assertTrue(self.window.autostart.get_sensitive())
            self.gate.set()
            self.wait_for(lambda: not self.window.inflight)
        self.assertEqual(disabled, [])

    def test_disconnect_during_poll_is_not_lost(self):
        self.hold_poll()
        self.window.toggle.clicked()
        self.assertFalse(self.window.toggle.get_sensitive())
        self.window.poll()
        self.window.execute({'op': 'reconnect'})
        self.assertEqual([c for c in self.calls if c['op'] != 'status'], [])
        self.gate.set()
        self.wait_for(lambda: not self.window.inflight)
        self.assertEqual([c for c in self.calls if c['op'] != 'status'], [{'op': 'disconnect'}])
        self.assertEqual(self.window.toggle.get_label(), 'Подключить')

    def test_autostart_during_poll_does_not_revert_or_get_lost(self):
        self.hold_poll()
        values = []
        self.window.autostart.connect('toggled', lambda widget: values.append(widget.get_active()))
        self.window.autostart.set_active(False)
        self.assertFalse(self.window.autostart.get_sensitive())
        self.gate.set()
        self.wait_for(lambda: not self.window.inflight)
        self.assertEqual([c for c in self.calls if c['op'] != 'status'], [{'op': 'autostart', 'enabled': False}])
        self.assertEqual(values, [False], 'stale poll must not revert the checkbox')
        self.assertFalse(self.data['autostart'])
        self.assertTrue(self.window.autostart.get_sensitive())

    def test_failed_poll_still_delivers_pending_action(self):
        self.hold_poll()
        self.fail_poll = True
        self.window.toggle.clicked()
        self.gate.set()
        self.wait_for(lambda: not self.window.inflight)
        self.assertEqual(self.data['active'], 'inactive')
        self.assertFalse(self.window.error.get_visible())

    def test_rtt_expires_even_while_status_request_is_pending(self):
        self.data.update(transport_state='ready', rtt_ms=12.5,
                         probe_at=time.monotonic(), probe_interval_ms=1000, probe='echo')
        self.window.poll()
        self.wait_for(lambda: not self.window.inflight)
        self.assertIn('12.5', self.window.rtt.get_text())
        self.assertIn('подключён', self.window.title.get_text())
        self.hold_poll()
        self.window.status['probe_at'] -= 6
        self.window.poll()
        self.assertEqual(self.window.rtt.get_text(), 'RTT: —')
        self.assertIn('свежих', self.window.title.get_text())

    def test_pending_commands_do_not_keep_old_rtt(self):
        for operation in ('reconnect', 'autostart'):
            with self.subTest(operation=operation):
                self.data.update(transport_state='ready', rtt_ms=12.5,
                                 probe_at=time.monotonic(), probe_interval_ms=1000, probe='echo')
                self.window.poll()
                self.wait_for(lambda: not self.window.inflight)
                self.assertIn('12.5', self.window.rtt.get_text())
                self.gate.clear()
                self.entered.clear()
                def blocked(command):
                    self.entered.set()
                    if not self.gate.wait(3):
                        raise app.ClientError('service_unavailable')
                    return self.request(command)
                with patch.object(app, 'request', blocked):
                    command = {'op':operation}
                    if operation == 'autostart': command['enabled'] = False
                    self.window.execute(command)
                    self.wait_for(self.entered.is_set)
                    if operation == 'reconnect':
                        self.assertEqual(self.window.rtt.get_text(), 'RTT: —')
                        self.assertNotIn('подключён', self.window.title.get_text())
                    self.window.status['probe_at'] -= 6
                    self.window.poll()
                    self.assertEqual(self.window.rtt.get_text(), 'RTT: —')
                    self.assertEqual(self.window.detail.get_text(), 'Применение команды…')
                    self.gate.set()
                    self.wait_for(lambda: not self.window.inflight)


if __name__=='__main__': unittest.main()
