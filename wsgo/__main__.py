import os
import sys


def main():
    # Replaces PyConfig.buffered_stdio = 0 from the old embedded build.
    sys.stdout.reconfigure(line_buffering=True)
    sys.stderr.reconfigure(line_buffering=True)

    # Serve modules from the working directory. For a console script,
    # sys.path[0] is the bin/ directory rather than the working directory.
    sys.path.append(os.getcwd())

    import faulthandler
    faulthandler.enable()  # dump all thread tracebacks on error signals

    from . import _wsgo
    sys.exit(_wsgo.main(sys.argv[1:], sys.executable))


if __name__ == '__main__':
    main()
