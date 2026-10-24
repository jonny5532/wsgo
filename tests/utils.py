from concurrent.futures import ThreadPoolExecutor
import os
import requests
import subprocess
import sys
import threading
import time
import unittest


class WsgoTestCase(unittest.TestCase):
    # Which fixture application to start when a test doesn't name one.
    module = 'fixtures.echo'

    def setUp(self):
        self.process = None
        self.pool = ThreadPoolExecutor(max_workers=10)
        self._server_output = []
        self._output_lock = threading.Lock()

    def tearDown(self):
        if self.process:
            self.process.terminate()
            self.process.wait()
            for stream in (self.process.stdout, self.process.stderr):
                try:
                    if stream:
                        stream.close()
                except Exception:
                    pass

    def url(self, path):
        return 'http://localhost:8000' + path

    def get(self, path, **kwargs):
        kwargs.setdefault('timeout', 10)
        return requests.get(self.url(path), **kwargs)

    def post(self, path, **kwargs):
        kwargs.setdefault('timeout', 10)
        return requests.post(self.url(path), **kwargs)

    def start(self, *args, env=None, drain=True, module=None):
        proc_env = os.environ.copy()
        if env:
            proc_env.update(env)

        cmd = ['wsgo', '--module', module or self.module, '--process', '1']
        cmd += [str(arg) for arg in args]

        self.process = subprocess.Popen(
            cmd,
            cwd=os.path.dirname(__file__),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=proc_env,
        )

        # Wait for the server's first log line so tests don't race startup.
        first = self.process.stderr.readline()
        if first:
            text = first.decode('utf-8', 'replace')
            self._record_output(text)
            sys.stdout.write(text)
            sys.stdout.flush()

        # Keep draining both pipes (and remembering the output) so the server
        # never blocks on a full pipe. Some tests deliberately pass
        # drain=False and read the pipes themselves.
        if drain:
            self._start_reader(self.process.stdout, sys.stdout)
            self._start_reader(self.process.stderr, sys.stderr)

    def _record_output(self, text):
        with self._output_lock:
            self._server_output.append(text)

    def _start_reader(self, stream, out):
        def pump():
            for line in iter(stream.readline, b''):
                text = line.decode('utf-8', 'replace')
                self._record_output(text)
                out.write(text)
                out.flush()
            try:
                stream.close()
            except Exception:
                pass

        threading.Thread(target=pump, daemon=True).start()

    def wait_for_output(self, text, timeout=5.0):
        """Wait until the server has logged a line containing text."""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            with self._output_lock:
                if text in ''.join(self._server_output):
                    return True
            time.sleep(0.01)
        return False

    def server_output(self):
        """Return everything the server has written to stdout/stderr so far."""
        with self._output_lock:
            return ''.join(self._server_output)

    def stop(self):
        self.process.terminate()
        self.process.wait()
