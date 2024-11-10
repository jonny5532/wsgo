import atexit
import hashlib
import logging
import time
import threading
import wsgo

thread_local = threading.local()
close_count = 0

logger = logging.getLogger('test_logger')

def application(environ, start_response):
    if environ['PATH_INFO'].startswith('/park/'):
        return park_testing(environ, start_response)
    if environ['PATH_INFO'].startswith('/block/'):
        return block_testing(environ, start_response)

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
        ('Content-Type','text/html'),
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
        return [environ['PATH_INFO'].encode('utf-8')]

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

    if environ['PATH_INFO'] == '/park/notify_wrong':
        wsgo.notify_parked("555", wsgo.HTTP_504, "")
        start_response('200 OK', [
            ('Content-Type','text/html'),
        ])
        return [b"notified!"]

def block_testing(environ, start_response):
    start_response('200 OK', [
        ('Content-Type','text/html'),
        ('X-WSGo-Block', '2'),
    ])

    return [b"ignored"]

def do_atexit():
    print('atexit was called')
atexit.register(do_atexit)
