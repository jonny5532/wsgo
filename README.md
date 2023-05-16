# wsgo

A simple and fast Python WSGI application server written in Go, with sensible defaults and minimal configuration.

## Installation

If you have Docker installed, you can run `./build.sh` to produce Go executables and Python wheels for all currently supported versions of Python, in the `dist/` folder.

Then either run `pip install <dist/file.whl>` to install `wsgo` in your bin folder, or manually retrieve the appropriate `dist/wsgo-*` executable and include it with your app.


## Usage

```
./wsgo 
  --workers 8
  --processes 4
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
- Request prioritisation (by URL prefix, request properties, and previous response times)
- Cron-like system for running background tasks
- Request parking mechanism to support long-polling


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

The most appropriate number of threads will depend on your application. An app that makes lots of disk or network accesses will likely be IO bound and benefit from multiple threads, as useful work can be done whilst some threads are blocked waiting for IO to complete. However an app that is primarily CPU bound would be better with fewer threads, as more threads will incur unnecessary context switches and cache misses for little benefit.

Threads carry some overhead - at least 8mb RAM per thread for the stack, as well as any thread-local resources such as open database connections. Beware of hitting MySQL's 150 or PostgreSQL's 100 default maximum connections.


## Timeouts

```
 --request-timeout <timeout in seconds>       (default 60)
```

There is a single configurable request timeout. If a worker is processing a request for longer than this, then a Python exception will be raised in the thread to interrupt it.

Occasionally a thread may get 'stuck', if it is blocking in such a way that the exception fails to interrupt it (for example, blocking IO requests being made within extensions without a timeout). If all of the workers get into this 'stuck' state, the process will exit and be restarted.

The actual http server has longer hardcoded timeouts (2 seconds to read the request header, 600 seconds to read the PUT/PATCH/POST body, 3600 seconds to write the response body, and 60 seconds max idle for a keep-alive). This is due to a Go limitation, where these can't be altered per-request, and so need to be large enough to accommodate the slowest uploads and downloads. However Go's coroutine mechanism means that a large number of lingering requests is not an issue, as long as the Python threads themselves are not overloaded.


## Response caching

```
 --max-age <maximum cache time in seconds>     (default 0, disabled)
```

You can enable caching by passing the `--max-age <seconds>` argument with a positive number of seconds to cache responses for. All responses to `GET`, `HEAD` and `OPTIONS` requests will be cached, unless they have a `Cache-control` header to disable caching, are more than 1 megabyte in size, or set cookies.

You will almost certainly need to modify your application for this to behave as you expect, since dynamic content will be cached by default. You should set a `Cache-control: no-cache` response header on any page where a user might expect to see updated content change immediately.

You can reduce the cache time from the maximum with a `Cache-Control: max-age=<seconds>` response header, with the desired cache time in seconds.

The cache respects the `Vary` response header, which is used to indicate that a cache entry should only be used if the supplied list of request header fields are the same on cached and future requests.

For example, setting `Vary: Cookie` on a response header for a page in a customer account area will ensure that another customer, with a varying session cookie, will not see a cached response belonging to the first customer.

The `Vary: Cookie` check will match the the entire `Cookie` request header, which may contain more than one cookie. You can supply an additional non-standard response header, `X-WSGo-Vary-Cookies`, to specify which individual cookies the page should vary on. This allows cookies such as tracking cookies, which don't affect the page content, to be ignored. For example:

`X-WSGo-Vary-Cookies: sessionid, remember_me`

A useful strategy is to add middleware to your application to add appropriate cache headers to every response if they are not already set. This might include:

- always setting `Cache-control: no-cache` on any request by a logged-in user


## Request prioritisation

Incoming requests are assigned a priority, and higher priority tasks will be served before lower ones, even if the lower one came in first.

Requests with a priority below a hardcoded threshold (currently -7000) will only run if no other workers are busy. Requests may time out without being handled, if the queue never emptied and their priority was insufficient for them to run within their request timeout.

The priority of a request is calculated as follows:

- All requests start with a priority of 1000
- Anything with a query string: -500
- Each concurrent request from the same IP: -1000
- Each historic request from the same IP: -200 (decays by +200/second)
- User-Agent containing bot/crawler/spider/index: -8000
- Each millisecond of average CPU time for same request URI: -10

The priorities are recalculated everytime a request is grabbed from the queue.


## Cron-like system

The `wsgo` module provides two decorators:

`@wsgo.cron(min, hour, day, mon, wday)`

`@wsgo.timer(period)`

These should be used on top-level functions declared in the WSGI application Python file (or directly imported from it).

For example:

```
@wsgo.cron(30, -1, -1, -1, -1)
def runs_at_half_past_every_hour():
    print("Hi there!")
```

If you are using multi-process mode, these will only be activated in the first process.


## Request parking

A request parking mechanism allows for long-polling, where a request may be held deliberately for a long time before being responded to, without tying up a worker for each request.

A worker may respond immediately with the response headers:

```
X-WSGo-Park: channel1, channel2, channel3
X-WSGo-Park-Timeout: 60 http-204
```

This will cause the worker to finish, but the client will not be responded to. When the park timeout expires, the request will be handled according to the timeout action:

*retry*: The request will be retried.
*disconnect*: The connection will be dropped without a response.
*http-204*: The client will receive a HTTP 204 No Content response.
*http-504*: The client will receive a HTTP 504 Gateway Timeout response.

Parked requests can also be awakened via a function called from another thread:

``wsgo.notify_parked(channels, action, arg)`

where `channels` is a string containing a comma separated list of channels, `action` is one of `wsgo.RETRY`/`wsgo.DISCONNECT`/`wsgo.HTTP_204`/`wsgo.HTTP_504`, and `arg` is a string argument that, in the case of a retry, will be passed along with the request as the header `X-WSGo-Park-Arg`.

Any channel in the list of supplied channels that matches a channel of a waiting parked request will cause it to be awakened.

A retried request will be called with the same HTTP method and request body, with the additional `X-WSGo-Park-Arg` header (which in the case of a timeout retry may be a blank string). A retried request cannot be parked (to avoid the potential for eternally parked requests).

Each process handles its parked requests separately, so if you need to be able to awaken requests parked on one process from another process, you will need a separate asynchronous signalling mechanism between processes (such as PostgreSQL's LISTEN/NOTIFY) with a dedicated listening thread in each process.


# Background

This project is heavily inspired by uWSGI, which the author has successfully used in production for many years. However it has some drawbacks:

- It is difficult to configure correctly - the defaults are unsuitable and achieving reliablity and high performance requires a lot of trial-and-error.

- It is has a large amount of functionality implemented in C, so has the potential for buffer overflow vulnerabilities (it has had one CVE due to this so far).

- Using a reverse proxy/load balancer infront of it is recommended, as it has limited defense against request floods or slow loris attacks. It would be nice to not require this.

- It lacks features like a page cache, which is understandable for larger deployments (where separate Varnish instances might be used) but would be nice to have built in for simpler deployments of smaller sites.


# Roadmap

This project is currently being used in production, but still needs some tuning and has some missing features.

- Is the threads-per-process count appropriate? It is deliberately quite high, but this may cause issues with the number of simultaneous database connections required. Also the GIL will prevent true multiprocessing, but then threading uses less memory than an equivalent number of processes.

- The code still needs tidying up, and more tests writing.
