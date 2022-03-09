from setuptools import setup
from setuptools.command.build_ext import build_ext
from setuptools.command.install import install
from setuptools.command.install_lib import install_lib
from setuptools.dist import Distribution

from wheel.bdist_wheel import bdist_wheel

wsgo_version = "0.0.1"


class CustomWheel(bdist_wheel):
    def finalize_options(self):
        bdist_wheel.finalize_options(self)
        self.root_is_pure = False

class CustomDistribution(Distribution):
    def __init__(self, *attrs):
        Distribution.__init__(self, *attrs)
#        self.cmdclass['install'] = CustomInstall
#        self.cmdclass['install_lib'] = CustomInstallLib
#        self.cmdclass['build_ext'] = CustomBuilder
        self.cmdclass['bdist_wheel'] = CustomWheel

    def is_pure(self):
        return False

setup(
    name='wsgo',
    version=wsgo_version,
    url='https://github.com/jonny5532/wsgo',
    license='MIT',
    author='jonny5532',
    #author_email='',
    install_requires=['setuptools'],
    python_requires=(
        ">=3.6"
    ),
    description="Simple and fast WSGI server in Go",
    long_description="",
    distclass=CustomDistribution,
    data_files=[ ('bin', ['wsgo']) ],
    zip_safe=False,
    #platforms='any',
    classifiers=[
        'Environment :: Web Environment',
        'Intended Audience :: Developers',
    ],
    options={'bdist_wheel': {'universal': False}},
)