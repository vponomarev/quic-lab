"""Unprivileged, bounded client. Never accepts a non-root control server."""
import json
import socket
import struct

SOCKET = '/run/quic-lab-control/control.sock'
ERRORS = {
    'access_denied': 'Этому пользователю не разрешено управление VPN.',
    'unsafe_configuration': 'Профиль или права его файла требуют проверки администратора.',
    'invalid_request': 'Команда не поддерживается установленной службой.',
    'operation_failed': 'Служба не смогла выполнить команду. Проверьте её состояние.',
    'response_too_large': 'Служба вернула слишком большой ответ.',
    'service_unavailable': 'Нет связи со службой управления. Состояние VPN неизвестно.',
    'service_busy': 'Служба выполняет другую операцию. Попробуйте через несколько секунд.',
    'unmanaged_profile': 'Активный профиль настроен вручную. GUI не будет заменять его.',
    'unmanaged_service': 'Служба VPN использует другую команду запуска. Требуется настройка администратором.',
    'start_not_ready': 'VPN не подтвердил запуск за отведённое время.',
    'switch_failed_rolled_back': 'Подключение не удалось. Прежний профиль и его состояние восстановлены.',
    'rollback_failed': 'Подключение и восстановление не удались. Требуется проверка службы.',
    'untrusted_server': 'Не удалось подтвердить подлинность службы управления.',
    'invalid_response': 'Несовместимый ответ службы управления.',
}


class ClientError(Exception):
    pass


def request(value, path=SOCKET):
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as peer:
            peer.settimeout(8 if value.get('op') == 'status' else 150)
            peer.connect(path)
            uid = struct.unpack('3i', peer.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))[1]
            if uid != 0:
                raise ClientError('untrusted_server')
            peer.sendall(json.dumps(value).encode() + b'\n')
            with peer.makefile('rb') as reader:
                raw = reader.readline(65537)
            if len(raw) > 65536 or not raw.endswith(b'\n'):
                raise ClientError('invalid_response')
            reply = json.loads(raw)
            if not isinstance(reply, dict) or type(reply.get('ok')) is not bool:
                raise ClientError('invalid_response')
            if not reply['ok']:
                error = reply.get('error')
                raise ClientError(error if isinstance(error, str) and error in ERRORS else 'invalid_response')
            if not isinstance(reply.get('data'), dict):
                raise ClientError('invalid_response')
            return reply['data']
    except (OSError, TimeoutError):
        raise ClientError('service_unavailable') from None
    except (ValueError, TypeError, RecursionError):
        raise ClientError('invalid_response') from None


if __name__ == '__main__':
    print(json.dumps(request({'op': 'status'}), ensure_ascii=False, indent=2))
