#!/bin/bash

set -e

ARCH=${1:-x86_64}

./build_version.sh 3  6 $ARCH
./build_version.sh 3  7 $ARCH
./build_version.sh 3  8 $ARCH
./build_version.sh 3  9 $ARCH
./build_version.sh 3 10 $ARCH
./build_version.sh 3 11 $ARCH
./build_version.sh 3 12 $ARCH
./build_version.sh 3 13 $ARCH
./build_version.sh 3 14 $ARCH