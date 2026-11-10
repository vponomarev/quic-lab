#!/usr/bin/env python3
"""Synthetic namespace fixture: real Echo payloads and a real router TTL error."""
import socket, struct, sys, time

def checksum(data):
    if len(data) % 2:
        data += b'\0'
    total = sum(struct.unpack('!%dH' % (len(data) // 2), data))
    while total >> 16:
        total = (total & 65535) + (total >> 16)
    return (~total) & 65535

def probe(target, ttl, size, expected, peer=None):
    payload = bytes((n % 251 for n in range(size)))
    msg = struct.pack('!BBHHH', 8, 0, 0, 0x514c, size) + payload
    msg = msg[:2] + struct.pack('!H', checksum(msg)) + msg[4:]
    with socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_ICMP) as sock:
        sock.setsockopt(socket.IPPROTO_IP, socket.IP_TTL, ttl)
        sock.settimeout(3)
        sock.sendto(msg, (target, 0))
        end = time.monotonic() + 3
        while time.monotonic() < end:
            try:
                reply, source = sock.recvfrom(4096)
            except socket.timeout:
                if expected is None:
                    return
                raise
            offset = (reply[0] & 15) * 4
            body = reply[offset:]
            if len(body) < 8 or body[0] == 8:
                continue
            assert expected is not None, 'unsupported transport fabricated an ICMP response'
            assert body[0] == expected and checksum(body) == 0, reply.hex()
            assert source[0] == (peer or target), source
            if expected == 0:
                assert body[4:] == msg[4:], 'Echo identifier/sequence/payload changed'
            else:
                quote = body[8:]
                qlen = (quote[0] & 15) * 4
                assert quote[8] == ttl and socket.inet_ntoa(quote[16:20]) == target
                assert quote[qlen:qlen+8] == msg[:8], 'router error quotation changed'
            assert reply[8] > 0
            return
        raise AssertionError('no correlated ICMP reply')

kind = sys.argv[1]
if kind.startswith('vless'):
    probe('192.0.2.2', 64, 32, None)
    print(kind + ': unsupported ICMP has no fabricated/direct reply PASS', flush=True)
else:
    for size in (32, 1200):
        probe('192.0.2.2', 64, size, 0)
    print(kind + ': real ICMP Echo 32/1200 bytes PASS', flush=True)
    if kind in ('quic', 'https'):
        probe('192.0.2.3', 1, 32, 11, '203.0.113.2')
        print(kind + ': real Time Exceeded with original TTL/quote PASS', flush=True)
