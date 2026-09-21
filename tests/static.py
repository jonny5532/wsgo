import gzip
import os
import tempfile

from .utils import WsgoTestCase


class StaticTests(WsgoTestCase):

    def test_static_mapping_respects_path_segments(self):
        """
        A URL that only shares a string prefix with a static mapping (eg
        /mediaevil for /media) must not be served from the mapped root.
        """

        with tempfile.TemporaryDirectory() as webroot:
            os.makedirs(os.path.join(webroot, 'evil'))
            with open(os.path.join(webroot, 'evil', 'file.txt'), 'w') as f:
                f.write('sneaky')
            with open(os.path.join(webroot, 'file.txt'), 'w') as f:
                f.write('hello')

            self.start('--static-map', '/media=' + webroot)

            r = self.get('/mediaevil/file.txt')
            self.assertNotEqual(r.text, 'sneaky')

            # A genuine match still works.
            self.assertEqual(self.get('/media/file.txt').text, 'hello')

    def test_static_symlink_outside_root_is_not_served(self):
        """
        Symlinks that resolve outside the mapped root must not be served,
        while symlinks within the root still are.
        """

        with tempfile.TemporaryDirectory() as webroot, \
                tempfile.TemporaryDirectory() as outside:
            with open(os.path.join(outside, 'secret.txt'), 'w') as f:
                f.write('top-secret')
            os.symlink(os.path.join(outside, 'secret.txt'),
                       os.path.join(webroot, 'link.txt'))

            with open(os.path.join(webroot, 'real.txt'), 'w') as f:
                f.write('real')
            os.symlink('real.txt', os.path.join(webroot, 'alias.txt'))

            self.start('--static-map', '/=' + webroot)

            r = self.get('/link.txt')
            self.assertNotEqual(r.text, 'top-secret')

            self.assertEqual(self.get('/alias.txt').text, 'real')

    def test_static_gzip_negotiation(self):
        """
        gzip sidecars are only used when the client explicitly accepts gzip
        (honouring q-values), and responses advertise Vary: Accept-Encoding.
        """

        with tempfile.TemporaryDirectory() as webroot:
            body = b'plain body'
            with open(os.path.join(webroot, 'file.txt'), 'wb') as f:
                f.write(body)
            with open(os.path.join(webroot, 'file.txt.gz'), 'wb') as f:
                f.write(gzip.compress(body))

            self.start('--static-map', '/=' + webroot)

            r = self.get('/file.txt', headers={'Accept-Encoding': 'gzip'})
            self.assertEqual(r.status_code, 200)
            self.assertEqual(r.headers.get('Content-Encoding'), 'gzip')
            self.assertEqual(r.headers.get('Vary'), 'Accept-Encoding')
            # requests transparently decompresses Content-Encoding: gzip.
            self.assertEqual(r.content, body)

            # q=0 explicitly refuses gzip, so the plain file must be served.
            r = self.get('/file.txt', headers={'Accept-Encoding': 'gzip;q=0'})
            self.assertEqual(r.status_code, 200)
            self.assertIsNone(r.headers.get('Content-Encoding'))
            self.assertEqual(r.content, body)

            # No gzip in the request means the plain file.
            r = self.get('/file.txt', headers={'Accept-Encoding': 'identity'})
            self.assertEqual(r.status_code, 200)
            self.assertIsNone(r.headers.get('Content-Encoding'))
            self.assertEqual(r.content, body)

    def test_static_does_not_shadow_post(self):
        """
        Non-GET/HEAD methods must reach the WSGI application even when the
        URL matches a static file.
        """

        with tempfile.TemporaryDirectory() as webroot:
            with open(os.path.join(webroot, 'post.txt'), 'w') as f:
                f.write('static')
            self.start('--static-map', '/=' + webroot)

            r = self.post('/post.txt', data=b'hello')
            self.assertEqual(r.status_code, 200)
            self.assertNotEqual(r.text, 'static')
