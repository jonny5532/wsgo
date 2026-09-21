# Test fixtures

Each module here is a small WSGI application. Tests pick the fixture they
need with `self.start(module='fixtures.<name>')`; the default is
`fixtures.echo`. Keeping one concern per module means a fixture can't leak
state or route handling into unrelated tests.

Fixture modules run inside the `wsgo` server process and may import `wsgo`.
The test runner must not import them, because the `wsgo` C extension only
exists inside the server.

| module      | routes                                                       | used by |
|-------------|--------------------------------------------------------------|---------|
| `echo`      | `/`, `/ok`, `/echo/...`, `/stream/`, `/thread-local/`          | basic get/post/headers/threading/encoding/atexit |
| `cache`     | `/cache/time`, `/cache/method`, `/cache/biguid`                | caching and page-cache tests |
| `slow`      | `/slow/wait`, `/slow/wait-long`, `/slow/busy`, `/slow/fast`, `/slow/log`, `/slow/fill-stderr` | timeout, logging-deadlock and stale-timeout tests |
| `body`      | `/body/read-all`, `/body/read-n`, `/body/late-read`, `/body/ok` | `wsgi.input` tests |
| `errors`    | `/errors/none`, `/errors/not-iterable`, `/errors/bad-header-*`, `/errors/many-headers`, `/errors/iterator-raises`, `/errors/close-raises`, `/errors/chunk-refcount`, `/errors/pending-exception`, `/errors/ok` | malformed response and exception-leak tests |
| `close`     | `/close/tracked`, `/close/state`, `/close/accel-sendfile`, `/close/accel-park` | response `.close()` tests |
| `park`      | `/park/start`, `/park/notify-*`, `/park/retry-again`, `/park/notify-retry-again` | park/notify tests |
| `block`     | `/block/start`, `/block/ping`                                 | blocking tests |
| `refcount`  | `/`                                                           | borrowed module dict reference test |

Helpers shared by fixtures live in `toolkit.py` (`respond`, `read_body`,
`Router`). Route paths are plain strings in both the fixture and the test,
so each side is readable on its own.
