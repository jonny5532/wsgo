#!/bin/bash

set -e

ARCH=${1:-x86_64}

#./build_version.sh 3 6 15 python-3.6 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
./build_version.sh 3 7 17 python-3.7 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
./build_version.sh 3 8 20 python-3.8-embed $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
./build_version.sh 3 9 20 python-3.9-embed $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
# python-3.10-embed $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 11 10 python-3.11-embed $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 12 7 python-3.12-embed $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 13 0 python-3.13-embed $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 14 0a1 python-3.14-embed $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
