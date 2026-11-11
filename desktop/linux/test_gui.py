"""Run as an unprivileged user under Xvfb; uses only a fake controller."""
import os
import time
import unittest
import app
from gi.repository import Gtk, Gio, Gdk


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
        application = Gtk.Application(application_id='ru.vpnc.QuicLab.Test', flags=Gio.ApplicationFlags.NON_UNIQUE)
        application.register(None)
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


if __name__=='__main__': unittest.main()
