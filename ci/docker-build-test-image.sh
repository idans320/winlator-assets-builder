#!/bin/bash
# ci/docker-build-test-image.sh — assemble the ci/Dockerfile.test build context
# and build the ARM64 test kit image.
#
# The repo root is a bad Docker context (hundreds of MB of .wcp/.apk files), so
# this stages only what the image needs:
#
#   <tmp>/ci/            copy of ci/
#   <tmp>/artifacts/     wcp files, test exes, optional Termux rootfs
#   <tmp>/Dockerfile.test
#
# Usage:
#   ci/docker-build-test-image.sh \
#       --wine wine-arm64-*.wcp --fexcore fexcore-*.wcp --dxvk dxvk-*.wcp \
#       [--tests ci/build/tests] [--termux-rootfs wine/workdir/termux-rootfs] \
#       [--tag winlator-test:arm64] [--platform linux/arm64] [--no-build]
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TAG="winlator-test:arm64"
PLATFORM="linux/arm64"
TESTS="$ROOT_DIR/ci/build/tests"
TERMUX_ROOTFS=""
WINE=""
FEXCORE=""
DXVK=""
DO_BUILD=1

while [ $# -gt 0 ]; do
    case "$1" in
        --wine) WINE="${2:?}"; shift ;;
        --fexcore) FEXCORE="${2:?}"; shift ;;
        --dxvk) DXVK="${2:?}"; shift ;;
        --tests) TESTS="${2:?}"; shift ;;
        --termux-rootfs) TERMUX_ROOTFS="${2:?}"; shift ;;
        --tag) TAG="${2:?}"; shift ;;
        --platform) PLATFORM="${2:?}"; shift ;;
        --no-build) DO_BUILD=0 ;;
        -h | --help)
            sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

STAGE="$(mktemp -d "${TMPDIR:-/tmp}/winlator-test-image.XXXXXX")"
echo "staging context: $STAGE"

mkdir -p "$STAGE/artifacts"
# Copy ci/ but not build outputs (ci/build may contain a 1 GiB test Wine).
tar -C "$ROOT_DIR" --exclude='ci/build' -cf - ci | tar -C "$STAGE" -xf -
cp -f "$ROOT_DIR/ci/Dockerfile.test" "$STAGE/Dockerfile.test"

copy_artifact() {
    local src="$1"
    [ -n "$src" ] || return 0
    [ -e "$src" ] || {
        echo "warning: not found: $src" >&2
        return 0
    }
    if [ -d "$src" ]; then
        cp -a "$src" "$STAGE/artifacts/$(basename "$src")"
    else
        cp -f "$src" "$STAGE/artifacts/"
    fi
    echo "  + $(basename "$src")"
}

copy_artifact "$FEXCORE"
copy_artifact "$DXVK"
copy_artifact "$WINE"
if [ -d "$TESTS" ]; then
    mkdir -p "$STAGE/artifacts/tests"
    cp -f "$TESTS"/*.exe "$STAGE/artifacts/tests/" 2> /dev/null || true
    echo "  + tests/*.exe"
fi
if [ -n "$TERMUX_ROOTFS" ]; then
    copy_artifact "$TERMUX_ROOTFS"
fi

if [ "$DO_BUILD" -eq 0 ]; then
    echo "context ready (--no-build): $STAGE"
    exit 0
fi

command -v docker > /dev/null 2>&1 || {
    echo "error: docker not found" >&2
    exit 1
}

# BuildKit is required for the syntax directive + future cache mounts.
docker buildx build \
    --platform "$PLATFORM" \
    -f "$STAGE/Dockerfile.test" \
    -t "$TAG" \
    --load \
    "$STAGE"

cat << EOF

Built $TAG ($PLATFORM).

  static gate (no device):
    docker run --rm $TAG static

  Android device/emulator:
    docker run --rm --network host -e ANDROID_SERIAL=emulator-5554 $TAG android

  NOTE: the image cannot execute the bionic Wine itself. It needs an ARM64
  Android target; see ci/README.md.
EOF
