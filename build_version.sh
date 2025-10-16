# usage ./build_version.sh <python major version> <python minor version> <python patch version> <pkgconfig name> <architecture>
# eg: ./build_version.sh 3 8 12 python-3.8-embed x86_64

PY_MAJ=${1:-3}
PY_MIN=${2:-10}

ARCH=${3:-"x86_64"}

DEBIAN_RELEASE=bullseye

if [ "$PY_MIN" -ge 14 ]; then
  DEBIAN_RELEASE=bookworm
fi

PLATFORM=unknown

case $ARCH in
  x86_64)
    PLATFORM=linux/amd64
    ;;
  arm64)
    PLATFORM=linux/arm64
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac


ACTION=${4:-"cp bin/wsgo /output/wsgo-cp${PY_MAJ}${PY_MIN}-linux_${ARCH}; cp dist/*.whl /output"}


DOCKER_BUILDKIT=1 docker build \
 --platform ${PLATFORM} \
 --build-arg PY_MAJ=${PY_MAJ} \
 --build-arg PY_MIN=${PY_MIN} \
 --build-arg DEBIAN_RELEASE=${DEBIAN_RELEASE} \
 --progress plain \
 . || exit 1


exec docker run -it --rm -u $(id -u):$(id -g) \
 --platform ${PLATFORM} \
 -v $PWD/dist:/output \
 -v $PWD/tests:/code/tests \
  $( \
   DOCKER_BUILDKIT=1 docker build \
    --platform ${PLATFORM} \
    --build-arg PY_MAJ=${PY_MAJ} \
    --build-arg PY_MIN=${PY_MIN} \
    --build-arg DEBIAN_RELEASE=${DEBIAN_RELEASE} \
    -q . \
  ) bash -c "$ACTION"
