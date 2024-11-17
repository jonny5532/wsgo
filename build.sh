#!/bin/bash

set -e

ARCH=${1:-x86_64}

./build_version.sh 3 6 15 python-3.6 $ARCH
./build_version.sh 3 7 17 python-3.7 $ARCH
./build_version.sh 3 8 20 python-3.8-embed $ARCH
./build_version.sh 3 9 20 python-3.9-embed $ARCH
./build_version.sh 3 10 15 python-3.10-embed $ARCH
./build_version.sh 3 11 10 python-3.11-embed $ARCH
./build_version.sh 3 12 7 python-3.12-embed $ARCH
./build_version.sh 3 13 0 python-3.13-embed $ARCH
