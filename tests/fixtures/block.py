"""Blocking endpoint plus a ping route that the tests make from a blocked
and an unblocked address."""

from . import toolkit

router = toolkit.Router()


@router.route('/block/start')
def start(environ, start_response):
    return toolkit.respond(start_response, b'ignored', headers=[
        ('Content-Type', 'text/html'),
        ('X-WSGo-Block', '2'),
    ])


@router.route('/block/ping')
def ping(environ, start_response):
    return toolkit.respond(start_response, b'BLOCK-PING')


def application(environ, start_response):
    return router.dispatch(environ, start_response)
