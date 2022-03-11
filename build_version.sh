# usage ./build_version.sh <python major version> <python minor version> <python patch version> <pkgconfig name>
# eg: ./build_version.sh 3 8 12 python-3.8-embed

PY_MAJ=${1:-3}
PY_MIN=${2:-8}

ACTION=${5:-"cp bin/wsgo /output/wsgo-cp${PY_MAJ}${PY_MIN}-linux_x86_64; cp dist/*.whl /output"}

exec docker run -it --rm -u $(id -u):$(id -g) -v $PWD/dist:/output -v $PWD/tests:/tests $( \
 DOCKER_BUILDKIT=1 docker build \
  --build-arg PY_MAJ=${PY_MAJ} \
  --build-arg PY_MIN=${PY_MIN} \
  --build-arg PY_PCH=${3:-12} \
  --build-arg PY_PKGCONFIG=${4:-python-3.8-embed} \
  -q . \
) bash -c "$ACTION"
