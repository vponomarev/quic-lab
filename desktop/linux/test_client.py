import json
import os
from pathlib import Path
import socket
import tempfile
import threading
import unittest
from client import request, ClientError


class ClientTests(unittest.TestCase):
    def server(self, response):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        path = str(Path(temp.name) / 'socket')
        server = socket.socket(socket.AF_UNIX)
        server.bind(path); server.listen(1)
        self.addCleanup(server.close)
        def worker():
            with server.accept()[0] as peer:
                peer.recv(4096)
                peer.sendall(response)
        t = threading.Thread(target=worker, daemon=True); t.start()
        self.addCleanup(lambda: t.join(2))
        return path

    def test_bounded_response_and_protocol(self):
        for raw in (b'x'*65537, b'{}\n', b'{"ok":true,"data":[]}\n'):
            with self.assertRaises(ClientError):
                request({'op':'status'}, self.server(raw))

    def test_success_and_known_error(self):
        self.assertEqual(request({'op':'status'}, self.server(b'{"ok":true,"data":{"active":"inactive"}}\n')), {'active':'inactive'})
        with self.assertRaisesRegex(ClientError, 'access_denied'):
            request({'op':'status'}, self.server(b'{"ok":false,"error":"access_denied"}\n'))


if __name__ == '__main__':
    unittest.main()
