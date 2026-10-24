"""Endpoints for the priority-scheduling tests: one blocking route to occupy
a worker, plus fast routes whose scheduling order the test observes."""

import itertools
import time

from . import toolkit

router = toolkit.Router()

# Incremented as each work request is handled, so a response carries the order
# in which the server actually served it, independent of the order the client
# happens to see responses complete.
_served = itertools.count(1)


@router.route('/prio/block')
def block(environ, start_response):
    # Signal that this request now occupies a worker, so the test knows the
    # requests it sends next are guaranteed to queue.
    print('PRIO-BLOCK-ENTER', flush=True)
    time.sleep(2)
    return toolkit.respond(start_response, b'BLOCK-DONE')


def _work(start_response):
    return toolkit.respond(start_response, str(next(_served)).encode('ascii'))


@router.route('/prio/normal')
def normal(environ, start_response):
    return _work(start_response)


@router.route('/prio/crawler')
def crawler(environ, start_response):
    return _work(start_response)


def application(environ, start_response):
    return router.dispatch(environ, start_response)
