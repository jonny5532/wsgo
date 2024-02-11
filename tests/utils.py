from concurrent.futures import ThreadPoolExecutor
import os
import subprocess
import sys
import threading
import unittest

class WsgoTestCase(unittest.TestCase):
    def setUp(self):
        self.process = None
        self.pool = ThreadPoolExecutor(max_workers=10)

    def tearDown(self):
        if self.process:
            self.process.terminate()
            self.process.wait()

    def start(self, *args):
        self.process = subprocess.Popen(
            ['/code/bin/wsgo'] + list(args),
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
