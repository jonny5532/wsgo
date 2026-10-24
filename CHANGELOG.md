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
