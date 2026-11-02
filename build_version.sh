#!/bin/bash
# Builds the development image (Dockerfile) for a Python version and
# architecture, which builds and installs wsgo from the local source, then
# runs a command in it (an interactive shell by default).
# usage: ./build_version.sh <python major version> <python minor version> <architecture> [command]
# eg: ./build_version.sh 3 12 x86_64 "python -m tests"
#
# Release wheels are built in a manylinux image by ./build_arch.sh, and
# ./run_tests.sh tests that wheel against each supported CPython.

PY_MAJ=${1:-3}
PY_MIN=${2:-10}

ARCH=${3:-"x86_64"}

case $ARCH in
  x86_64)
    PLATFORM=linux/amd64
    ;;
  arm64)
    PLATFORM=linux/arm64
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

ACTION=${4:-"bash"}

BUILD_ARGS="--platform ${PLATFORM} --build-arg PY_MAJ=${PY_MAJ} --build-arg PY_MIN=${PY_MIN}"

DOCKER_BUILDKIT=1 docker build ${BUILD_ARGS} --progress plain . || exit 1

# Use -t only when attached to a terminal, so this also works from scripts.
TTY_FLAGS=-i
[ -t 0 ] && [ -t 1 ] && TTY_FLAGS=-it

exec docker run ${TTY_FLAGS} --rm -u $(id -u):$(id -g) \
 --platform ${PLATFORM} \
 -v $PWD/dist:/output \
 -v $PWD/tests:/code/tests \
 $(DOCKER_BUILDKIT=1 docker build ${BUILD_ARGS} -q .) \
 bash -c "$ACTION"
