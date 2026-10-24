#!/bin/bash
# Builds the cp310-abi3 wheel for an architecture into dist/. The wheel covers
# every CPython from 3.10, so it's built against the oldest one.
# usage: ./build_arch.sh [x86_64|arm64] [command]

ARCH=${1:-x86_64}

exec ./build_version.sh 3 10 $ARCH "${2:-cp dist/*.whl /output}"
