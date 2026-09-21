import atexit
import ctypes
import hashlib
import logging
import os
import sys
import time
import threading
import wsgo

# Direct access to the Python error indicator, so tests can check that wsgo
# doesn't leak an exception from one request into the next.
_pyerr_occurred = ctypes.pythonapi.PyErr_Occurred
_pyerr_occurred.restype = ctypes.c_void_p

thread_local = threading.local()
close_count = 0

logger = logging.getLogger('test_logger')

accel_close_count = 0

def _fill_stderr():
    # Fill the server's stderr pipe so that a Go log call blocks until the
    # test drains it.
    chunk = b'x' * 65536
    for _ in range(64):
        try:
            os.write(2, chunk)
        except OSError:
            break

def application(environ, start_response):
    if environ['PATH_INFO']=='/fillstderr/':
        threading.Thread(target=_fill_stderr, daemon=True).start()
        start_response('200 OK', [('Content-Type','text/plain')])
        return [b'filling']

    if environ['PATH_INFO']=='/busy/':
        end = time.monotonic() + 1.05
        while time.monotonic() < end:
            pass
        start_response('200 OK', [('Content-Type','text/plain')])
        return [b'BUSY-DONE']

    if environ['PATH_INFO']=='/fast/':
        start_response('200 OK', [('Content-Type','text/plain')])
        return [b'FAST-OK']

    if environ['PATH_INFO']=='/biguid/':
        start_response('200 OK', [
            ('Content-Type','text/plain'),
            ('Cache-Control','max-age=3600'),
        ])
        body = (environ['QUERY_STRING'] + ' ' + str(time.time())).encode('ascii')
        return [body + b'A' * 10000]

    if environ['PATH_INFO']=='/none/':
        # A common application bug: call start_response() but return None
        # instead of an iterable.
        start_response('200 OK', [('Content-Type','text/plain')])
        return None

    if environ['PATH_INFO']=='/notiterable/':
        start_response('200 OK', [('Content-Type','text/plain')])
        return 42

    if environ['PATH_INFO']=='/method/':
        start_response('200 OK', [
            ('Content-Type','text/plain'),
            ('Cache-Control','max-age=60'),
        ])
        return [environ['REQUEST_METHOD'].encode('ascii')]

    if environ['PATH_INFO']=='/read-all/':
        # read() with no arguments must return the whole body.
        data = environ['wsgi.input'].read()
        start_response('200 OK', [('Content-Type','text/plain')])
        return [str(len(data)).encode('ascii')]

    if environ['PATH_INFO']=='/read-n/':
        # read(n) must return exactly n bytes until EOF, like a buffered file.
        lengths = []
        while True:
            data = environ['wsgi.input'].read(1048576)
            lengths.append(len(data))
            if len(data) < 1048576:
                break
        start_response('200 OK', [('Content-Type','text/plain')])
        return [','.join(str(x) for x in lengths).encode('ascii')]

    if environ['PATH_INFO']=='/late-read/':
        # Simulate an application thread that keeps the environ (and thus
        # wsgi.input) alive after the request has finished.
        def late_read(env):
            time.sleep(1.0)
            try:
                env['wsgi.input'].read(10)
            except Exception:
                pass

        threading.Thread(target=late_read, args=(environ,), daemon=True).start()
        start_response('200 OK', [('Content-Type','text/plain')])
        return [b'started']

    if environ['PATH_INFO'] == '/accel-close/sendfile':
        start_response('200 OK', [('X-Sendfile', './wsgi_app.py')])
        return AccelCloseResponse()
    if environ['PATH_INFO'] == '/accel-close/park':
        start_response('200 OK', [
            ('X-WSGo-Park', 'close-channel'),
            ('X-WSGo-Park-Timeout', '1 http-204'),
        ])
        return AccelCloseResponse()
    if environ['PATH_INFO'] == '/accel-close/count':
        start_response('200 OK', [('Content-Type', 'text/plain')])
        return [str(accel_close_count).encode('ascii')]

    if environ['PATH_INFO'].startswith('/park/'):
        return park_testing(environ, start_response)
    if environ['PATH_INFO'].startswith('/block/'):
        return block_testing(environ, start_response)
    if environ['PATH_INFO'].startswith('/badheaders/'):
        return badheaders_testing(environ, start_response)
    if environ['PATH_INFO'] == '/refcount/':
        start_response('200 OK', [('Content-Type', 'text/plain')])
        return RefcountResponse()
    if environ['PATH_INFO'] == '/baditer/':
        start_response('200 OK', [('Content-Type', 'text/plain')])
        return BadIterResponse()
    if environ['PATH_INFO'] == '/close-raise/':
        start_response('200 OK', [('Content-Type', 'text/plain')])
        return RaiseCloseResponse()
    if environ['PATH_INFO'] == '/errored/':
        errored = b'yes' if _pyerr_occurred() else b'no'
        start_response('200 OK', [('Content-Type', 'text/plain')])
        return [errored]

    h = hashlib.md5()
    if environ['REQUEST_METHOD']=='POST':
        n = 0
        while True:
            to_read = 4096
            data = environ['wsgi.input'].read(to_read)
            h.update(data)
            n += len(data)
            if len(data)<to_read:
                break
        assert n>0

    if environ['PATH_INFO']=='/wait/':
        time.sleep(1)

    if environ['PATH_INFO']=='/wait10/':
        for i in range(100):
            time.sleep(0.1)

    if environ['PATH_INFO']=='/slowlog/':
        class SlowString:
            def __repr__(self):
                time.sleep(2)
                return "hello"

        logger.error(
            SlowString(),
        )

    print("calling start_response")

    if environ['PATH_INFO']=='/time/':
        start_response('200 OK', [
            ('Content-Type','text/html'),
            ('Cache-Control','max-age=60'),
            ('Vary','Cookie'),
        ])
        return [("%.3f"%(time.time())).encode('utf-8')]

    start_response('200 OK', [
        ('Content-Type','text/html; charset=utf-8'),
        ('Set-Cookie','cookie1=cookievalue1'),
        ('Set-Cookie','cookie2=cookievalue2'),
        ('Set-Cookie','cookie3=cookievalue3'),
        ('Vary','Cookie'),
    ])

    if environ['PATH_INFO']=='/output/':
        def ret():
            thread_local.name = threading.current_thread().name
            for i in range(100):
                yield b'hello'*100000
                assert thread_local.name == threading.current_thread().name
        return ret()

    if environ['PATH_INFO'].startswith('/echo/'):
        # WSGI environ variables are always delivered as ISO-8859-1 strings even
        # if they are some other encoding internally (like utf-8)
        path_info = environ['PATH_INFO'].encode('iso-8859-1').decode('utf-8')
        return [path_info.encode('utf-8')]

    if environ['PATH_INFO'].startswith('/close/'):
        class MyResponse:
            def __init__(self, data):
                self.data = data

            def __iter__(self):
                return iter(self.data)

            def close(self):
                global close_count
                close_count += 1
        return MyResponse([str(close_count).encode('utf-8')])

    if environ['PATH_INFO'].startswith('/thread-local/'):
        setattr(thread_local, 'count', getattr(thread_local, 'count', 0) + 1)
        return [str(thread_local.count).encode('utf-8')]

    return [h.hexdigest().encode('utf-8')]


