"""Display semantics independent of GTK."""
import time


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


def describe(status):
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
