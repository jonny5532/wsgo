import os
import subprocess
import sys
import tempfile

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

    def test_process_names(self):
        # The manager and server processes are named for ps/pgrep, rather
        # than showing as the Python interpreter.
        self.start('--workers', '2', processes=2)
        self.assertTrue(self.wait_for_output('Process 1 starting'))
        self.assertTrue(self.wait_for_output('Process 2 starting'))
        self.assertEqual(self.get('/ok').status_code, 200)

        def comm(pid):
            with open(f'/proc/{pid}/comm') as f:
                return f.read().strip()

        def children(pid):
            pids = []
            for entry in os.listdir('/proc'):
                if not entry.isdigit():
                    continue
                try:
                    with open(f'/proc/{entry}/stat') as f:
                        # The ppid follows the parenthesised name, which may
                        # itself contain spaces or parentheses.
                        stat = f.read().rsplit(')', 1)[1].split()
                except OSError:
                    continue
                if int(stat[1]) == pid:
                    pids.append(int(entry))
            return pids

        self.assertEqual(comm(self.process.pid), 'wsgo')
        self.assertEqual(
            sorted(comm(pid) for pid in children(self.process.pid)),
            ['wsgo-1', 'wsgo-2'],
        )

    def test_process_manager_local_wsgo_module(self):
        # A wsgo.py in the working directory mustn't shadow the package in the
        # respawned server processes.
        with tempfile.TemporaryDirectory() as appdir:
            with open(os.path.join(appdir, 'wsgo.py'), 'w') as f:
                f.write('raise ImportError("shadowed the wsgo package")\n')
            with open(os.path.join(appdir, 'local_app.py'), 'w') as f:
                f.write(
                    'def application(environ, start_response):\n'
                    '    start_response("200 OK", [("Content-Type", "text/plain")])\n'
                    '    return [b"local"]\n'
                )
            self.start(module='local_app', processes=1, cwd=appdir)
            self.assertTrue(self.wait_for_output('Process 1 starting'))
            self.assertEqual(self.get('/').text, 'local')
            self.assertNotIn('shadowed', self.server_output())
            self.stop()

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
