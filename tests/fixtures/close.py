"""Responses that track whether wsgo called .close() on them, for normal,
X-Sendfile and parked responses."""

from . import toolkit

router = toolkit.Router()

_counter = 0


class CountingResponse:
    def __iter__(self):
        return iter([str(_counter).encode('ascii')])

    def close(self):
        global _counter
        _counter += 1


@router.route('/close/tracked')
def tracked(environ, start_response):
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return CountingResponse()


@router.route('/close/state')
def state(environ, start_response):
    return toolkit.respond(start_response, str(_counter).encode('ascii'))


@router.route('/close/accel-sendfile')
def accel_sendfile(environ, start_response):
    start_response('200 OK', [
        ('Content-Type', 'text/plain'),
        ('X-Sendfile', 'fixtures/echo.py'),
    ])
    return CountingResponse()


@router.route('/close/accel-park')
def accel_park(environ, start_response):
    start_response('200 OK', [
        ('Content-Type', 'text/plain'),
        ('X-WSGo-Park', 'close-channel'),
        ('X-WSGo-Park-Timeout', '1 http-204'),
    ])
    return CountingResponse()


def application(environ, start_response):
    return router.dispatch(environ, start_response)
