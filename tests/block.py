import time

from .utils import WsgoTestCase


class BlockTests(WsgoTestCase):

    module = 'fixtures.block'

    def test_block(self):
        """
        Test that blocking works by sending a successful request, initiating a
        2s block, and then sending another request that should be blocked for
        the 2s.
        """

        headers = {
            "X-Forwarded-For": "1.2.3.4",
        }

        self.start()

        self.assertEqual(self.get('/block/ping', headers=headers, timeout=1).status_code, 200)

        block_response = self.get('/block/start', headers=headers)
        # Check block didn't error
        self.assertEqual(block_response.status_code, 200)
        # Check the header was intercepted
        self.assertNotIn("X-WSGo-Block", block_response.headers)

        blocked_response = self.pool.submit(self.get, '/block/ping', headers=headers)

        time.sleep(0.1)

        # Check the request is hanging due to the block
        self.assertFalse(blocked_response.done())

        # Wait for the 2s block to finish
        time.sleep(2)

        # Check the request is now done
        self.assertTrue(blocked_response.done())

        # Check the response was a block
        self.assertEqual(blocked_response.result().status_code, 429)

        # Check that the block has lifted
        self.assertEqual(self.get('/block/ping', headers=headers, timeout=1).status_code, 200)

    def test_block_without_deadlock(self):
        """
        Test that issuing a block for a specific IP doesn't hold up requests
        from another IP.
        """

        self.start()

        # Block 1.2.3.4
        self.assertEqual(
            self.get('/block/start', headers={
                "X-Forwarded-For": "1.2.3.4",
            }, timeout=1).status_code,
            200,
        )

        # A request from a different IP should succeed
        should_succeed = self.pool.submit(self.get, '/block/ping', headers={
            "X-Forwarded-For": "2.3.4.5",
        })
        # But one from the blocked IP should be delayed for a while
        should_delay = self.pool.submit(self.get, '/block/ping', headers={
            "X-Forwarded-For": "1.2.3.4",
        })

        time.sleep(0.5)

        # The request with a different IP shouldn't have been affected
        self.assertTrue(should_succeed.done())
        # But the one for the blocked IP should still be waiting
        self.assertFalse(should_delay.done())
