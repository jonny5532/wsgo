# usage ./build_version.sh <python major version> <python minor version> <python patch version> <pkgconfig name> <architecture>
# eg: ./build_version.sh 3 8 12 python-3.8-embed x86_64

PY_MAJ=${1:-3}
PY_MIN=${2:-10}
PY_PCH=${3:-11}
PY_PKGCONFIG=${4:-python-3.10-embed}

ARCH=${5:-"x86_64"}

PLATFORM=unknown
DEBIAN_VERSION=bullseye


case $ARCH in
  x86_64)
    PLATFORM=linux/amd64
    ;;
  arm64)
    PLATFORM=linux/arm64
    # need to use newer debian version for arm64
    DEBIAN_VERSION=bookworm
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac


ACTION=${6:-"cp bin/wsgo /output/wsgo-cp${PY_MAJ}${PY_MIN}-linux_${ARCH}; cp dist/*.whl /output"}


DOCKER_BUILDKIT=1 docker build \
 --platform ${PLATFORM} \
 --build-arg DEBIAN_VERSION=${DEBIAN_VERSION} \
 --build-arg PY_MAJ=${PY_MAJ} \
 --build-arg PY_MIN=${PY_MIN} \
 --build-arg PY_PCH=${PY_PCH} \
 --build-arg PY_PKGCONFIG=${PY_PKGCONFIG} \
 --progress plain \
 . || exit 1


exec docker run -it --rm -u $(id -u):$(id -g) \
 --platform ${PLATFORM} \
 -v $PWD/dist:/output \
 -v $PWD/tests:/code/tests \
  $( \
   DOCKER_BUILDKIT=1 docker build \
    --platform ${PLATFORM} \
    --build-arg DEBIAN_VERSION=${DEBIAN_VERSION} \
    --build-arg PY_MAJ=${PY_MAJ} \
    --build-arg PY_MIN=${PY_MIN} \
    --build-arg PY_PCH=${PY_PCH} \
    --build-arg PY_PKGCONFIG=${PY_PKGCONFIG} \
    -q . \
  ) bash -c "$ACTION"
