# wsgo

A simple and fast Python WSGI application server written in Go, with sensible defaults and minimal configuration.

## Installation

Binary packages are available via PyPI for Linux x86_64, install with:

`pip install wsgo`

## Quickstart

1. Create a `wsgi_app.py` file containing:

```python
def application(env, start_response):
    start_response('200 OK', [
        ('Content-Type','text/plain'),
    ])
    return [b'Hello world!']
```

2. Start wsgo with:

`wsgo --module wsgi_app --http-socket 127.0.0.1:8000`

3. Go to `http://127.0.0.1:8000` in your web browser.


## Usage

```bash
wsgo 
  --workers 8
  --processes 2
  --module wsgi_app 
  --static-map /=webroot 
  --http-socket 0.0.0.0:8000
  --request-timeout 300
```

## Features

- Simple configuration with sensible defaults
- Single executable (although requires the Python3 it is linked against to be installed)
- Multithreaded, with a multi-process mode
- Built-in header-controlled response cache
- Static file serving
- Request prioritisation (by URL prefix, request properties, and connection counts)
- Cron-like system for running background tasks
- Request parking mechanism to support long-polling
- Blocking mechanism for DoS mitigation


## Building from source

If you have Docker installed, you can run `./build.sh` to produce Go executables and Python wheels for all currently supported versions of Python, in the `dist/` folder.

Then either run `pip install <dist/file.whl>` to install `wsgo` in your bin folder, or manually retrieve the appropriate `dist/wsgo-*` executable and include it with your app.


## Caveats

- Uses Cgo, with lots of unsafeness
- Poorly tested (only with Django on Ubuntu/Debian x86 platforms so far)
- Opinionated, with some hardcoded behaviour


## Known issues

- The `wsgi.input` file-like object only implements the `read(n)` and `readline()` methods (this is enough for Django).
- It can't yet be built via the regular `python setup.py compile` mechanism.


# Documentation

## Multithreading and multi-process

```
 --workers <number of threads per process>     (default 16)
 --processes <number of processes>             (default 1)
```

By default several worker threads will be started, so that multiple responses can be handled in parallel. This requires that your app is thread-safe - if it isn't then you can limit the thread count with `--workers 1`.

Due to the Python Global Interpreter Lock (GIL), only one thread can run at a time, so a single process cannot use more than one core. If you start wsgo with the `--processes <n>` option, then `n` processes will be started, each with a full set of threads. The listening socket has `SO_REUSEPORT` set so that multiple processes can bind to the same address and will share the incoming request stream.

The most appropriate number of threads or processes will depend on your application[^1].


## Timeouts

```
 --request-timeout <timeout in seconds>       (default 60)
```

There is a single configurable request timeout. If a worker is processing a request for longer than this, then a `wsgo.RequestTimeoutException` will be raised asynchronously in the thread to interrupt it.

Interrupting a running worker can cause problems in some code,[^2] resulting in the worker getting 'stuck'. If all of the workers get into a 'stuck' state simultaneously, the process will exit and be restarted. Note that if four or more requests from the same IP are 'stuck', the server may still be responsive to others, but that IP won't be able to make any further requests due to the priority-based IP limiting.

The actual http server has longer hardcoded timeouts (2 seconds to read the request header, 600 seconds to read the PUT/PATCH/POST body, 3600 seconds to write the response body, and 60 seconds max idle for a keep-alive). This is due to a Go limitation, where these can't be altered per-request, and so need to be large enough to accommodate the slowest uploads and downloads. However Go's coroutine mechanism means that a large number of lingering requests is not an issue, as long as the Python threads themselves are not overloaded.


## Static file serving

```
 --static-map <path prefix>=<local path>

eg:
 --static-map /=webroot
      # eg: requests to /favicon.ico will map to ./webroot/favicon.ico
 --static-map /media=storage_dir/media_files
      # eg: requests to /media/images/file1.jpg will map to ./storage_dir/media_files/images/file1.jpg
```

Static file serving is enabled by specifying one or more static mappings. If a request matches a static mapping path prefix, then the specified local path will be checked for a matching file (after stripping the mapping path prefix from the request), and if found it will be sent as the response.

Static file serving happens before WSGI request handling, so if a matching static file is found, it will be returned as the response and processing will finish without calling the WSGI handler. If no matching file was found (even if a mapping prefix matched), the request will be passed on to the WSGI handler.

