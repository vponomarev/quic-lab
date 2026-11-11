#!/usr/bin/python3
"""GUI never owns the VPN process or private configuration."""
import os
import threading
import gi
gi.require_version('Gtk', '3.0')
from gi.repository import Gtk, GLib, Gio
from client import request, ClientError, ERRORS
from model import Traffic, describe, human_bytes, EVENTS


class Window(Gtk.ApplicationWindow):
    def __init__(self, app):
        super().__init__(application=app, title='QUIC Lab')
        self.set_default_size(620, 640)
        self.set_position(Gtk.WindowPosition.CENTER)
        self.status = None
        self.inflight = False
        self.closed = False
        self.confirming = False
        self.updating = False
        self.rate = Traffic()
        self.profile_ids = []
        header = Gtk.HeaderBar(title='QUIC Lab', subtitle='Управление VPN')
        header.set_show_close_button(True)
        self.set_titlebar(header)
        refresh = Gtk.Button.new_from_icon_name('view-refresh-symbolic', Gtk.IconSize.BUTTON)
        refresh.set_tooltip_text('Обновить состояние')
        refresh.connect('clicked', lambda *_: self.poll())
        header.pack_end(refresh)
        outer = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=16)
        outer.set_border_width(24)
        self.add(outer)
        self.title = Gtk.Label(xalign=0)
        self.title.set_markup('<span size="x-large" weight="bold">Проверка состояния…</span>')
        outer.pack_start(self.title, False, False, 0)
        self.detail = self.label('Подключение к локальной службе управления.')
        outer.pack_start(self.detail, False, False, 0)
        self.current = self.label('Текущий профиль: —')
        outer.pack_start(self.current, False, False, 0)
        row = Gtk.Box(spacing=12)
        row.pack_start(Gtk.Label(label='Подключение', xalign=0), False, False, 0)
        self.profiles = Gtk.ComboBoxText()
        self.profiles.set_hexpand(True)
        self.profiles.connect('changed', lambda *_: self.controls())
        row.pack_start(self.profiles, True, True, 0)
        outer.pack_start(row, False, False, 0)
        actions = Gtk.Box(spacing=12)
        self.toggle = Gtk.Button(label='Подключить')
        self.toggle.get_style_context().add_class('suggested-action')
        self.toggle.connect('clicked', self.change_connection)
        actions.pack_start(self.toggle, True, True, 0)
        self.reconnect = Gtk.Button(label='Переподключить')
        self.reconnect.connect('clicked', lambda *_: self.execute({'op': 'reconnect'}))
        actions.pack_start(self.reconnect, True, True, 0)
        outer.pack_start(actions, False, False, 0)
        self.autostart = Gtk.CheckButton(label='Подключать VPN при загрузке компьютера')
        self.autostart.connect('toggled', self.change_autostart)
        outer.pack_start(self.autostart, False, False, 0)
        outer.pack_start(Gtk.Separator(), False, False, 0)
        self.traffic = self.label('Получено: —    Отправлено: —')
        self.speed = self.label('↓ — /с    ↑ — /с')
        outer.pack_start(self.traffic, False, False, 0)
        outer.pack_start(self.speed, False, False, 0)
        self.error = self.label('')
        self.error.get_style_context().add_class('error')
        self.error.set_no_show_all(True)
        outer.pack_start(self.error, False, False, 0)
        outer.pack_start(Gtk.Label(label='События текущего подключения', xalign=0), False, False, 0)
        self.log = Gtk.TextView(editable=False, cursor_visible=False)
        self.log.set_wrap_mode(Gtk.WrapMode.WORD_CHAR)
        self.log.set_left_margin(10); self.log.set_right_margin(10)
        self.log.set_top_margin(8); self.log.set_bottom_margin(8)
        scroll = Gtk.ScrolledWindow()
        scroll.set_min_content_height(100)
        scroll.set_shadow_type(Gtk.ShadowType.IN)
        scroll.add(self.log)
        outer.pack_start(scroll, True, True, 0)
        footer = self.label('Закрытие окна не отключает VPN. Изменение профиля разрывает текущие соединения.')
        footer.get_style_context().add_class('dim-label')
        outer.pack_start(footer, False, False, 0)
        self.controls()
        self.connect('destroy', self.destroyed)
        self.timer = GLib.timeout_add_seconds(2, self.poll)
        self.poll()

    @staticmethod
    def label(text):
        label = Gtk.Label(label=text, xalign=0)
        label.set_line_wrap(True)
        label.set_max_width_chars(64)
        return label

    def destroyed(self, *_):
        self.closed = True
        GLib.source_remove(self.timer)
        # No disconnect request: systemd owns the running connection.

    def controls(self):
        ready = self.status is not None and not self.inflight
        active = self.status and self.status.get('active') == 'active'
        selected = self.profiles.get_active_id()
        current = self.status and self.status.get('selected')
        transitional = self.status and self.status.get('active') in ('activating', 'deactivating', 'reloading')
        self.toggle.set_label('Отключить' if active and (selected == current or current is None) else 'Переключить' if active else 'Подключить')
        self.toggle.set_sensitive(bool(ready and not transitional and (selected or active)))
        self.profiles.set_sensitive(bool(ready and not transitional))
        self.reconnect.set_sensitive(bool(ready and active and current and not transitional))
        self.autostart.set_sensitive(bool(ready))

    def change_connection(self, *_):
        if self.status is None:
            return
        name = self.profiles.get_active_id()
        active = self.status.get('active') == 'active'
        if active and (name == self.status.get('selected') or self.status.get('selected') is None):
            self.execute({'op': 'disconnect'})
            return
        if active:
            dialog = Gtk.MessageDialog(transient_for=self, modal=True,
                message_type=Gtk.MessageType.QUESTION, buttons=Gtk.ButtonsType.OK_CANCEL,
                text='Переключить VPN-подключение?')
            dialog.format_secondary_text('Текущие соединения будут разорваны. Во время переключения обычный доступ к сети не блокируется.')
            self.confirming = True
            try:
                answer = dialog.run()
            finally:
                dialog.destroy()
                self.confirming = False
            if answer != Gtk.ResponseType.OK:
                return
        if name:
            self.execute({'op': 'connect', 'profile': name})

    def change_autostart(self, button):
        if not self.updating:
            self.execute({'op': 'autostart', 'enabled': button.get_active()})

    def execute(self, command):
        if self.inflight or self.closed:
            return
        self.inflight = True
        self.controls()
        if command['op'] != 'status':
            self.detail.set_text('Применение команды…')
            self.error.hide()
        def work():
            try:
                result, error = request(command), None
            except ClientError as e:
                result, error = None, str(e)
            GLib.idle_add(self.finished, result, error, command['op'])
        threading.Thread(target=work, daemon=True).start()

    def poll(self):
        if not self.closed and not self.confirming:
            self.execute({'op': 'status'})
        return not self.closed

    def finished(self, result, error, op):
        if self.closed:
            return False
        self.inflight = False
        if error:
            self.error.set_text(ERRORS.get(error, ERRORS['service_unavailable']))
            self.error.show()
            if op == 'status':
                self.status = None
                self.rate = Traffic()
                self.title.set_text('Состояние VPN неизвестно')
                self.detail.set_text('Проверьте локальную службу управления. VPN мог продолжить работу.')
                self.traffic.set_text('Получено: —    Отправлено: —')
                self.speed.set_text('↓ — /с    ↑ — /с')
        else:
            previous = self.status and self.status.get('selected')
            self.status = result
            title, detail = describe(result)
            self.title.set_markup('<span size="x-large" weight="bold">'+GLib.markup_escape_text(title)+'</span>')
            self.detail.set_text(detail)
            entries = result.get('profiles', [])
            ids = [p['id'] for p in entries]
            choice = self.profiles.get_active_id()
            if ids != self.profile_ids:
                self.profiles.remove_all()
                for p in entries:
                    self.profiles.append(p['id'], p['label'])
                self.profile_ids = ids
            current = result.get('selected')
            if choice is None or choice == previous:
                choice = current
            if choice in ids:
                self.profiles.set_active_id(choice)
            elif ids:
                self.profiles.set_active(0)
            label = next((p['label'] for p in entries if p['id'] == current), 'настроен вручную')
            self.current.set_text('Текущий профиль: '+label)
            self.updating = True
            self.autostart.set_active(result.get('autostart', False))
            self.updating = False
            tx, rx = result.get('tx_bytes', 0), result.get('rx_bytes', 0)
            up, down = self.rate.update(result.get('invocation', ''), tx, rx)
            self.traffic.set_text('Получено: '+human_bytes(rx)+'    Отправлено: '+human_bytes(tx))
            self.speed.set_text('↓ '+human_bytes(down)+'/с    ↑ '+human_bytes(up)+'/с')
            events = [EVENTS[e] for e in result.get('events', []) if e in EVENTS]
            self.log.get_buffer().set_text('\n'.join(events) or 'Нет новых событий в доступной части журнала.')
            if op != 'status' or self.error.get_text() == ERRORS['service_unavailable']:
                self.error.hide()
        self.controls()
        return False


class App(Gtk.Application):
    def __init__(self):
        super().__init__(application_id='ru.vpnc.QuicLab', flags=Gio.ApplicationFlags.FLAGS_NONE)

    def do_activate(self):
        window = self.get_active_window()
        if window is None:
            window = Window(self)
            window.show_all()
        window.present()


if __name__ == '__main__':
    if os.geteuid() == 0:
        raise SystemExit('Запускайте GUI от обычного пользователя, без sudo.')
    GLib.set_application_name('QUIC Lab')
    App().run(None)
