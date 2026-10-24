import os
import subprocess
import sys

from .utils import WsgoTestCase


class LaunchTests(WsgoTestCase):

    def test_python_m(self):
        self.start(python_m=True)
        self.assertEqual(self.get('/ok').status_code, 200)

    def test_process_manager(self):
        # The manager respawns server processes with `sys.executable -m wsgo`.
        self.start('--workers', '2', processes=2)
        self.assertTrue(self.wait_for_output('Process 1 starting'))
        self.assertTrue(self.wait_for_output('Process 2 starting'))
        self.assertEqual(self.get('/ok').status_code, 200)
        self.stop()
        self.assertTrue(self.wait_for_output('Process 1 has finished.'))
        self.assertTrue(self.wait_for_output('Process 2 has finished.'))

    def test_import_outside_server(self):
        # `import wsgo` (and registering a cron job) must be harmless when no
        # server is running, eg. from a management command.
        code = (
            'import wsgo\n'
            '@wsgo.timer(60)\n'
            'def tick(): pass\n'
            'assert issubclass(wsgo.RequestTimeoutException, Exception)\n'
            'assert wsgo.RequestTimeoutException.__module__ == "wsgo"\n'
            'print("imported", wsgo.RETRY)\n'
        )
        out = subprocess.run(
            [sys.executable, '-c', code],
            cwd=os.path.dirname(__file__),
            capture_output=True,
            timeout=10,
        )
        self.assertEqual(out.returncode, 0, out.stderr.decode('utf-8', 'replace'))
        self.assertIn(b'imported 1', out.stdout)
