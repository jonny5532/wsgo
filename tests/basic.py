from concurrent.futures import ThreadPoolExecutor
import glob
import os
import requests
import subprocess
import tempfile
import threading
import time

from .utils import WsgoTestCase


class BasicTests(WsgoTestCase):

    def test_get(self):
        self.start()
        self.assertEqual(self.get('/').status_code, 200)

    def test_post(self):
        self.start()
        r = self.post('/', data={
            'key1': 'value1 \U0001F600',
            'key2\U0001F600': 'value2',
        })
        self.assertEqual(r.status_code, 200)

    def test_huge_post(self):
        self.start()
        r = self.post('/', data={
            'key1': 'value1 \U0001F600',
            'key2\U0001F600': 'value2',
        }, files={
            'upload': b'0123456789' * 1000000
        })
        self.assertEqual(r.status_code, 200)

    def test_repeated_headers(self):
        self.start()
        r = self.get('/')
        self.assertEqual(r.cookies['cookie1'], 'cookievalue1')
        self.assertEqual(r.cookies['cookie2'], 'cookievalue2')
        self.assertEqual(r.cookies['cookie3'], 'cookievalue3')

    def test_threading(self):
        self.start()

        def go(n):
            r = self.get('/stream/', timeout=20)
            return len(r.content) == 50000000

        with ThreadPoolExecutor(16) as executor:
            self.assertTrue(all(i for i in executor.map(go, range(16), timeout=20)))

    def test_caching(self):
        self.start('--max-age', '60', module='fixtures.cache')

        t = self.get('/cache/time').text
        time.sleep(0.01)

        # Should have been cached
        self.assertEqual(self.get('/cache/time').text, t)

        time.sleep(0.01)

        t2 = self.get('/cache/time', headers={'Cookie': 'asdf=dsfg'}).text

        # Should not be cached due to Vary: Cookie
        self.assertNotEqual(t, t2)

        time.sleep(0.01)

        # Should have been cached against cookie
        self.assertEqual(
            self.get('/cache/time', headers={'Cookie': 'asdf=dsfg'}).text, t2)

    def test_cache_ignores_method(self):
        """
        A cached GET response must never be served in response to a different
        HTTP method, as that lets state-changing requests be answered from
        the cache without ever reaching the application.
        """

        self.start('--max-age', '60', module='fixtures.cache')

        # Cache a GET response
        self.assertEqual(self.get('/cache/method').text, 'GET')

        # A POST to the same URL must reach the application, not the cache
        r = self.post('/cache/method')
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'POST')

        # And the GET entry should still be cached afterwards
        self.assertEqual(self.get('/cache/method').text, 'GET')

    def test_static_does_not_leak_fds(self):
        """
        Serving a static file must not leak a file descriptor. http.ServeContent
        does not close the io.ReadSeeker it is given, so wsgo must close the
        file itself.
        """

        with tempfile.TemporaryDirectory() as webroot:
            with open(os.path.join(webroot, 'file.txt'), 'w') as f:
                f.write('hello')

            # GOGC=off stops the Go finalizer on os.File from closing the
            # descriptors, making the leak observable.
            self.start('--static-map', '/=' + webroot, env={'GOGC': 'off'})

            for _ in range(50):
                r = self.get('/file.txt')
                self.assertEqual(r.status_code, 200)
                self.assertEqual(r.text, 'hello')

            open_fds = [
                os.readlink(path)
                for path in glob.glob('/proc/%d/fd/*' % self.process.pid)
            ]
            leaked = [path for path in open_fds if path.endswith('file.txt')]
            self.assertEqual(leaked, [])

    def test_stale_timeout_does_not_affect_next_request(self):
        """
        If a request completes at almost exactly the same moment as its
        request timeout fires, the timeout must not be delivered afterwards.
        Delivering it late raises RequestTimeoutException in a later request
        and leaves the worker flagged as stuck (which, with one worker,
        causes the process to be killed).

        The server's stderr pipe is deliberately not drained, and is filled
        by the application, so that the timeout goroutine blocks in its log
        call between the timer expiring and flagging the worker as stuck.
        Once the request has completed successfully we drain the pipe, letting
        the (now stale) timeout run to completion.
        """

        self.start('--workers', '1',
                   '--request-timeout', '1', drain=False, module='fixtures.slow')

        # Fill the stderr pipe so that the timeout handling below blocks on
        # its log call (the application writes more than the pipe buffer).
        self.assertEqual(self.get('/slow/fill-stderr').text, 'filling')
        time.sleep(0.5)

        # The busy loop finishes just after the 1s request timeout expires,
        # so the timer fires but the request itself completes successfully.
        r = self.get('/slow/busy', timeout=10)
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'BUSY-DONE')

        # Drain stderr, letting the now-stale timeout goroutine run to
        # completion.
        def drain():
            try:
                while self.process.stderr.read(65536):
                    pass
            except Exception:
                pass

        threading.Thread(target=drain, daemon=True).start()
        time.sleep(2.0)

        # The stale timeout must not have flagged the worker as stuck, nor be
        # delivered to this request.
        try:
            r = self.get('/slow/fast', timeout=10)
        except requests.exceptions.RequestException as e:
            self.fail('Server was taken down by a stale request timeout: %r' % e)
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'FAST-OK')

    def test_non_iterable_response(self):
        """
        A response which is not iterable (e.g. the application returns None)
        must be reported as a 502 without the response header already having
        been sent, and without crashing the server.
        """

        self.start(module='fixtures.errors')

        for path in ('/errors/none', '/errors/not-iterable'):
            r = self.get(path)
            self.assertEqual(r.status_code, 502, path)
            self.assertEqual(r.text, 'Bad Gateway', path)

            # The server must still be usable afterwards
            self.assertEqual(self.get('/errors/ok').text, 'ERRORS-OK')

    def test_page_cache_respects_size_limit(self):
        """
        Once the page cache exceeds --cache-size, its least recently used
        entries must be evicted. Otherwise an attacker can fill it with
        unique cacheable URLs indefinitely.
        """

        self.start('--max-age', '3600', '--cache-size', '100000', module='fixtures.cache')

        url = '/cache/biguid?id=0'
        first = self.get(url).text
        time.sleep(0.01)
        self.assertEqual(self.get(url).text, first)

        # Fill the cache with more unique ~11KB responses than fit in the
        # 100KB limit. Each is requested twice so that it has been served,
        # which is what the old pruning logic used to keep entries alive.
        for i in range(1, 31):
            u = '/cache/biguid?id=%d' % i
            self.get(u)
            time.sleep(0.01)
            self.get(u)

        # The first entry must have been evicted by now.
        time.sleep(0.01)
        self.assertNotEqual(self.get(url).text, first)

    def test_atexit(self):
        self.start()
        time.sleep(0.5)
        self.stop()
        self.assertTrue(self.wait_for_output('atexit was called'))

    def test_timeout(self):
        self.start('--request-timeout', '2', module='fixtures.slow')

        def do(t, u):
            time.sleep(t)
            return self.get(u).status_code

        with ThreadPoolExecutor(15) as executor:
            proms = []
            for i in range(10):
                # these should finish in time
                proms.append(executor.submit(do, 1.5, '/slow/wait'))
            for i in range(5):
                # this should time out whilst the other ones are still running
                proms.append(executor.submit(do, 0, '/slow/wait-long'))

            self.assertEqual(
                [p.result(timeout=5) for p in proms],
                # 10 successes, 5 timeouts
                [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 502, 502, 502, 502, 502]
            )

    def test_logging_deadlock(self):
        """
        Make requests that will timeout during a logging.error call, and check
        whether they all finish without deadlocking.

        In Python <3.13, a request timeout that occurs during a logging.error
        call can cause a deadlock, because the logger's RLock won't be released
        when the RequestTimeoutException fires.

        wsgo mitigates this by releasing certain locks after unsticking a
        worker.
        """

        self.start('--request-timeout', '1', module='fixtures.slow')

        def do():
            return self.get('/slow/log', timeout=10).status_code

        with ThreadPoolExecutor(5) as executor:
            proms = [executor.submit(do) for _ in range(5)]

            self.assertEqual(
                sorted(p.result() for p in proms),
                # one request succeeds, the rest time out
                [200, 502, 502, 502, 502]
            )

    def test_response_close(self):
        """
        Check that wsgo is calling .close() on the response object after the
        response has finished.
        """

        self.start('--request-timeout', '2', module='fixtures.close')

        # close won't have been called yet
        self.assertEqual(self.get('/close/tracked').text, '0')
        # close should have been called once already
        self.assertEqual(self.get('/close/tracked').text, '1')
        # close should have been called twice now
        self.assertEqual(self.get('/close/tracked').text, '2')

    def test_thread_local(self):
        """
        Check that thread local storage is working correctly between requests.

        (Using PyGILState_Release instead of PyEval_ReleaseThread was causing
        thread local storage to be reset between requests).
        """

        # Start with only one thread
        self.start('--workers', '1', '--request-timeout', '2')

        # Check that thread local is actually retaining state between requests.

        self.assertEqual(self.get('/thread-local/').text, '1')
        self.assertEqual(self.get('/thread-local/').text, '2')
        self.assertEqual(self.get('/thread-local/').text, '3')

    def test_environ_encoding(self):
        """
        As per the WSGI spec, the values in the environ dictionary should be
        encoded as ISO-8859-1, even if they're actually UTF-8.
        """

        self.start('--workers', '1', '--request-timeout', '2')

        # Check that the URL gets echoed back correctly
        r = self.get('/echo/Božja')
        self.assertEqual(r.text, '/echo/Božja')

    def test_bad_start_response_headers(self):
        """
        Malformed start_response() headers (non-string values, wrong tuple
        length, or an excessive number of headers) must be rejected without
        crashing the worker process.
        """

        self.start(module='fixtures.errors')

        for path in ['/errors/bad-header-value', '/errors/bad-header-tuple',
                     '/errors/many-headers']:
            r = self.get(path, timeout=15)
            self.assertEqual(r.status_code, 502, 'expected 502 for ' + path)

        # The process must still be alive and serving requests.
        self.assertEqual(self.get('/errors/ok').status_code, 200)

    def test_cache_refreshes_before_expiry(self):
        """
        Once a cached response is in the last quarter of its lifetime, an
        incoming request should still be served from cache but trigger a
        background refresh, so the following request sees fresh content.
        """

        self.start('--workers', '1', '--max-age', '4', module='fixtures.cache')

        t0 = self.get('/cache/time').text

        # Immediately afterwards this is a plain cache hit with no refresh.
        self.assertEqual(self.get('/cache/time').text, t0)

        # Still before the refresh window.
        time.sleep(2)
        self.assertEqual(self.get('/cache/time').text, t0)

        # In the refresh window: still served from cache...
        time.sleep(1.2)
        r = self.get('/cache/time')
        self.assertEqual(r.text, t0)
        self.assertEqual(r.headers['Age'], '3')

        # ...but a refresh has been queued, so fresh content appears shortly.
        time.sleep(0.5)
        self.assertNotEqual(self.get('/cache/time').text, t0)

    def test_cache_refresh_that_parks(self):
        """
        A background refresh has no client, so a refreshed response that asks
        to park must be dropped rather than parked.
        """

        self.start('--workers', '1', '--max-age', '4', module='fixtures.cache')

        # Keep the connection open, as a browser would, so that the request
        # isn't cancelled before a parked refresh would time out.
        session = requests.Session()
        self.addCleanup(session.close)
        url = self.url('/cache/park-on-refresh')

        self.assertEqual(session.get(url, timeout=10).text, 'cached')
        time.sleep(3.2)
        # Served from cache, and triggers the refresh, which parks.
        self.assertEqual(session.get(url, timeout=10).text, 'cached')
        # Wait past the park timeout, when a parked refresh would respond.
        time.sleep(2)
        # This used to crash the server.
        self.assertIsNone(self.process.poll())
        self.assertNotIn('parked request', self.server_output())

    def test_cache_ignores_params(self):
        self.start('--max-age', '60', module='fixtures.cache')

        # Parameters are only ignored once the application has said to.
        r = self.get('/cache/landing?utm_source=a')
        t = r.text
        self.assertNotIn('X-WSGo-Ignore-Params', r.headers)
        self.assertNotIn('X-WSGo-Vary-Cookies', r.headers)

        r = self.get('/cache/landing?utm_source=b&fbclid=c')
        self.assertEqual(r.text, t)
        self.assertNotIn('X-WSGo-Ignore-Params', r.headers)
        self.assertNotIn('X-WSGo-Vary-Cookies', r.headers)
        self.assertEqual(self.get('/cache/landing').text, t)
        self.assertNotEqual(self.get('/cache/landing?id=1&utm_source=a').text, t)

        # Other paths still use every parameter.
        t2 = self.get('/cache/time?utm_source=a').text
        self.assertNotEqual(self.get('/cache/time?utm_source=b').text, t2)

    def test_cache_coalesces_misses(self):
        """
        Concurrent requests for a cacheable page that isn't in the cache yet
        should share a single response from the application.
        """

        self.start('--max-age', '60', module='fixtures.cache')

        with ThreadPoolExecutor(8) as executor:
            bodies = list(executor.map(lambda _: self.get('/cache/slow').text, range(8)))
        self.assertEqual(len(set(bodies)), 1)

    def test_response_chunks_are_released(self):
        """
        Each yielded chunk must be released as soon as it has been written,
        not retained until the entire response has finished.
        """

        self.start('--workers', '1', module='fixtures.errors')
        r = self.get('/errors/chunk-refcount')
        self.assertEqual(r.text, 'chunk-0chunk-1chunk-2counts:2,2')

    def test_iterator_exception_does_not_leak(self):
        """
        If an application's response iterator raises, the exception must not
        be left pending in the worker's Python thread state.
        """

        self.start('--workers', '1', module='fixtures.errors')

        r = self.get('/errors/iterator-raises')
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'partial')

        self.assertEqual(self.get('/errors/pending-exception').text, 'no')

    def test_close_exception_does_not_leak(self):
        """
        If an application's response .close() raises, the exception must not
        be left pending in the worker's Python thread state.
        """

        self.start('--workers', '1', module='fixtures.errors')

        self.assertEqual(self.get('/errors/close-raises').status_code, 200)

        self.assertEqual(self.get('/errors/pending-exception').text, 'no')

    def test_module_dict_reference_not_over_released(self):
        """
        PyModule_GetDict returns a borrowed reference and must not be
        decref'd. Compare this module's dictionary refcount with an
        equivalent dynamically-created module.
        """

        self.start('--workers', '1', module='fixtures.refcount')

        r = self.get('/')
        ours, theirs = [int(v) for v in r.text.split(',')]
        self.assertEqual(ours, theirs, "module dict refcount was over-released")

    def test_wsgi_input_read_all(self):
        """
        read() with no size argument returns the whole request body.
        """

        self.start('--workers', '1', module='fixtures.body')

        body = b'x' * 1000 + b'y' * 3000000
        r = self.post('/body/read-all', data=body, timeout=20)
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, str(len(body)))

    def test_wsgi_input_read_n(self):
        """
        read(n) returns exactly n bytes until EOF, like a buffered file.
        """

        self.start('--workers', '1', module='fixtures.body')

        body = b'z' * (2 * 1048576 + 123)
        r = self.post('/body/read-n', data=body, timeout=20)
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, '1048576,1048576,123')

    def test_late_input_read_does_not_crash(self):
        """
        Reading wsgi.input after the request has finished must raise a Python
        exception rather than dereferencing a nil reader and crashing.
        """

        self.start('--workers', '1', module='fixtures.body')

        self.assertEqual(self.get('/body/late-read').status_code, 200)

        # The delayed read happens in an application thread after the request.
        time.sleep(1.5)

        # The process must still be alive and serving requests.
        self.assertEqual(self.get('/body/ok').status_code, 200)

    def test_accel_response_is_closed(self):
        """
        X-Sendfile and parked responses don't stream the application's body,
        but the response iterable must still have .close() called on it.
        """

        self.start('--workers', '1', module='fixtures.close')

        # X-Sendfile
        self.assertEqual(self.get('/close/accel-sendfile').status_code, 200)
        self.assertEqual(self.get('/close/state').text, '1')

        # Parked (times out with a 204 after one second)
        self.assertEqual(self.get('/close/accel-park', timeout=10).status_code, 204)
        self.assertEqual(self.get('/close/state').text, '2')

    def test_zero_workers_rejected(self):
        """
        --workers 0 must be rejected at startup rather than serving a process
        whose first request declares every worker stuck.
        """

        self.start('--workers', '0', drain=False)

        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.fail("server started with --workers 0")

        self.assertNotEqual(self.process.returncode, 0)
