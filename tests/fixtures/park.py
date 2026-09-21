"""Park/notify endpoints. The parking request and the notify trigger live in
the same application so a test only needs one running server."""

import wsgo

from . import toolkit

router = toolkit.Router()

PARK_CHANNELS = '12345, 12346'
RETRY_AGAIN_CHANNEL = 'retry-again-channel'


@router.route('/park/start')
def start(environ, start_response):
    if 'HTTP_X_WSGO_PARK_ARG' in environ:
        return toolkit.respond(start_response, environ['HTTP_X_WSGO_PARK_ARG'].encode('ascii'))

    return toolkit.respond(start_response, b'ignored', headers=[
        ('Content-Type', 'text/html'),
        ('X-WSGo-Park', PARK_CHANNELS),
        ('X-WSGo-Park-Timeout', '6 http-504'),
    ])


@router.route('/park/notify-204')
def notify_204(environ, start_response):
    wsgo.notify_parked('12345', wsgo.HTTP_204, '')
    return toolkit.respond(start_response, b'notified')


@router.route('/park/notify-retry')
def notify_retry(environ, start_response):
    wsgo.notify_parked('12345', wsgo.RETRY, 'retry_arg')
    return toolkit.respond(start_response, b'notified')


@router.route('/park/notify-wrong')
def notify_wrong(environ, start_response):
    wsgo.notify_parked('wrong-channel', wsgo.HTTP_504, '')
    return toolkit.respond(start_response, b'notified')


@router.route('/park/retry-again')
def retry_again(environ, start_response):
    # Ask to be parked again, even on a retry. wsgo must ignore the park
    # header on a retry, otherwise the request would loop forever.
    headers = [
        ('Content-Type', 'text/html'),
        ('X-WSGo-Park', RETRY_AGAIN_CHANNEL),
        ('X-WSGo-Park-Timeout', '5 http-504'),
    ]
    if 'HTTP_X_WSGO_PARK_ARG' in environ:
        return toolkit.respond(start_response, b'retry-complete', headers=headers)
    return toolkit.respond(start_response, b'first-pass', headers=headers)


@router.route('/park/notify-retry-again')
def notify_retry_again(environ, start_response):
    wsgo.notify_parked(RETRY_AGAIN_CHANNEL, wsgo.RETRY, 'some-arg')
    return toolkit.respond(start_response, b'notified')


def application(environ, start_response):
    return router.dispatch(environ, start_response)
