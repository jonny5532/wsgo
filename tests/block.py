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

        self.start('--module', 'wsgi_app', '--process', '1')

        self.assertEqual(requests.get('http://localhost:8000/', timeout=1).status_code, 200)

        block_response = requests.get('http://localhost:8000/block/')
        # Check block didn't error
        self.assertEqual(block_response.status_code, 200)
        # Check the header was intercepted
        self.assertNotIn("X-WSGo-Block", block_response.headers)

        def test_blocked():
            return requests.get('http://localhost:8000/')
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
        self.assertEqual(requests.get('http://localhost:8000/', timeout=1).status_code, 200)
