import time
from concurrent.futures import ThreadPoolExecutor, as_completed

from .utils import WsgoTestCase


class PriorityTests(WsgoTestCase):

    module = 'fixtures.priority'

    # Enough queued requests that priority and arrival order clearly disagree,
    # but few enough to keep the test quick.
    QUEUED = 6

    def test_crawler_requests_are_scheduled_last(self):
        """
        While the queue is contended, crawler requests must be scheduled after
        normal ones, even though they arrive first.

        A single worker is occupied by a blocking request, then a batch of
        crawler requests and a batch of normal requests are queued (crawlers
        first). If the scheduler ignored priority, the crawlers would be
        served first; instead every normal request must be served first.
        """

        self.start('--workers', '1')

        def headers(source, user_agent=None):
            result = {'X-Forwarded-For': source}
            if user_agent is not None:
                result['User-Agent'] = user_agent
            return result

        with ThreadPoolExecutor(max_workers=16) as executor:
            # Occupy the only worker and wait until it is actually running, so
            # the requests below are guaranteed to queue behind it.
            blocker = executor.submit(
                self.get, '/prio/block', headers=headers('192.0.2.1'),
                timeout=30)
            self.assertTrue(self.wait_for_output('PRIO-BLOCK-ENTER', timeout=5))

            # Crawlers are submitted first: priority must override arrival
            # order. Each gets a distinct public source so that per-source
            # rate limiting cannot affect the expected priorities.
            kinds = {}
            for i in range(self.QUEUED):
                kinds[executor.submit(
                    self.get, '/prio/crawler', timeout=30,
                    headers=headers('203.0.113.%d' % (i + 1), 'Googlebot/2.1'),
                )] = 'crawler'
            for i in range(self.QUEUED):
                kinds[executor.submit(
                    self.get, '/prio/normal', timeout=30,
                    headers=headers('198.51.100.%d' % (i + 1)),
                )] = 'normal'

            served_at = {}
            for future in as_completed(kinds, timeout=30):
                served_at[future] = int(future.result().text)

            # Every normal request must be served before any crawler request.
            # The service order is reported by the server in the response body,
            # so this does not depend on the order the client sees replies.
            normal_served = [served_at[f] for f in kinds if kinds[f] == 'normal']
            crawler_served = [served_at[f] for f in kinds if kinds[f] == 'crawler']
            self.assertLess(
                max(normal_served), min(crawler_served),
                'normal served at %r, crawler served at %r' %
                (normal_served, crawler_served),
            )

            self.assertEqual(blocker.result().text, 'BLOCK-DONE')

        # The access log records the priority each job was scheduled with, so
        # check the demotion was applied end to end.
        output = self._wait_for_log_lines()
        normal_lines = [l for l in output.splitlines() if '/prio/normal' in l]
        crawler_lines = [l for l in output.splitlines() if '/prio/crawler' in l]
        self.assertEqual(len(normal_lines), self.QUEUED)
        self.assertEqual(len(crawler_lines), self.QUEUED)
        for line in normal_lines:
            self.assertEqual(line.split()[-1], '1000', line)
        for line in crawler_lines:
            self.assertEqual(line.split()[-1], '-7000', line)

    def _wait_for_log_lines(self):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            output = self.server_output()
            if (output.count('/prio/normal') >= self.QUEUED and
                    output.count('/prio/crawler') >= self.QUEUED):
                break
            time.sleep(0.05)
        return self.server_output()
