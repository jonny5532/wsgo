ARG PY_MAJ=3
ARG PY_MIN=10
ARG PY_PCH=15

# We don't run anything in this stage, but we'll steal files from it later.
FROM golang:bullseye AS golang

# We prefer older bullseye since we can generate binaries with a low glibc version requirement.
FROM python:${PY_MAJ}.${PY_MIN}.${PY_PCH}-bullseye AS python

# Copy entire golang toolchain across
COPY --from=golang /usr/local/go /usr/local/go

RUN pip install --upgrade wheel setuptools requests

ENV PATH=$PATH:/usr/local/go/bin
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

# Make the .pc file name consistent across all Python versions.
RUN cp $(ls /usr/local/lib/pkgconfig/python*.pc | head -n1) /usr/local/lib/pkgconfig/python-3-embed.pc

WORKDIR /code

# Install go dependencies

ADD go.mod go.sum /code/

RUN go mod download

# Build the main binary

ADD *.go /code/
ADD wsgo/ /code/wsgo/

RUN mkdir bin

RUN CGO_LDFLAGS=-no-pie go build -o bin/wsgo

# Create the .whl package

ADD setup.py README.md /code/

RUN LD_LIBRARY_PATH=/usr/local/lib python setup.py bdist_wheel
