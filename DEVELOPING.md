## How it's built

wsgo is a CPython extension module, `wsgo/_wsgo.abi3.so`, built from Go with
`go build -buildmode=c-shared` against the stable ABI
(`Py_LIMITED_API=0x030A0000`). One wheel per architecture covers CPython 3.10
and later. `setup.py` runs the Go build from its `build_ext` command.

To build the module in place, for a quick local test:

```sh
go build -buildmode=c-shared -ldflags="-s -w" -o wsgo/_wsgo.abi3.so .
rm -f wsgo/_wsgo.abi3.h
```

The module doesn't link libpython: the Python symbols are resolved when the
interpreter loads it. Standalone binaries, such as the Go test binaries, need
`-tags wsgo_embed` to link libpython themselves.


## Cross-compiling

Run this to enable local qemu-static shimming:

```sh
docker run --rm -it --privileged linuxserver/qemu-static --reset -p yes
```

(note that debian bullseye segfaults randomly on aarch64, so may need to build with bookworm and suffer the higher GLIBC requirement).


## Running tests locally

```sh
# Go unit tests
go test -tags wsgo_embed -race ./...

# Python tests, against a locally installed build
virtualenv /tmp/env && /tmp/env/bin/pip install requests .
source /tmp/env/bin/activate
python3 -m tests
```

To run everything in Docker, against CPython 3.10-3.14 and uv-managed
interpreters:

```sh
./run_tests.sh x86_64
```

You can also run a command in the build image for a particular Python
version, which has the wheel installed, eg:

```sh
./build_version.sh 3 12 x86_64 "python -m tests"
```


## Deploying to PyPI

```sh
./build.sh x86_64
./build.sh arm64
```

```sh
# Check the wheels only use the stable ABI

pip install abi3audit
abi3audit --strict dist/wsgo-0.1.0-cp310-abi3-*.whl
```

```sh
# Fix binary platform level using local auditwheel

pip install auditwheel\<6.4

auditwheel show dist/wsgo-0.1.0-cp310-abi3-linux_x86_64.whl
auditwheel repair --plat manylinux_2_17_x86_64 dist/wsgo-0.1.0-cp310-abi3-linux_x86_64.whl
```

```sh
# Fix binary platform level using docker (x86_64 and aarch64)

docker run -it --rm -v $PWD:/work -w /work --platform linux/amd64 $(docker build -q --platform linux/amd64 -f Dockerfile.auditwheel .) auditwheel repair --plat manylinux_2_17_x86_64 dist/wsgo-0.1.?-cp310-abi3-linux_x86_64.whl

docker run -it --rm -v $PWD:/work -w /work --platform linux/arm64 $(docker build -q --platform linux/arm64 -f Dockerfile.auditwheel .) auditwheel repair --plat manylinux_2_17_aarch64 dist/wsgo-0.1.?-cp310-abi3-linux_aarch64.whl
```

Use `auditwheel show` to find the lowest platform tag a wheel supports; a
wheel built on bookworm (eg. for aarch64) needs `manylinux_2_34`.

```sh
# Upload to PyPI
python3 -m twine upload wheelhouse/wsgo-<ver>*.whl
```
