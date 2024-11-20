#!/bin/bash

set -e

ARCH=${1:-x86_64}

./build_version.sh 3 6 15 $ARCH
./build_version.sh 3 7 17 $ARCH
./build_version.sh 3 8 20 $ARCH
./build_version.sh 3 9 20 $ARCH
./build_version.sh 3 10 15 $ARCH
./build_version.sh 3 11 10 $ARCH
./build_version.sh 3 12 7 $ARCH
./build_version.sh 3 13 0 $ARCH
