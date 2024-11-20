#!/bin/bash

set -e

ARCH=${1:-x86_64}

./build_version.sh 3  6  15 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3  7  17 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3  8  20 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3  9  20 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.?  -m tests"
./build_version.sh 3 10  15 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 11  10 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 12   7 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 13   0 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 14 0a1 $ARCH "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"

echo "All tests passed!"
