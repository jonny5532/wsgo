import os
import random
import requests
import subprocess
import sys
import threading
import time
from .utils import WsgoTestCase

class BlockTests(WsgoTestCase):

    def test_block(self):
        """
        Test that blocking works by sending a successful request, initiating a
        2s block, and then sending another request that should be blocked for
        the 2s.
        """

        headers = {
            "X-Forwarded-For": "1.2.3.4",
        }

        self.start('--module', 'wsgi_app', '--process', '1')

        self.assertEqual(requests.get('http://localhost:8000/', headers=headers, timeout=1).status_code, 200)

        block_response = requests.get('http://localhost:8000/block/', headers=headers)
        # Check block didn't error
        self.assertEqual(block_response.status_code, 200)
        # Check the header was intercepted
        self.assertNotIn("X-WSGo-Block", block_response.headers)

        def test_blocked():
            return requests.get('http://localhost:8000/', headers=headers)
        blocked_response = self.pool.submit(test_blocked)

        time.sleep(0.1)

        # Check the request is hanging due to the block
        self.assertEqual(blocked_response.done(), False)

        # Wait for the 2s block to finish
        time.sleep(2)

        # Check the request is now done
        self.assertEqual(blocked_response.done(), True)

        # Check the response was a block
        self.assertEqual(blocked_response.result().status_code, 429)

        # Check that the block has lifted
        self.assertEqual(requests.get('http://localhost:8000/', headers=headers, timeout=1).status_code, 200)

    def test_block_without_deadlock(self):
        """
        Test that issuing a block for a specific IP doesn't hold up requests
        from another IP.
        """

        self.start('--module', 'wsgi_app', '--process', '1')

        # Block 1.2.3.4
        self.assertEqual(
            requests.get('http://localhost:8000/block/', headers={
                "X-Forwarded-For": "1.2.3.4",
            }, timeout=1).status_code, 
            200,
        )

        # A request from a different IP should succeed
        should_succeed = self.pool.submit(lambda: requests.get('http://localhost:8000/', headers={
            "X-Forwarded-For": "2.3.4.5",
        }))
        # But one from the blocked IP should be delayed for a while
        should_delay = self.pool.submit(lambda: requests.get('http://localhost:8000/', headers={
            "X-Forwarded-For": "1.2.3.4",
        }))

        time.sleep(0.5)

        # The request with a different IP shouldn't have been affected
        self.assertTrue(should_succeed.done())
        # But the one for the blocked IP should still be waiting
        self.assertFalse(should_delay.done())
