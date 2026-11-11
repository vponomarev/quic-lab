"""Display semantics independent of GTK."""
import time
import math


def human_bytes(value):
    amount = max(0, value)
    for suffix in ('Б', 'КиБ', 'МиБ', 'ГиБ', 'ТиБ'):
        if amount < 1024 or suffix == 'ТиБ':
            return f'{amount:.1f} {suffix}' if suffix != 'Б' else f'{amount:.0f} Б'
        amount /= 1024


class Traffic:
    def __init__(self):
        self.previous = None

    def update(self, invocation, tx, rx, now=None):
        now = time.monotonic() if now is None else now
        previous, self.previous = self.previous, (invocation, tx, rx, now)
        if previous is None or not invocation or previous[0] != invocation:
            return (0, 0)
        _, oldtx, oldrx, then = previous
        if now <= then or tx < oldtx or rx < oldrx:
            return (0, 0)
        return ((tx-oldtx)/(now-then), (rx-oldrx)/(now-then))


def transport_state(status, now=None):
    state = status.get('transport_state', 'unknown')
    if state in ('ready', 'checking'):
        at = status.get('probe_at')
        interval = status.get('probe_interval_ms', 1000)
        now = time.monotonic() if now is None else now
        if (type(at) not in (int, float) or not math.isfinite(at) or
                type(interval) is not int or interval not in (1000, 5000) or
                not 0 <= now - at <= max(3 * interval / 1000, 5)):
            return 'stale'
    return state


def latency(status, now=None):
    rtt = status.get('rtt_ms')
    if (status.get('active') != 'active' or status.get('session_state') == 'reconnecting' or
            status.get('readiness') in ('unavailable', 'degraded') or
            transport_state(status, now) != 'ready' or type(rtt) not in (int, float) or
            not math.isfinite(rtt) or not 0 <= rtt <= 60000):
        return 'RTT: —'
    kind = {'echo': 'echo внутри туннеля', 'icmp': 'ICMP через AWG',
            'https': 'HTTPS через VLESS'}.get(status.get('probe'), 'проверка туннеля')
    return f'RTT: {rtt:.1f} мс · {kind}'


def describe(status, now=None):
    active = status.get('active')
    if active == 'failed':
        return ('Ошибка подключения', 'Проверьте профиль и доступность сервера.')
    if active == 'inactive':
        return ('VPN отключён', 'Трафик использует обычные системные маршруты.')
    if active in ('activating', 'reloading'):
        return ('Подключение…', 'Служба запускается.')
    if active == 'deactivating':
        return ('Отключение…', 'Служба освобождает маршруты и сетевые настройки.')
    if status.get('session_state') == 'reconnecting':
        return ('Восстановление VPN…', 'Служба повторяет подключение к серверу.')
    if status.get('readiness') in ('unavailable', 'degraded'):
        return ('VPN недоступен' if status['readiness'] == 'unavailable' else 'Проверка VPN не прошла',
                'Последняя проверка DNS через VPN завершилась ошибкой.')
    transport = transport_state(status, now)
    if transport == 'unavailable':
        return ('Туннель не отвечает', 'Проверка транспорта не прошла. Ожидаем следующего ответа.')
    if transport == 'stale':
        return ('Нет свежих ответов VPN', 'Последнее измерение устарело. Проверяем соединение.')
    if transport == 'checking':
        return ('Проверка VPN…', 'Транспорт запущен. Ожидаем ответа через туннель.')
    if transport == 'ready':
        dns = 'DNS через VPN проверен.' if status.get('readiness') == 'vpn_dns_ready' else 'Проверка DNS через VPN ещё не подтверждена.'
        return ('VPN подключён', 'Туннель отвечает. ' + dns)
    if status.get('readiness') == 'vpn_dns_ready':
        return ('VPN запущен', 'Проверка DNS через VPN успешна.')
    if status.get('readiness') == 'engine_ready':
        return ('VPN запущен', 'Транспорт запущен. Доступность интернета не проверялась.')
    return ('Служба работает', 'Готовность VPN не подтверждена текущим журналом.')


EVENTS = {
    'running': 'VPN запущен', 'stopped': 'VPN остановлен',
    'ready': 'Соединение восстановлено', 'reconnecting': 'Восстановление соединения',
    'engine_ready': 'Транспорт запущен', 'vpn_dns_ready': 'DNS через VPN проверен',
    'checking': 'Проверка соединения', 'unavailable': 'Соединение недоступно',
    'degraded': 'Проверка DNS через VPN не прошла',
    'flow_failed': 'Не удалось открыть одно из соединений',
    'reload_applied': 'Правила применены', 'reload_rejected': 'Обновление правил отклонено',
}
