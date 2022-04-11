import hashlib
import time
import threading

thread_local = threading.local()

def application(environ, start_response):
    h = hashlib.md5()
    if environ['REQUEST_METHOD']=='POST':
        n = 0
        while True:
            to_read = 4096
            data = environ['wsgi.input'].read(to_read)
            h.update(data)
            n += len(data)
            if len(data)<to_read:
                break
        assert n>0

    if environ['PATH_INFO']=='/wait/':
        time.sleep(1)

    print("calling start_response")
  
    start_response('200 OK', [
        ('Content-Type','text/html'),
        ('Set-Cookie','cookie1=cookievalue1'),
        ('Set-Cookie','cookie2=cookievalue2'),
        ('Set-Cookie','cookie3=cookievalue3'),
    ])

    if environ['PATH_INFO']=='/output/':
        def ret():
            thread_local.name = threading.current_thread().name
            for i in range(100):
                yield b'hello'*1000000
                #print('checking name', thread_local.name, threading.current_thread().name)
                assert thread_local.name == threading.current_thread().name
        return ret()


    return [h.hexdigest().encode('utf-8')]
