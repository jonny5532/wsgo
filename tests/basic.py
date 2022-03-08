import os
import requests
import signal
import subprocess
import time
import unittest

class BasicTests(unittest.TestCase):
    def setUp(self):
        self.process = None

    def tearDown(self):
        if self.process:
            self.process.terminate()
            # print('pgid:', os.getpgid(self.process.pid))
            # os.killpg(os.getpgid(self.process.pid), signal.SIGKILL)
            self.process.wait()

    def start(self, *args):
        self.process = subprocess.Popen(
            ['/code/wsgo'] + list(args),
            cwd=os.path.dirname(__file__),
            start_new_session=True,
        )
        time.sleep(0.1)

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


    # def test_upper(self):
    #     self.assertEqual('foo'.upper(), 'FOO')

    # def test_isupper(self):
    #     self.assertTrue('FOO'.isupper())
    #     self.assertFalse('Foo'.isupper())

    # def test_split(self):
    #     s = 'hello world'
    #     self.assertEqual(s.split(), ['hello', 'world'])
    #     # check that s.split fails when the separator is not a string
    #     with self.assertRaises(TypeError):
    #         s.split(2)

if __name__ == '__main__':
    unittest.main()
