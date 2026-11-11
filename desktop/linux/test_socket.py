"""Actual Unix socket peer credentials, not a mock credential parameter."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import unittest
from control import Server
from client import request


class Fixture:
    uid=65534
    def handle(self, uid, value):
        return {'peer':uid,'command':value['op']}


class SocketTests(unittest.TestCase):
    def test_real_peer_credentials_and_frame_limit(self):
        with tempfile.TemporaryDirectory() as temp:
            os.chmod(temp,0o755)
            path=str(Path(temp)/'control.sock')
            with Server(path,Fixture()) as server:
                os.chmod(path,0o666)
                thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
                try:
                    self.assertEqual(request({'op':'status'},path)['peer'],0)
                    code='import client,json;print(json.dumps(client.request({"op":"status"},'+repr(path)+')))'
                    allowed=subprocess.run(['/usr/bin/python3','-c',code],user=65534,group=65534,extra_groups=[],capture_output=True,text=True)
                    self.assertEqual(allowed.returncode,0,allowed.stderr)
                    self.assertEqual(json.loads(allowed.stdout)['peer'],65534)
                    denied=subprocess.run(['/usr/bin/python3','-c',code],user=65533,group=65533,extra_groups=[],capture_output=True,text=True)
                    self.assertNotEqual(denied.returncode,0)
                    self.assertIn('access_denied',denied.stderr)
                    with socket.socket(socket.AF_UNIX) as peer:
                        peer.connect(path);peer.sendall(b'x'*4097)
                        self.assertEqual(json.loads(peer.recv(4096))['error'],'invalid_request')
                finally:
                    server.shutdown();thread.join(2)


if __name__=='__main__': unittest.main()
