"""Test fixture: compares the refcount of this module's dictionary with an
equivalent dynamically-created module. PyModule_GetDict returns a borrowed
reference, so wsgo must not decref it during startup; doing so leaves the
dictionary's refcount one too low."""

import sys
import types

_refmod = types.ModuleType('_wsgo_refmod')
exec("""
import sys
def measure():
    return sys.getrefcount(globals())
""", _refmod.__dict__)


def application(environ, start_response):
    ours = sys.getrefcount(globals())
    theirs = _refmod.measure()
    start_response('200 OK', [('Content-Type', 'text/plain')])
    return [('%d,%d' % (ours, theirs)).encode('ascii')]
