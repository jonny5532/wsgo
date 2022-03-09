import hashlib
import time

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

    return [h.hexdigest().encode('utf-8')]
