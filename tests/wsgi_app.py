import hashlib

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

    start_response('200 OK', [
        ('Content-Type','text/html'),
    ])

    return [h.hexdigest().encode('utf-8')]