Static files (after relative paths are resolved and symlinks are followed) must reside within the local path specified, which prevents escapes such as requests to `/media/../../../../etc/passwd`. You therefore cannot serve files which are symlinked to outside of the local path.

You can place gzipped versions of static files adjacent to the originals, with the suffix `.gz`, for example:

```
./webroot/static/styles.css
./webroot/static/styles.css.gz
```

Any request to `/static/styles.css` with a `Accept-Encoding:` header including `gzip` will be served the adjacent gzipped version instead (with `Content-Encoding: gzip` set), which will generally be smaller and served more quickly.


## Response caching

```
 --max-age <maximum cache time in seconds>     (default 0, disabled)
```

You can enable caching by passing the `--max-age <seconds>` argument with a positive number of seconds to cache responses for. Responses to `GET`, `HEAD` and `OPTIONS` requests which include a `Cache-Control: max-age=<seconds>` header will be cached, for the specified number of seconds or the command-line argument, whichever is lower. 

Responses that set cookies or are more than 1 megabyte in size are not cached.

The cache respects the `Vary` response header, which is used to indicate that a cache entry should only be used if the supplied list of request header fields are the same on cached and future requests.

For example, setting `Vary: Cookie` on a response header for a page in a customer account area will ensure that another customer, with a different session cookie, will not see a cached response belonging to the first customer.

The `Vary: Cookie` check will match the the entire `Cookie` request header, which may contain more than one cookie. You can supply an additional non-standard response header, `X-WSGo-Vary-Cookies`, to specify which individual cookies the page should vary on. This allows cookies such as tracking cookies, which don't affect the page content, to be ignored. For example:

`X-WSGo-Vary-Cookies: sessionid, remember_me`

A useful strategy is to add middleware to your application to add appropriate cache headers to every response if they are not already set. This might include:

- always setting `Cache-control: no-cache` on any request by a logged-in user


## Request prioritisation

Incoming requests are assigned a priority, and higher priority tasks will be served before lower ones, even if the lower one came in first.

Requests with a priority below a hardcoded threshold (currently -7000) will only run if no other workers are busy. Requests may time out without being handled, if the queue never emptied and their priority was insufficient for them to run within their request timeout.

A consequence of this is that there is a limit of five concurrent requests from the same IP (v4 /32 or v6 /64) per process.

The priority of a request is calculated as follows:

- All requests start with a priority of 1000
- Anything with a query string: -500
- Each concurrent request from the same IPv4 or /64: -2000
- Each historic request from the same IPv4 or /64: -1000 (decays by +1000/second)
- User-Agent containing bot/crawler/spider/index: -8000

The priorities are recalculated everytime a request is grabbed from the queue.


## Buffering

The first 1mb of POST/PUT/PATCH request bodies will be buffered before the WSGI handler is started. 

Responses are not buffered.

It is therefore possible for users on slow connections to tie up handlers for a significant time during large uploads or downloads - if this is a concern then consider using a buffering load balancer upstream of wsgo.


## Signals

You can send signals to running wsgo processes to cause them to report status information to stdout.

`killall wsgo -s USR1` will print stack traces of all Python workers, which can be useful to diagnose hangs (such as every worker waiting for an unresponsive database with no timeout).

`killall wsgo -s USR2` will print request and error count and memory statistics.


## Cron-like system

The `wsgo` module provides two decorators:

`@wsgo.cron(min, hour, day, mon, wday)`

`@wsgo.timer(period)`

These should be used on top-level functions declared in the WSGI application Python file (or directly imported from it).

For example:

```python
import wsgo

@wsgo.cron(30, -1, -1, -1, -1)
def runs_at_half_past_every_hour():
    print("Hi there!")

@wsgo.timer(30)
def runs_every_thirty_seconds():
    print("Hello again!")
```

If you are using more than one process, these will only be activated in the first one.


## Blocking

You can block a source IP address from being handled for a limited time period by sending the response header:

```
X-WSGo-Block: <time in seconds>
```

Any subsequent requests from the same source IP, to the same process, within the time period, will be responded to with an empty 429 response. The responses will be delayed for 25s (or the remaining block time, whichever is lower), to attempt to slow down DoS attacks or crawlers, although a maximum of 100 simultaneous requests will be delayed so as to not exhaust open file handles.

Blocking is per-process, so if you are running multiple processes and want a block to apply across all of them, you will need a way to record blocks (eg, via a database) and reissue them if the visitor comes back via a different process, until the visitor is blocked in every process.

