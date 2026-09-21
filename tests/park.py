import os
import random
import requests
import subprocess
import sys
import threading
import time
from .utils import WsgoTestCase

class ParkTests(WsgoTestCase):

    def _park(self):
        self.start('--module', 'wsgi_app', '--process', '1')

        def park():
            return requests.get('http://localhost:8000/park/park')
        park_response = self.pool.submit(park)

        time.sleep(0.1)

        self.assertEqual(park_response.done(), False)

        return park_response

    def test_park_notify(self):
        park_response = self._park()

        requests.get('http://localhost:8000/park/notify_wrong')

        time.sleep(0.1)

        # Check our wrong-channel notification didn't work
        self.assertEqual(park_response.done(), False)

        requests.get('http://localhost:8000/park/notify_204')

        self.assertEqual(park_response.result().status_code, 204)

    def test_park_notify_retry(self):
        park_response = self._park()

        r = requests.get('http://localhost:8000/park/notify_retry')

        self.assertEqual(park_response.result().status_code, 200)
        self.assertEqual(park_response.result().text, 'retry_arg')

    def test_park_retry_cannot_be_reparked(self):
        """
        A request that was woken as a retry must not be parked again, even
        if the application responds with the park headers.
        """

        self.start('--module', 'wsgi_app', '--process', '1')

        park_response = self.pool.submit(
            requests.get, 'http://localhost:8000/park/reretry', timeout=15
        )

        time.sleep(0.5)
        requests.get('http://localhost:8000/park/notify_reretry')

        r = park_response.result(timeout=15)
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'retry-complete')
