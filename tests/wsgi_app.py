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

    if environ['PATH_INFO']=='/time/':
        start_response('200 OK', [
            ('Content-Type','text/html'),
            ('Cache-Control','max-age=60'),
            ('Vary','Cookie'),
        ])
        return [("%.3f"%(time.time())).encode('utf-8')]

    start_response('200 OK', [
        ('Content-Type','text/html'),
        ('Set-Cookie','cookie1=cookievalue1'),
        ('Set-Cookie','cookie2=cookievalue2'),
        ('Set-Cookie','cookie3=cookievalue3'),
        ('Vary','Cookie'),
    ])

    if environ['PATH_INFO']=='/output/':
        def ret():
            thread_local.name = threading.current_thread().name
            for i in range(100):
                yield b'hello'*100000
                assert thread_local.name == threading.current_thread().name
        return ret()

    if environ['PATH_INFO'].startswith('/echo/'):
        return [environ['PATH_INFO'].encode('utf-8')]

    return [h.hexdigest().encode('utf-8')]
