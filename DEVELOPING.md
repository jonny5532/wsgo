## Deploying to PyPI

./build.sh
auditwheel repair --plat manylinux_2_17_x86_64 dist/*.whl
python3 -m twine upload wheelhouse/wsgo-<ver>*.whl
