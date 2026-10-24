#!/bin/bash

set -e

ARCH=${1:-x86_64}

./build_arch.sh $ARCH
