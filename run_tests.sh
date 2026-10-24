#!/bin/bash
# Builds the wheel for an architecture, runs the Go unit tests, then installs
# the wheel into each supported CPython (and uv-managed CPythons) and runs the
# Python tests against it.
# usage: ./run_tests.sh [x86_64|arm64]

set -e

ARCH=${1:-x86_64}

case $ARCH in
  x86_64)
    PLATFORM=linux/amd64
    WHEEL_ARCH=x86_64
    ;;
  arm64)
    PLATFORM=linux/arm64
    WHEEL_ARCH=aarch64
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

VERSION=$(sed -n 's/^wsgo_version = "\(.*\)"/\1/p' setup.py)
WHEEL=wsgo-${VERSION}-cp310-abi3-linux_${WHEEL_ARCH}.whl

# Build the wheel into dist/, and run the Go tests in the same build image.
./build_arch.sh $ARCH "cp dist/*.whl /output && GOCACHE=/tmp/gocache go test -tags wsgo_embed -race ./..."

# Runs the Python tests in a container, with the wheel and tests mounted.
run_python_tests() {
  IMAGE=$1
  SETUP=$2
  docker run --rm \
   --platform ${PLATFORM} \
   -e PYTHONDONTWRITEBYTECODE=1 \
   -v $PWD/dist:/dist:ro \
   -v $PWD/tests:/code/tests:ro \
   -w /code \
   ${IMAGE} bash -c "set -e; ${SETUP}; python -m tests"
}

for PY_MIN in 10 11 12 13 14; do
  run_python_tests python:3.${PY_MIN}-slim \
   "pip install -q --root-user-action=ignore /dist/${WHEEL} requests"
done

# uv-managed interpreters, which embedding builds couldn't support.
for PY_VER in 3.12 3.14; do
  run_python_tests ghcr.io/astral-sh/uv:debian-slim \
   "uv venv -q --python ${PY_VER} /venv && uv pip install -q --python /venv /dist/${WHEEL} requests && export VIRTUAL_ENV=/venv PATH=/venv/bin:\$PATH"
done

echo "All tests passed!"
