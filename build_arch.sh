#!/bin/bash
# Builds the manylinux cp310-abi3 release wheel for an architecture into
# wheelhouse/. The wheel covers every CPython from 3.10.
# usage: ./build_arch.sh [x86_64|arm64]

set -e

ARCH=${1:-x86_64}

case $ARCH in
  x86_64)
    PLATFORM=linux/amd64
    MANYLINUX=manylinux2014_x86_64
    ;;
  arm64)
    PLATFORM=linux/arm64
    MANYLINUX=manylinux2014_aarch64
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

DOCKER_BUILDKIT=1 docker build \
 -f Dockerfile.manylinux \
 --platform ${PLATFORM} \
 --build-arg MANYLINUX=${MANYLINUX} \
 --target wheel \
 --output type=local,dest=wheelhouse \
 --progress plain \
 .
