FROM golang:1.20-bullseye

RUN apt-get update && apt-get install build-essential zlib1g-dev libncurses5-dev libgdbm-dev libnss3-dev libssl-dev libsqlite3-dev libreadline-dev libffi-dev curl libbz2-dev -y

ARG PY_MAJ=3
ARG PY_MIN=10
ARG PY_PCH=11
ARG PY_PKGCONFIG=python-3.10-embed

RUN cd /tmp \
 && wget -O python.tar.gz https://www.python.org/ftp/python/${PY_MAJ}.${PY_MIN}.${PY_PCH}/Python-${PY_MAJ}.${PY_MIN}.${PY_PCH}.tgz \
 && tar xzvf python.tar.gz

RUN cd /tmp/Python-${PY_MAJ}* \
 && ./configure --enable-shared \
 && make -j6 \
 && make altinstall

RUN LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/pip${PY_MAJ}.${PY_MIN} install --upgrade wheel setuptools requests

RUN cp /usr/local/lib/pkgconfig/${PY_PKGCONFIG}.pc /usr/local/lib/pkgconfig/python-3-embed.pc



WORKDIR /code

ADD go.mod go.sum /code/

RUN go mod download

ADD *.go /code/
ADD wsgo/ /code/wsgo/

RUN mkdir bin

#RUN --mount=type=cache,target=/root/.cache/go-build-py${PY_MAJ}${PY_MIN} \
RUN CGO_LDFLAGS=-no-pie go build -o bin/wsgo



ADD setup.py /code/

RUN LD_LIBRARY_PATH=/usr/local/lib /usr/local/bin/python${PY_MAJ}.${PY_MIN} setup.py bdist_wheel
