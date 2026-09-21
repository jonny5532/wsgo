"""Generic fallback application: body hashing, cookies, environ echo,
streaming and thread-local checks. Most tests that don't care about a
specific feature start this module."""

import atexit
import hashlib
import threading

from . import toolkit

thread_local = threading.local()


def _do_atexit():
    print('atexit was called')


atexit.register(_do_atexit)


def default_app(environ, start_response):
    digest = hashlib.md5()
    for chunk in toolkit.read_body(environ):
        digest.update(chunk)
    return toolkit.respond(start_response, digest.hexdigest().encode('ascii'), headers=[
        ('Content-Type', 'text/html; charset=utf-8'),
        ('Set-Cookie', 'cookie1=cookievalue1'),
        ('Set-Cookie', 'cookie2=cookievalue2'),
        ('Set-Cookie', 'cookie3=cookievalue3'),
        ('Vary', 'Cookie'),
    ])


router = toolkit.Router(default=default_app)


@router.route('/ok')
def ok(environ, start_response):
    return toolkit.respond(start_response, b'OK')


@router.route('/thread-local/')
def thread_local_count(environ, start_response):
    count = getattr(thread_local, 'count', 0) + 1
    thread_local.count = count
    return toolkit.respond(start_response, str(count).encode('ascii'))


@router.route('/stream/')
def stream(environ, start_response):
    def chunks():
        thread_local.name = threading.current_thread().name
        for _ in range(100):
            yield b'hello' * 100000
            assert thread_local.name == threading.current_thread().name

    start_response('200 OK', [('Content-Type', 'text/plain')])
    return chunks()


def echo_path(environ, start_response):
    # WSGI environ variables are always delivered as ISO-8859-1 strings even
    # if they are some other encoding internally (like utf-8).
    path_info = environ['PATH_INFO'].encode('iso-8859-1').decode('utf-8')
    return toolkit.respond(start_response, path_info.encode('utf-8'), headers=[
        ('Content-Type', 'text/html; charset=utf-8'),
    ])


def application(environ, start_response):
    if environ['PATH_INFO'].startswith('/echo/'):
        return echo_path(environ, start_response)
    return router.dispatch(environ, start_response)
