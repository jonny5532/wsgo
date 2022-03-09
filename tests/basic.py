import os
import requests
import signal
import subprocess
import sys
import threading
import time
import unittest

class BasicTests(unittest.TestCase):
    def setUp(self):
        self.process = None

    def tearDown(self):
        if self.process:
            self.process.terminate()
            self.process.wait()

    def start(self, *args):
        self.process = subprocess.Popen(
            ['/code/wsgo'] + list(args),
            cwd=os.path.dirname(__file__),
            #start_new_session=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE
        )

        # Pass through process stdout/stderr to system one, which might not be a
        # proper fd if it is being buffered by unittest so we need to do it
        # manually.

        def passthrough(inp, out):
            while True:
                v = inp.read(1)
                if len(v)==0: 
                    break
                out.write(v.decode('utf-8'))
            inp.close()

        # Wait for process to return the first line of output
        print(self.process.stderr.readline())

        # Then shuffle the rest through with threads
        threading.Thread(target=passthrough, args=(self.process.stdout, sys.stdout)).start()
        threading.Thread(target=passthrough, args=(self.process.stderr, sys.stderr)).start()

    def test_get(self):
        self.start('--module', 'wsgi_app', '--process', '1')
        r = requests.get('http://localhost:8000')
        self.assertEqual(r.status_code, 200)

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

if __name__ == '__main__':
    unittest.main()
