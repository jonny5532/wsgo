"""Per-feature WSGI applications used by the integration tests.

Each module here is deliberately small and owns one concern, so a test can
start only the application it needs::

    self.start(module='fixtures.park')

Fixture modules may import wsgo (they run inside the server process). The
test runner itself must not import them, since the wsgo C extension only
exists inside the server process.
"""
