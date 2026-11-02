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

(note that debian bullseye segfaults randomly on aarch64, which affected the old Debian-based release builds).


## Running tests locally

```sh
# Go unit tests
go test -tags wsgo_embed -race ./...

# Python tests, against a locally installed build
virtualenv /tmp/env && /tmp/env/bin/pip install requests .
source /tmp/env/bin/activate
python3 -m tests
```

To run everything in Docker, against CPython 3.10-3.15 and uv-managed
interpreters:

```sh
./run_tests.sh x86_64
```

`./run_tests.sh arm64` builds and tests the arm64 wheel too, usually under
qemu, so it skips the Go tests and only covers CPython 3.10 and 3.15 (plus
uv's 3.15).

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

This builds the wheels with `Dockerfile.manylinux`, in the official
`manylinux2014` images (glibc 2.17) with a recent Go toolchain, and repairs
them with auditwheel, leaving `wheelhouse/wsgo-<ver>-cp310-abi3-manylinux2014_<arch>.manylinux_2_17_<arch>.whl`.
To use a newer Go, change `GO_VERSION` in `Dockerfile.manylinux`.

```sh
# Check the wheels only use the stable ABI

pip install abi3audit
abi3audit --strict wheelhouse/wsgo-0.1.0-cp310-abi3-*.whl
```

```sh
# Upload to PyPI
python3 -m twine upload wheelhouse/wsgo-<ver>*.whl
```
