#!/bin/bash
# Builds the release wheel for an architecture, runs the Go unit tests, then
# installs the wheel into each supported CPython (and uv-managed CPythons) and
# runs the Python tests against it.
#
# arm64 usually runs under qemu, so it skips the Go tests (which are
# architecture-independent and covered by the x86_64 run) and only tests the
# wheel on the oldest and newest CPython.
# usage: ./run_tests.sh [x86_64|arm64]

set -e

ARCH=${1:-x86_64}

case $ARCH in
  x86_64)
    PLATFORM=linux/amd64
    WHEEL_ARCH=x86_64
    RUN_GO_TESTS=1
    PY_MINS="10 11 12 13 14 15"
    UV_PY_VERS="3.12 3.15"
    ;;
  arm64)
    PLATFORM=linux/arm64
    WHEEL_ARCH=aarch64
    RUN_GO_TESTS=0
    PY_MINS="10 15"
    UV_PY_VERS="3.15"
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

VERSION=$(sed -n 's/^wsgo_version = "\(.*\)"/\1/p' setup.py)
# Expanded inside the test containers.
WHEEL="wsgo-${VERSION}-cp310-abi3-manylinux*_${WHEEL_ARCH}.whl"

# Build the release wheel into wheelhouse/.
./build_arch.sh $ARCH

# The Go tests need libpython to link, which the Debian image provides.
if [ "$RUN_GO_TESTS" = 1 ]; then
  ./build_version.sh 3 10 $ARCH "go test -tags wsgo_embed -race ./..."
else
  echo "Skipping Go tests on $ARCH"
fi

# Runs the Python tests in a container, with the wheel and tests mounted.
run_python_tests() {
  IMAGE=$1
  SETUP=$2
  docker run --rm \
   --platform ${PLATFORM} \
   -e PYTHONDONTWRITEBYTECODE=1 \
   -v $PWD/wheelhouse:/wheelhouse:ro \
   -v $PWD/tests:/code/tests:ro \
   -w /code \
   ${IMAGE} bash -c "set -e; ${SETUP}; python -m tests"
}

for PY_MIN in $PY_MINS; do
  run_python_tests python:3.${PY_MIN}-slim \
   "pip install -q --root-user-action=ignore /wheelhouse/${WHEEL} requests"
done

# uv-managed interpreters, which embedding builds couldn't support.
for PY_VER in $UV_PY_VERS; do
  run_python_tests ghcr.io/astral-sh/uv:debian-slim \
   "uv venv -q --python ${PY_VER} /venv && uv pip install -q --python /venv /wheelhouse/${WHEEL} requests && export VIRTUAL_ENV=/venv PATH=/venv/bin:\$PATH"
done

echo "All tests passed!"
