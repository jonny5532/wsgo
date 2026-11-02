# Development image: a Debian CPython with the Go toolchain, used for the Go
# unit tests (which link against libpython) and ad-hoc shells, via
# ./build_version.sh. Release wheels are built by Dockerfile.manylinux, and
# ./run_tests.sh tests that wheel against each CPython.
ARG PY_MAJ=3
ARG PY_MIN=10
ARG GO_MINOR=1.27

# We don't run anything in this stage, but we'll steal files from it later.
# (Go is statically linked, so the toolchain runs on any base.)
FROM golang:${GO_MINOR}-trixie AS golang

FROM python:${PY_MAJ}.${PY_MIN}-trixie AS python

# Copy entire golang toolchain across
COPY --from=golang /usr/local/go /usr/local/go

RUN pip install --root-user-action=ignore --upgrade wheel setuptools requests

ENV PATH=$PATH:/usr/local/go/bin
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

# build_version.sh runs containers as the host uid, which has no home dir
# here, so use a fixed module cache that it can read.
ENV GOPATH=/go
ENV GOMODCACHE=/go/pkg/mod

WORKDIR /code

# Install go dependencies

ADD go.mod go.sum /code/

RUN go mod download && chmod -R a+rX /go

# Build a wheel from the local source against this image's Python. It's
# tagged cp310-abi3 but compiled against this version's headers, so it isn't
# a substitute for testing the release wheel.

ADD *.go /code/
ADD internal/ /code/internal/
ADD wsgo/ /code/wsgo/
ADD setup.py pyproject.toml README.md /code/

RUN pip wheel --no-deps --no-build-isolation -w dist .

# Install it, so `wsgo` and `python -m tests` work inside the container.
RUN pip install --root-user-action=ignore --no-deps dist/*.whl

# Writable Go build cache for the host uid at runtime. (Set after the build
# steps, so it isn't created by root during the image build.)
ENV GOCACHE=/tmp/gocache
