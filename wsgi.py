import datetime
import functools
import gc
import hashlib
import sys
import time
import threading

def blah(func):
    @functools.wraps(func)
    def inner(*args, **kwargs):
        print("work it")
        return func(*args, **kwargs)
    return inner

import wsgo
@wsgo.cron(-2, -1, -1, -1, -1)
@blah
def every_two_minutes():
    print("hey")

@wsgo.timer(4)
def yep():
    print("sometimes")

def application(env, start_response):
    #time.sleep(0.01)

    # def func():
    #     print("Thread starting!")
    #     time.sleep(2)
    #     print("Thread finishing!")
    # threading.Thread(target=func).start()
    h = hashlib.md5()

    n = 0
    while True:
        to_read = 100000
        data = env['wsgi.input'].read(to_read)
        h.update(data)
        #if n==0:
        #    print(data[:1000])
        n += len(data)

        if len(data)<to_read:
            break

    #print(n, h.hexdigest())
    #env['wsgi.errors'].write('reporting an error!\n')
    #env['wsgi.errors'].flush()
    
    #gc.collect()
    #print(sys._debugmallocstats())

    start_response('200 OK', [
        #('Content-Type','text/html'),
        ('SomeHeader', 'yeah'),
        ('X-sendfile', 'go.mod'),
    ])
    return [("The time is %s!"%(datetime.datetime.now())).encode('utf-8')]

data = {'hi':'there'}
