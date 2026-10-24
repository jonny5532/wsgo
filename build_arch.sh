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

# Building for another architecture runs its binaries under qemu, which needs
# a binfmt handler (registrations don't survive a reboot).
if [ "$ARCH" = arm64 ] && [ "$(uname -m)" != aarch64 ] \
   && [ ! -e /proc/sys/fs/binfmt_misc/qemu-aarch64 ]; then
  echo "Warning: no qemu-aarch64 binfmt handler is registered, so the build will"
  echo "probably fail with 'exec format error'. Register it with:"
  echo "  docker run --rm --privileged linuxserver/qemu-static --reset -p yes"
fi

DOCKER_BUILDKIT=1 docker build \
 -f Dockerfile.manylinux \
 --platform ${PLATFORM} \
 --build-arg MANYLINUX=${MANYLINUX} \
 --target wheel \
 --output type=local,dest=wheelhouse \
 --progress plain \
 .
