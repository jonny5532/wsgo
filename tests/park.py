import time

from .utils import WsgoTestCase


class ParkTests(WsgoTestCase):

    module = 'fixtures.park'

    def _park(self):
        self.start()

        park_response = self.pool.submit(self.get, '/park/start')
        time.sleep(0.1)

        self.assertFalse(park_response.done())

        return park_response

    def test_park_notify(self):
        park_response = self._park()

        self.get('/park/notify-wrong')

        time.sleep(0.1)

        # Check our wrong-channel notification didn't work
        self.assertFalse(park_response.done())

        self.get('/park/notify-204')

        self.assertEqual(park_response.result(timeout=10).status_code, 204)

    def test_park_notify_retry(self):
        park_response = self._park()

        self.get('/park/notify-retry')

        r = park_response.result(timeout=10)
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'retry_arg')

    def test_park_retry_cannot_be_reparked(self):
        """
        A request that was woken as a retry must not be parked again, even
        if the application responds with the park headers.
        """

        self.start()

        park_response = self.pool.submit(self.get, '/park/retry-again', timeout=15)

        time.sleep(0.5)
        self.get('/park/notify-retry-again')

        r = park_response.result(timeout=15)
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'retry-complete')
