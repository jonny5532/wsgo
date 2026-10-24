# Changelog

## 0.1.0

- wsgo is now a CPython extension module rather than an executable embedding
  Python. A single `cp310-abi3` wheel per architecture works with any CPython
  3.10 or later, under any interpreter setup, including uv, venv, pyenv, conda
  and statically linked interpreters.
- Python 3.6-3.9 are no longer supported. pip will install 0.0.25 on those
  versions.
- wsgo can be started with `python -m wsgo` as well as the `wsgo` command.
  The command-line flags are unchanged.
- `import wsgo` now works outside the server (eg. from a management command),
  so modules that use `@wsgo.cron`/`@wsgo.timer` can be imported anywhere.
- Server processes now show up as `python` processes. The main process passes
  SIGUSR1/SIGUSR2 on to them, so `killall wsgo -s USR1` still works when wsgo
  was started with the `wsgo` command.
- wsgo can be built from source with `pip install .`.
- Page cache fixes and improvements:
  - A cached page is refreshed in the background during the last quarter of
    its own lifetime, by a single request. Previously the window was half of
    `--max-age`, so a page with a shorter lifetime than `--max-age` ran the
    application on every hit.
  - Concurrent requests that miss the cache wait for one of them to fetch the
    page, instead of all running the application.
  - `Cache-Control`, `Vary` and `X-WSGo-Vary-Cookies` are parsed properly, so
    eg. `public,max-age=60` (with no space) is now cached, and directives are
    case-insensitive.
  - `s-maxage` sets a lifetime for the page cache that differs from the one
    given to browsers, and `stale-while-revalidate` allows an expired page to
    be served whilst it is refreshed.
  - Responses with `Vary: *` are no longer cached.
  - Cached responses include an `Age` header.
  - The new `X-WSGo-Ignore-Params` response header lists query parameters
    (such as `utm_*` tracking parameters) to ignore when looking up the cache.
  - `X-WSGo-Vary-Cookies` is no longer sent to the client.
