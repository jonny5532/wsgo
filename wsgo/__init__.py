"""wsgo: a simple and fast WSGI server in Go."""

from ._wsgo import add_cron, notify_parked

__all__ = [
    'add_cron',
    'notify_parked',
    'cron',
    'timer',
    'DISCONNECT',
    'RETRY',
    'HTTP_204',
    'HTTP_504',
    'RequestTimeoutException',
]

# Actions for notify_parked()
DISCONNECT = 0
RETRY = 1
HTTP_204 = 2
HTTP_504 = 3


def cron(*args):
    """Runs the decorated function on a cron-style schedule (min, hour, day,
    mon, wday), where -1 means any."""
    def cron(func):
        add_cron(func, 0, *args)
        return func
    return cron


def timer(period_seconds):
    """Runs the decorated function every period_seconds seconds."""
    def timer(func):
        add_cron(func, period_seconds, 0, 0, 0, 0, 0)
        return func
    return timer


class RequestTimeoutException(Exception):
    """Raised in a worker thread when its request has timed out."""
    pass
