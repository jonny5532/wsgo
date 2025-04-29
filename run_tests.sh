#!/bin/bash

set -e

ARCH=${1:-x86_64}

./build_version.sh 3  6  15 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3  7  17 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3  8  20 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3  9  22 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3 10  17 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 11  12 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 12   9 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 13   3 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 14 0a7 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"

echo "All tests passed!"
