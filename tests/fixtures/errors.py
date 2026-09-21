"""Malformed-response and exception-leak endpoints."""

import ctypes
import sys

from . import toolkit

# Direct access to the Python error indicator, so tests can check that wsgo
# doesn't leak an exception from one request into the next.
_pyerr_occurred = ctypes.pythonapi.PyErr_Occurred
_pyerr_occurred.restype = ctypes.c_void_p

router = toolkit.Router()


@router.route('/errors/none')
def none(environ, start_response):
    # Common application bug: call start_response but return None.
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return None


@router.route('/errors/not-iterable')
def not_iterable(environ, start_response):
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return 42


@router.route('/errors/bad-header-value')
def bad_header_value(environ, start_response):
    start_response('200 OK', [('Content-Type', 123)])
    return [b'ignored']


@router.route('/errors/bad-header-tuple')
def bad_header_tuple(environ, start_response):
    start_response('200 OK', [('Content-Type', 'text/plain', 'extra')])
    return [b'ignored']


@router.route('/errors/many-headers')
def many_headers(environ, start_response):
    # Large enough to overflow a C stack VLA in the unfixed version.
    start_response('200 OK', [('X-%d' % i, 'v') for i in range(500000)])
    return [b'ignored']


class ChunkRefcountResponse:
    """Reports the refcount of the previously yielded chunk while the server
    is iterating the response. wsgo must release each chunk as soon as it has
    been written, rather than retaining the whole response."""

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


@router.route('/errors/chunk-refcount')
def chunk_refcount(environ, start_response):
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return ChunkRefcountResponse()


class IteratorRaises:
    """Yields one chunk, then raises. It defines close() so that wsgo's
    close-attribute check can't accidentally clear the leaked error."""

    def __iter__(self):
        yield b'partial'
        raise ValueError('iterator-boom')

    def close(self):
        pass


@router.route('/errors/iterator-raises')
def iterator_raises(environ, start_response):
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return IteratorRaises()


class CloseRaises:
    """A response whose .close() raises."""

    def __iter__(self):
        return iter([])

    def close(self):
        raise ValueError('close-boom')


@router.route('/errors/close-raises')
def close_raises(environ, start_response):
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return CloseRaises()


@router.route('/errors/pending-exception')
def pending_exception(environ, start_response):
    errored = b'yes' if _pyerr_occurred() else b'no'
    return toolkit.respond(start_response, errored)


@router.route('/errors/ok')
def ok(environ, start_response):
    return toolkit.respond(start_response, b'ERRORS-OK')


def application(environ, start_response):
    return router.dispatch(environ, start_response)
