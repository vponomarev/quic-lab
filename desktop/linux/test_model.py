import unittest
from model import Traffic, describe, human_bytes, latency


class ModelTests(unittest.TestCase):
    def test_transport_health_and_rtt_expire_together(self):
        status = {'active':'active', 'readiness':'vpn_dns_ready', 'transport_state':'ready',
                  'rtt_ms':12.5, 'probe_at':100, 'probe_interval_ms':1000, 'probe':'echo'}
        self.assertIn('12.5', latency(status, now=104))
        self.assertIn('подключён', describe(status, now=104)[0])
        self.assertNotIn('12.5', latency(status, now=106))
        self.assertIn('свежих', describe(status, now=106)[0])
        for key, value in [('active','inactive'), ('session_state','reconnecting'),
                           ('transport_state','unavailable'), ('readiness','unavailable')]:
            self.assertNotIn('12.5', latency(dict(status, **{key:value}), now=104))

    def test_rates_reset_on_reconnect_and_counter_reset(self):
        rate = Traffic()
        self.assertEqual(rate.update('a', 100, 200, 1), (0, 0))
        self.assertEqual(rate.update('a', 300, 600, 3), (100, 200))
        self.assertEqual(rate.update('b', 5000, 9000, 4), (0, 0))
        self.assertEqual(rate.update('b', 10, 20, 5), (0, 0))

    def test_process_is_not_internet_readiness(self):
        self.assertIn('запущен', describe({'active':'active', 'readiness':'engine_ready'})[0])
        self.assertIn('Восстановление', describe({'active':'active','session_state':'reconnecting'})[0])
        self.assertIn('отключён', describe({'active':'inactive'})[0])
        self.assertEqual(human_bytes(1024), '1.0 КиБ')

    def test_unknown_and_unavailable_are_not_healthy(self):
        self.assertIn('недоступен', describe({'active':'active','readiness':'unavailable'})[0])
        self.assertIn('не подтверждена', describe({'active':'active','readiness':'unknown'})[1])


if __name__ == '__main__':
    unittest.main()
