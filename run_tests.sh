#!/bin/bash

set -e

./build_version.sh 3 6 15 python-3.6 "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
./build_version.sh 3 7 17 python-3.7 "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
./build_version.sh 3 8 20 python-3.8-embed "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
./build_version.sh 3 9 20 python-3.9-embed "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.? -m tests"
./build_version.sh 3 10 15 python-3.10-embed "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 11 10 python-3.11-embed "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 12 7 python-3.12-embed "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 13 0 python-3.13-embed "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
./build_version.sh 3 14 0a1 python-3.14-embed "LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python3.1? -m tests"
