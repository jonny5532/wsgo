from concurrent.futures import ThreadPoolExecutor
import glob
import os
import requests
import sys
import tempfile
import threading
import time
import unittest

from .utils import WsgoTestCase

class BasicTests(WsgoTestCase):

    def test_get(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.get('http://localhost:8000')
        self.assertEqual(r.status_code, 200)

    def _disable_test_wait(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        try:
            r = requests.get('http://localhost:8000/wait/', timeout=0.001)
        except requests.exceptions.ReadTimeout as e: pass
        time.sleep(2)
        self.assertNotIn("calling start_response", sys.stdout.getvalue())

    def test_post(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.post('http://localhost:8000', data={
            'key1':'value1 \U0001F600',
            'key2\U0001F600':'value2',
        })
        self.assertEqual(r.status_code, 200)

    def test_huge_post(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.post('http://localhost:8000', data={
            'key1':'value1 \U0001F600',
            'key2\U0001F600':'value2',
        }, files={
            'upload':b'0123456789'*1000000
        })
        self.assertEqual(r.status_code, 200)

    def test_repeated_headers(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.get('http://localhost:8000')
        self.assertEqual(r.cookies['cookie1'], 'cookievalue1')
        self.assertEqual(r.cookies['cookie2'], 'cookievalue2')
        self.assertEqual(r.cookies['cookie3'], 'cookievalue3')

    def test_threading(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        def go(n):
            r = requests.get('http://localhost:8000/output/', timeout=20)
            return len(r.content)==50000000
        with ThreadPoolExecutor(16) as executor:
            self.assertTrue(all(i for i in executor.map(go, range(16), timeout=20)))
    
    def test_caching(self):
        self.start('--module', 'wsgi_app', '--process', '1', '--max-age', '60')
        t = requests.get('http://localhost:8000/time/').text
        time.sleep(0.01)

        # Should have been cached
        self.assertEqual(requests.get('http://localhost:8000/time/').text, t)

        time.sleep(0.01)

        t2 = requests.get(
            'http://localhost:8000/time/',
            headers={'Cookie':'asdf=dsfg'}
        ).text

        # Should not be cached due to Vary: Cookie
        self.assertNotEqual(t, t2)
        
        time.sleep(0.01)
        
        # Should have been cached against cookie
        self.assertEqual(requests.get(
            'http://localhost:8000/time/',
            headers={'Cookie':'asdf=dsfg'}
        ).text, t2)

    def test_cache_ignores_method(self):
        """
        A cached GET response must never be served in response to a different
        HTTP method, as that lets state-changing requests be answered from
        the cache without ever reaching the application.
        """

        self.start('--module', 'wsgi_app', '--process', '1', '--max-age', '60')

        # Cache a GET response
        self.assertEqual(requests.get('http://localhost:8000/method/').text, 'GET')

        # A POST to the same URL must reach the application, not the cache
        r = requests.post('http://localhost:8000/method/')
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.text, 'POST')

        # And the GET entry should still be cached afterwards
        self.assertEqual(requests.get('http://localhost:8000/method/').text, 'GET')

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
            self.start(
                '--module', 'wsgi_app', '--process', '1',
                '--static-map', '/=' + webroot,
                env={'GOGC': 'off'},
            )

            for _ in range(50):
                r = requests.get('http://localhost:8000/file.txt')
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

        self.start(
            '--module', 'wsgi_app', '--process', '1', '--workers', '1',
            '--request-timeout', '1',
            drain=False,
        )

        # Fill the stderr pipe so that the timeout handling below blocks on
        # its log call (the application writes more than the pipe buffer).
        self.assertEqual(
            requests.get('http://localhost:8000/fillstderr/').text, 'filling')
        time.sleep(0.5)

        # The busy loop finishes just after the 1s request timeout expires,
        # so the timer fires but the request itself completes successfully.
        r = requests.get('http://localhost:8000/busy/', timeout=10)
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
            r = requests.get('http://localhost:8000/fast/', timeout=10)
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

        self.start('--module', 'wsgi_app', '--process', '1')

        for path in ('/none/', '/notiterable/'):
            r = requests.get('http://localhost:8000' + path)
            self.assertEqual(r.status_code, 502, path)
            self.assertEqual(r.text, 'Bad Gateway', path)

            # The server must still be usable afterwards
            self.assertEqual(
                requests.get('http://localhost:8000/fast/').text, 'FAST-OK')

    def test_page_cache_respects_size_limit(self):
        """
        Once the page cache exceeds --cache-size, its least recently used
        entries must be evicted. Otherwise an attacker can fill it with
        unique cacheable URLs indefinitely.
        """

        self.start('--module', 'wsgi_app', '--process', '1',
                   '--max-age', '3600', '--cache-size', '100000')

        url = 'http://localhost:8000/biguid/?id=0'
        first = requests.get(url).text
        time.sleep(0.01)
        self.assertEqual(requests.get(url).text, first)

        # Fill the cache with more unique ~11KB responses than fit in the
        # 100KB limit. Each is requested twice so that it has been served,
        # which is what the old pruning logic used to keep entries alive.
        for i in range(1, 31):
            u = 'http://localhost:8000/biguid/?id=%d' % i
            requests.get(u)
            time.sleep(0.01)
            requests.get(u)

        # The first entry must have been evicted by now.
        time.sleep(0.01)
        self.assertNotEqual(requests.get(url).text, first)

    def test_atexit(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        time.sleep(0.5)
        self.stop()
        self.assertIn("atexit was called", sys.stdout.getvalue())

    def test_timeout(self):
        self.start('--module', 'wsgi_app', '--process', '1', '--request-timeout', '2')

        def do(t, u):
            time.sleep(t)
            r = requests.get('http://localhost:8000' + u)
            return r.status_code

        with ThreadPoolExecutor(15) as executor:
            proms = []
            for i in range(10):
                # these should finish in time
                proms.append(executor.submit(do, 1.5, '/wait/'))
            for i in range(5):
                # this should time out whilst the other ones are still running
                proms.append(executor.submit(do, 0, '/wait10/'))

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

        self.start('--module', 'wsgi_app', '--process', '1', '--request-timeout', '1')

        def do():
            r = requests.get('http://localhost:8000/slowlog/', timeout=10)
            return r.status_code

        with ThreadPoolExecutor(5) as executor:
            proms = []
            for i in range(5):
                proms.append(executor.submit(do))

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

        self.start('--module', 'wsgi_app', '--process', '1', '--request-timeout', '2')

        # close won't have been called yet
        self.assertEqual(requests.get('http://localhost:8000/close/').text, '0')
        # close should have been called once already
        self.assertEqual(requests.get('http://localhost:8000/close/').text, '1')
        # close should have been called twice now
        self.assertEqual(requests.get('http://localhost:8000/close/').text, '2')

    def test_thread_local(self):
        """
        Check that thread local storage is working correctly between requests.

        (Using PyGILState_Release instead of PyEval_ReleaseThread was causing
        thread local storage to be reset between requests).

        """

        # Start with only one thread
        self.start('--module', 'wsgi_app', '--process', '1', '--workers', '1', '--request-timeout', '2')

        # Check that thread local is actually retaining state between requests.

        self.assertEqual(requests.get('http://localhost:8000/thread-local/').text, '1')
        self.assertEqual(requests.get('http://localhost:8000/thread-local/').text, '2')
        self.assertEqual(requests.get('http://localhost:8000/thread-local/').text, '3')

    def test_environ_encoding(self):
        """
        As per the WSGI spec, the values in the environ dictionary should be
        encoded as ISO-8859-1, even if they're actually UTF-8.
        """

        self.start('--module', 'wsgi_app', '--process', '1', '--workers', '1', '--request-timeout', '2')

        # Check that the URL gets echoed back correctly
        r = requests.get('http://localhost:8000/echo/Božja')
        self.assertEqual(r.text, '/echo/Božja')

    def test_bad_start_response_headers(self):
        """
        Malformed start_response() headers (non-string values, wrong tuple
        length, or an excessive number of headers) must be rejected without
        crashing the worker process.
        """

        self.start('--module', 'wsgi_app', '--process', '1')

        for path in ['/badheaders/nonstring', '/badheaders/longtuple', '/badheaders/many']:
            r = requests.get('http://localhost:8000' + path, timeout=15)
            self.assertEqual(r.status_code, 502, 'expected 502 for ' + path)

        # The process must still be alive and serving requests.
        r = requests.get('http://localhost:8000/')
        self.assertEqual(r.status_code, 200)

    def test_cache_refreshes_before_expiry(self):
        """
        Once a cached response is within half of --max-age, an incoming
        request should still be served from cache but trigger a background
        refresh, so the following request sees fresh content.
        """

        self.start('--module', 'wsgi_app', '--process', '1', '--workers', '1', '--max-age', '4')

        t0 = requests.get('http://localhost:8000/time/').text

        # Immediately afterwards this is a plain cache hit with no refresh.
        self.assertEqual(requests.get('http://localhost:8000/time/').text, t0)

        # Past half of the max-age: still served from cache...
        time.sleep(2.5)
        self.assertEqual(requests.get('http://localhost:8000/time/').text, t0)

        # ...but a refresh has been queued, so fresh content appears shortly.
        time.sleep(0.5)
        self.assertNotEqual(requests.get('http://localhost:8000/time/').text, t0)
