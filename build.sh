#!/bin/bash

set -e

ARCH=${1:-x86_64}

./build_version.sh 3  6  15 $ARCH
./build_version.sh 3  7  17 $ARCH
./build_version.sh 3  8  20 $ARCH
./build_version.sh 3  9  22 $ARCH
./build_version.sh 3 10  17 $ARCH
./build_version.sh 3 11  12 $ARCH
./build_version.sh 3 12   9 $ARCH
./build_version.sh 3 13   3 $ARCH
