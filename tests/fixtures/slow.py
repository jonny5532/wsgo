"""Slow/blocking endpoints used by the timeout and logging tests."""

import logging
import os
import threading
import time

from . import toolkit

logger = logging.getLogger('test_slow')

router = toolkit.Router()


@router.route('/slow/wait')
def wait(environ, start_response):
    time.sleep(1)
    return toolkit.respond(start_response, b'WAIT-DONE')


@router.route('/slow/wait-long')
def wait_long(environ, start_response):
    for _ in range(100):
        time.sleep(0.1)
    return toolkit.respond(start_response, b'WAIT-LONG-DONE')


@router.route('/slow/busy')
def busy(environ, start_response):
    # Finishes just after a one second request timeout.
    end = time.monotonic() + 1.05
    while time.monotonic() < end:
        pass
    return toolkit.respond(start_response, b'BUSY-DONE')


@router.route('/slow/fast')
def fast(environ, start_response):
    return toolkit.respond(start_response, b'FAST-OK')


@router.route('/slow/log')
def slow_log(environ, start_response):
    class SlowString:
        def __repr__(self):
            time.sleep(2)
            return 'hello'

    logger.error(SlowString())
    return toolkit.respond(start_response, b'LOGGED')


def _fill_stderr():
    # Fill the server's stderr pipe so that a Go log call blocks until the
    # test drains it.
    chunk = b'x' * 65536
    for _ in range(64):
        try:
            os.write(2, chunk)
        except OSError:
            break


@router.route('/slow/fill-stderr')
def fill_stderr(environ, start_response):
    threading.Thread(target=_fill_stderr, daemon=True).start()
    return toolkit.respond(start_response, b'filling')


def application(environ, start_response):
    return router.dispatch(environ, start_response)
