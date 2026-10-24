import os
import subprocess

from setuptools import Extension, setup
from setuptools.command.build_ext import build_ext

wsgo_version = "0.1.0"

# The oldest CPython whose stable ABI we target. Must match the
# Py_LIMITED_API value in internal/wsgo/python_interface.go.
LIMITED_API_HEX = "0x030A0000"
LIMITED_API_TAG = "cp310"

from pathlib import Path
this_directory = Path(__file__).parent
long_description = (this_directory / "README.md").read_text()


class GoBuildExt(build_ext):
    """Builds the _wsgo extension module with `go build -buildmode=c-shared`."""

    def build_extension(self, ext):
        output = self.get_ext_fullpath(ext.name)
        os.makedirs(os.path.dirname(output), exist_ok=True)

        env = os.environ.copy()
        env["CGO_ENABLED"] = "1"
        env["CGO_CFLAGS"] = (
            env.get("CGO_CFLAGS", "-O2 -g") + " -DPy_LIMITED_API=" + LIMITED_API_HEX
        )

        # -buildvcs=false: the wheel doesn't need Go's VCS stamp, and stamping
        # fails when git rejects the checkout (eg. a mount owned by another
        # user in a build container).
        subprocess.check_call(
            ["go", "build", "-buildmode=c-shared", "-buildvcs=false", "-ldflags=-s -w", "-o", output, "."],
            cwd=this_directory,
            env=env,
        )

        # go build also writes a C header for the exports, which we don't need.
        header = os.path.splitext(output)[0] + ".h"
        if os.path.exists(header):
            os.remove(header)


setup(
    name='wsgo',
    version=wsgo_version,
    url='https://github.com/jonny5532/wsgo',
    license='MIT',
    author='jonny5532',
    #author_email='',
    python_requires=(
        ">=3.10"
    ),
    description="Simple and fast WSGI server in Go",
    long_description=long_description,
    long_description_content_type="text/markdown",
    packages=['wsgo'],
    ext_modules=[
        Extension('wsgo._wsgo', sources=[], py_limited_api=True),
    ],
    cmdclass={'build_ext': GoBuildExt},
    entry_points={
        'console_scripts': ['wsgo = wsgo.__main__:main'],
    },
    zip_safe=False,
    #platforms='any',
    classifiers=[
        'Environment :: Web Environment',
        'Intended Audience :: Developers',
    ],
    options={'bdist_wheel': {'py_limited_api': LIMITED_API_TAG}},
)
