"""Request-body reading endpoints."""

import threading
import time

from . import toolkit

router = toolkit.Router()


@router.route('/body/read-all')
def read_all(environ, start_response):
    # read() with no size must return the whole body.
    data = environ['wsgi.input'].read()
    return toolkit.respond(start_response, str(len(data)).encode('ascii'))


@router.route('/body/read-n')
def read_n(environ, start_response):
    # read(n) must return exactly n bytes until EOF, like a buffered file.
    lengths = []
    while True:
        data = environ['wsgi.input'].read(1048576)
        lengths.append(len(data))
        if len(data) < 1048576:
            break
    return toolkit.respond(start_response, ','.join(str(n) for n in lengths).encode('ascii'))


@router.route('/body/late-read')
def late_read(environ, start_response):
    # Simulate an application thread that keeps environ (and thus
    # wsgi.input) alive after the request has finished.
    def read_later(env):
        time.sleep(1.0)
        try:
            env['wsgi.input'].read(10)
        except Exception:
            pass

    threading.Thread(target=read_later, args=(environ,), daemon=True).start()
    return toolkit.respond(start_response, b'started')


@router.route('/body/ok')
def ok(environ, start_response):
    return toolkit.respond(start_response, b'BODY-OK')


def application(environ, start_response):
    return router.dispatch(environ, start_response)
