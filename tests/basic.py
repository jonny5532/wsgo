import requests
import sys
import threading
import time
import unittest

from .utils import WsgoTestCase

class BasicTests(WsgoTestCase):

    def test_get(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.get('http://localhost:8000')
        self.assertEqual(r.status_code, 200)

    def _disable_test_wait(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        try:
            r = requests.get('http://localhost:8000/wait/', timeout=0.001)
        except requests.exceptions.ReadTimeout as e: pass
        time.sleep(2)
        self.assertNotIn("calling start_response", sys.stdout.getvalue())

    def test_post(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.post('http://localhost:8000', data={
            'key1':'value1 \U0001F600',
            'key2\U0001F600':'value2',
        })
        self.assertEqual(r.status_code, 200)

    def test_huge_post(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.post('http://localhost:8000', data={
            'key1':'value1 \U0001F600',
            'key2\U0001F600':'value2',
        }, files={
            'upload':b'0123456789'*1000000
        })
        self.assertEqual(r.status_code, 200)

    def test_repeated_headers(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.get('http://localhost:8000')
        self.assertEqual(r.cookies['cookie1'], 'cookievalue1')
        self.assertEqual(r.cookies['cookie2'], 'cookievalue2')
        self.assertEqual(r.cookies['cookie3'], 'cookievalue3')

    def test_threading(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        def go():
            r = requests.get('http://localhost:8000/output/', timeout=5)
            assert len(r.content)==50000000
        for i in range(16):
            threading.Thread(target=go).start()
        time.sleep(2)
        #print(sys.stdout.getvalue())
    
    def test_caching(self):
        self.start('--module', 'wsgi_app', '--process', '1', '--max-age', '60')
        t = requests.get('http://localhost:8000/time/').text
        time.sleep(0.01)

        # Should have been cached
        self.assertEqual(requests.get('http://localhost:8000/time/').text, t)

        time.sleep(0.01)

        t2 = requests.get(
            'http://localhost:8000/time/',
            headers={'Cookie':'asdf=dsfg'}
        ).text

        # Should not be cached due to Vary: Cookie
        self.assertNotEqual(t, t2)
        
        time.sleep(0.01)
        
        # Should have been cached against cookie
        self.assertEqual(requests.get(
            'http://localhost:8000/time/',
            headers={'Cookie':'asdf=dsfg'}
        ).text, t2)
