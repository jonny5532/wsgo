"""Cacheable responses for the page-cache tests."""

import time

from . import toolkit

router = toolkit.Router()


@router.route('/cache/time')
def cache_time(environ, start_response):
    return toolkit.respond(start_response, ('%.3f' % time.time()).encode('utf-8'), headers=[
        ('Content-Type', 'text/html'),
        ('Cache-Control', 'max-age=60'),
        ('Vary', 'Cookie'),
    ])


@router.route('/cache/method')
def cache_method(environ, start_response):
    return toolkit.respond(start_response, environ['REQUEST_METHOD'].encode('ascii'), headers=[
        ('Content-Type', 'text/html'),
        ('Cache-Control', 'max-age=60'),
    ])


@router.route('/cache/biguid')
def cache_biguid(environ, start_response):
    body = (environ['QUERY_STRING'] + ' ' + str(time.time())).encode('ascii')
    return toolkit.respond(start_response, body + b'A' * 10000, headers=[
        ('Content-Type', 'text/plain'),
        ('Cache-Control', 'max-age=3600'),
    ])


@router.route('/cache/landing')
def cache_landing(environ, start_response):
    return toolkit.respond(start_response, ('%.6f' % time.time()).encode('utf-8'), headers=[
        ('Content-Type', 'text/html'),
        ('Cache-Control', 'max-age=60'),
        ('Vary', 'Cookie'),
        ('X-WSGo-Vary-Cookies', 'sessionid'),
        ('X-WSGo-Ignore-Params', 'utm_*, fbclid'),
    ])


@router.route('/cache/slow')
def cache_slow(environ, start_response):
    time.sleep(0.5)
    return toolkit.respond(start_response, ('%.6f' % time.time()).encode('utf-8'), headers=[
        ('Content-Type', 'text/html'),
        ('Cache-Control', 'max-age=60'),
    ])


def application(environ, start_response):
    return router.dispatch(environ, start_response)
