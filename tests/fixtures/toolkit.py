"""Small helpers shared by the WSGI test fixtures."""


def respond(start_response, body=b'', status='200 OK', headers=()):
    """Call start_response with sensible defaults and return a one-chunk body."""
    headers = list(headers)
    if not any(name.lower() == 'content-type' for name, _ in headers):
        headers.append(('Content-Type', 'text/plain'))
    start_response(status, headers)
    return [bytes(body)]


def read_body(environ, chunk=4096):
    """Yield the request body in chunks (empty stream if there is no body)."""
    while True:
        data = environ['wsgi.input'].read(chunk)
        if not data:
            return
        yield data


class Router:
    """Exact-match router with duplicate-route detection at import time."""

    def __init__(self, default=None):
        self._routes = {}
        self._default = default

    def route(self, path):
        def decorator(func):
            if path in self._routes:
                raise ValueError('duplicate route: %s' % path)
            self._routes[path] = func
            return func
        return decorator

    def dispatch(self, environ, start_response):
        handler = self._routes.get(environ['PATH_INFO'])
        if handler is not None:
            return handler(environ, start_response)
        if self._default is not None:
            return self._default(environ, start_response)
        return respond(start_response, b'not found', status='404 Not Found')
