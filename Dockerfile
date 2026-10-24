ARG PY_MAJ=3
ARG PY_MIN=10
ARG DEBIAN_RELEASE=bullseye

# We don't run anything in this stage, but we'll steal files from it later.
FROM golang:bullseye AS golang

# We prefer older bullseye since we can generate binaries with a low glibc version requirement.
FROM python:${PY_MAJ}.${PY_MIN}-${DEBIAN_RELEASE} AS python

# Copy entire golang toolchain across
COPY --from=golang /usr/local/go /usr/local/go

RUN pip install --upgrade wheel setuptools requests

ENV PATH=$PATH:/usr/local/go/bin
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

WORKDIR /code

# Install go dependencies

ADD go.mod go.sum /code/

RUN go mod download

# Build the .whl package (a cp310-abi3 extension module, so the Python
# version used here only needs to be the oldest supported one).

ADD *.go /code/
ADD internal/ /code/internal/
ADD wsgo/ /code/wsgo/
ADD setup.py pyproject.toml README.md /code/

RUN pip wheel --no-deps --no-build-isolation -w dist .

# Install it, so `wsgo` and `python -m tests` work inside the container.
RUN pip install --no-deps dist/*.whl
