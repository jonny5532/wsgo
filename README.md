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


## Caveats

- Uses Cgo, with lots of unsafeness
- Poorly tested (only with Django on Ubuntu/Debian x86 platforms so far)
- Opinionated, with some hardcoded behaviour


## Known issues

- The `wsgi.input` file-like object only implements the `read(...)` method (this is enough for Django).
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

```
 --heavy <number of worker threads>       (default 2)
```

Requests are classified into two tiers - normal and heavy. Normal requests can be handled by any of the worker threads, whereas heavy requests are limited to a subset of worker threads (2 by default, configurable with `--heavy`), so will queue up if those workers are busy. This avoids slow endpoints bogging down the server and denying access to the normal ones.

Requests are classified according to several criteria:

- The response time of previous requests is stored. Any request to a URL where the 75th percentile of the past response times is over the _slow response threshold_ (1000ms by default) will be considered heavy.

- Any request with a URL prefix matching one of the __heavy-prefix__ arguments will be considered heavy.

- Any request with a query string will be considered heavy.


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


## Asynchronous retry

An asynchronous 'Retry' mechanism allows workers to return immediately, but leaves the request hanging without a response having been sent. A later call to `wsgo.notify_retry` from a different thread within the same process can then wake the hanging request and cause it to be processed again.

This allows long-running requests (such as long-polls) to be queued up without using up a worker each. 



# Background

This project is heavily inspired by uWSGI, which the author has successfully used in production for many years. However it has some drawbacks:

- It is difficult to configure correctly - the defaults are unsuitable and achieving reliablity and high performance requires a lot of trial-and-error.

- It is has a large amount of functionality implemented in C, so has the potential for buffer overflow vulnerabilities (it has had one CVE due to this so far).

- Using a reverse proxy/load balancer infront of it is recommended, as it has limited defense against request floods or slow loris attacks. It would be nice to not require this.

- It lacks features like a page cache, which is understandable for larger deployments (where separate Varnish instances might be used) but would be nice to have built in for simpler deployments of smaller sites.


# Roadmap

This project is currently being used in production, but still needs some tuning and has some missing features.

- Is the threads-per-process count appropriate? It is deliberately quite high, but this may cause issues with the number of simultaneous database connections required. It does provide an easy way to queue low priority requests, by limiting them to a smaller pool of threads, but this may be better done by reordering the queue.

- It is currently too easy to (accidentially or deliberately) bog down the server with a flood of requests. It already attempts to confine consistently-slow endpoint requests to a smaller pool of threads, but it might also be useful to restrict the number of simultaneous requests by source IP, or apply rate limiting. The defaults need to be set such that this doesn't get in the way of normal use, however.

- The code still needs tidying up, and some tests writing.
