## Cross-compiling

Run this to enable local qemu-static shimming:

```sh
docker run --rm -it --privileged linuxserver/qemu-static --reset -p yes
```

(note that debian bullseye segfaults randomly on aarch64, so need to build with bookworm and suffer the higher GLIBC requirement).


## Deploying to PyPI

```sh
./build.sh x86_64
./build.sh arm64
```

```sh
# Fix binary platform level using local auditwheel
auditwheel repair --plat manylinux_2_17_x86_64 dist/*x86_64.whl
```

```sh
# Fix binary platform level using docker (x86_64 and aarch64)

docker run -it --rm -v $PWD:/work -w /work --platform linux/amd64 $(docker build -q --platform linux/amd64 -f Dockerfile.auditwheel .) auditwheel repair --plat manylinux_2_17_x86_64 dist/wsgo-0.0.??-*x86_64.whl

docker run -it --rm -v $PWD:/work -w /work --platform linux/arm64 $(docker build -q --platform linux/arm64 -f Dockerfile.auditwheel .) auditwheel repair --plat manylinux_2_17_aarch64 dist/wsgo-0.0.??-*aarch64.whl
```

```sh
# Upload to PyPI
python3 -m twine upload wheelhouse/wsgo-<ver>*.whl
```