def park_testing(environ, start_response):
    if environ['PATH_INFO'] == '/park/park':
        # Is this a retry?
        if 'HTTP_X_WSGO_PARK_ARG' in environ:
            start_response('200 OK', [])
            # Return the arg
            return [environ['HTTP_X_WSGO_PARK_ARG'].encode('ascii')]

        start_response('200 OK', [
            ('Content-Type','text/html'),
            ('X-WSGo-Park', '12345, 12346'),
            ('X-WSGo-Park-Timeout', '6 http-504'),
        ])

        return [b"ignored"]

    if environ['PATH_INFO'] == '/park/notify_204':
        wsgo.notify_parked("12345", wsgo.HTTP_204, "")
        start_response('200 OK', [
            ('Content-Type','text/html'),
        ])
        return [b"notified!"]

    if environ['PATH_INFO'] == '/park/notify_retry':
        wsgo.notify_parked("12345", wsgo.RETRY, "retry_arg")
        start_response('200 OK', [
            ('Content-Type','text/html'),
        ])
        return [b"notified!"]

    if environ['PATH_INFO'] == '/park/reretry':
        # Ask to be parked again, even on a retry. wsgo must ignore the park
        # header on a retry, otherwise the request loops forever.
        start_response('200 OK', [
            ('Content-Type', 'text/html'),
            ('X-WSGo-Park', 'reretry'),
            ('X-WSGo-Park-Timeout', '5 http-504'),
        ])
        if 'HTTP_X_WSGO_PARK_ARG' in environ:
            return [b'retry-complete']
        return [b'first-pass']

    if environ['PATH_INFO'] == '/park/notify_reretry':
        wsgo.notify_parked("reretry", wsgo.RETRY, "some-arg")
        start_response('200 OK', [('Content-Type', 'text/html')])
        return [b'notified']

    if environ['PATH_INFO'] == '/park/notify_wrong':
        wsgo.notify_parked("555", wsgo.HTTP_504, "")
        start_response('200 OK', [
            ('Content-Type','text/html'),
        ])
        return [b"notified!"]

class RefcountResponse:
    """Reports the refcount of the previously yielded chunk while the
    server is iterating the response. wsgo must release each chunk as soon
    as it has been written, rather than retaining the whole response."""

    def __init__(self):
        self.items = []
        self.counts = []

    def __iter__(self):
        for i in range(3):
            self.items.append(b'chunk-%d' % i)
            if len(self.items) > 1:
                self.counts.append(sys.getrefcount(self.items[-2]))
            yield self.items[-1]
        yield ('counts:' + ','.join(str(c) for c in self.counts)).encode()


class BadIterResponse:
    """Yields one chunk, then raises. It defines close() so that wsgo's
    close-attribute check can't accidentally clear the leaked error."""

    def __iter__(self):
        yield b'partial'
        raise ValueError('iterator-boom')

    def close(self):
        pass


class RaiseCloseResponse:
    """A response whose .close() raises."""

    def __iter__(self):
        return iter([])

    def close(self):
        raise ValueError('close-boom')


class AccelCloseResponse:
    """Response object whose close() is counted, used to check that
    X-Sendfile/parked responses are still closed."""

    def __iter__(self):
        return iter([b'ignored'])

    def close(self):
        global accel_close_count
        accel_close_count += 1


def badheaders_testing(environ, start_response):
    """Routes that exercise malformed start_response() arguments. wsgo must
    reject these without crashing the worker process."""
    path = environ['PATH_INFO']
    if path == '/badheaders/nonstring':
        start_response('200 OK', [('Content-Type', 123)])
    elif path == '/badheaders/longtuple':
        start_response('200 OK', [('Content-Type', 'text/plain', 'extra')])
    elif path == '/badheaders/many':
        # Large enough to overflow a C stack VLA in the unfixed version.
        start_response('200 OK', [('X-%d' % i, 'v') for i in range(500000)])
    return [b'ignored']


def block_testing(environ, start_response):
    start_response('200 OK', [
        ('Content-Type','text/html'),
        ('X-WSGo-Block', '2'),
    ])

    return [b"ignored"]

def do_atexit():
    print('atexit was called')
atexit.register(do_atexit)