Blocked requests are not logged individually (to avoid DoS attacks overflowing the logs), but the total number of blocked requests will be reported when the block expires.


## Request parking

A request parking mechanism allows for long-polling, where a request may be held deliberately for a long time before being responded to, without tying up a worker for each request.

A worker may respond immediately with the response headers:

```
X-WSGo-Park: channel1, channel2, channel3
X-WSGo-Park-Timeout: 60 http-204
```

This will cause the worker to finish, but the client will not be responded to. When the park timeout expires, the request will be handled according to the timeout action:

`retry`: The request will be retried.\
`disconnect`: The connection will be dropped without a response.\
`http-204`: The client will receive a HTTP 204 No Content response.\
`http-504`: The client will receive a HTTP 504 Gateway Timeout response.

Parked requests can also be awakened via a function called from another thread:

`wsgo.notify_parked(channels, action, arg)`

where `channels` is a string containing a comma separated list of channels, `action` is one of `wsgo.RETRY`/`wsgo.DISCONNECT`/`wsgo.HTTP_204`/`wsgo.HTTP_504`, and `arg` is a string argument that, in the case of a retry, will be passed along with the request as the header `X-WSGo-Park-Arg`.

Any channel in the list of supplied channels that matches a channel of a waiting parked request will cause it to be awakened.

A retried request will be called with the same HTTP method and request body, with the additional `X-WSGo-Park-Arg` header (which in the case of a timeout retry may be a blank string). A retried request cannot be parked (to avoid the potential for eternally parked requests).

Each process handles its parked requests separately, so if you need to be able to awaken requests parked on one process from another process, you will need a separate asynchronous signalling mechanism between processes (such as PostgreSQL's LISTEN/NOTIFY) with a dedicated listening thread in each process.


# Background

This project is heavily inspired by uWSGI, which the author has successfully used in production for many years. However it has some drawbacks:

- It is difficult to configure correctly - the defaults are unsuitable and achieving reliablity and high performance requires a lot of trial-and-error.

- It has a lot of protocol handling implemented in C, so has the potential for buffer overflow vulnerabilities (it has had one CVE due to this so far).

- It lacks features like a page cache, which is understandable for larger deployments (where separate Varnish instances might be used) but would be nice to have built in for simpler deployments of smaller sites.


# Notes

[^1]:  An app that makes lots of database queries or API calls will likely benefit from multiple threads, as useful work can be done whilst some threads are blocked waiting for responses. However an app that is primarily CPU bound would be better with fewer threads, as more threads will incur unnecessary context switches and cache misses for little benefit.

    A significant advantage of using multiple threads within a single process, as opposed to multiple processes, is that you can easily share data between workers (as they are all running in the same interpreter), which allows the use of fast local in-memory caches such as Django's LocMemCache.

    Threads do carry some overhead - at least 8mb RAM per thread for the stack, as well as any thread-local resources such as open database connections. Also beware of hitting MySQL's 150 or PostgreSQL's 100 default maximum connections.

    Multiple processes are completely isolated from each other, each with their own Python interpreter, request queue, block list and page cache. This is deliberate, to reduce the complexity of the system, and the chance of a deadlock spreading to all processes, but does reduce the effectiveness of using multiple processes rather than threads.


[^2]: Raising exceptions asynchronously in code that does not expect it can cause deadlocks, resulting in threads getting permanently 'stuck'. This includes Python's logging framework pre-3.13, which serializes all logging calls with a per-logger lock, and will leave the lock unreleased if an asynchronous exception occurs during the acquire. Whilst this shouldn't happen often, since the lock should only be held briefly during logging IO, it can occur reliably if the logging triggers slow database queries (such as Django error reports when outputting QuerySets).

    wsgo mitigates this for the logging module, by attempting to release these locks after the request has finished, if an asynchronous exception was raised. Other libraries with this issue may require middleware to catch the `wsgo.RequestTimeoutException` and release the locks, or to always release the locks after every request just in case.


# Roadmap

This project is currently being used in production, but still needs some tuning and has some missing features.

- Is the threads-per-process count appropriate? It is deliberately quite high, but this may cause issues with the number of simultaneous database connections required. Also the GIL will prevent true multiprocessing, but then threading uses less memory than an equivalent number of processes.

- Using Python 3.12's subinterpreters to allow concurrent Python execution inside the same process.

- Deciding whether to add more features such as Websockets or ASGI.

- The code still needs tidying up, and more tests writing.
