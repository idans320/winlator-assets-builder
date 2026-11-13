#!/bin/bash
# ci/docker-entrypoint.sh — entrypoint for ci/Dockerfile.test.
#
#   auto     static gate unless ANDROID_SERIAL is set, then Android run
#   static   ci/check-machine-types.sh on the baked artifacts (no device)
#   android  ci/android-run-tests.sh against $ANDROID_SERIAL
#   shell    interactive shell
#
# Exits nonzero when a gate or a test fails, so `docker run` is CI-usable.
set -uo pipefail

ART="${ARTIFACTS:-/opt/artifacts}"
CI_DIR="$(cd "$(dirname "$0")" && pwd)"

mode="${1:-auto}"
[ $# -gt 0 ] && shift

find_first() {
    local pattern
    for pattern in "$@"; do
        local match
        match="$(find "$ART" -maxdepth 3 -name "$pattern" 2> /dev/null | sort | head -1)"
        if [ -n "$match" ]; then
            printf '%s\n' "$match"
            return 0
        fi
    done
    return 1
}

FEXCORE="$(find_first 'fexcore-*.wcp' 'fexcore-*.zip')" || true
DXVK="$(find_first 'dxvk-*.wcp' '*-arm64ec-gplasync.wcp' 'dxvk-*.zip')" || true
WINE_PKG="$(find_first 'wine-arm64-*.wcp' 'wine-*.wcp')" || true
TESTS=""
for candidate in "$ART/tests" "$ART/pe-tests" "$ART/ci/build/tests"; do
    if [ -d "$candidate" ]; then
        TESTS="$candidate"
        break
    fi
done
TERMUX_ROOTFS=""
for candidate in "$ART/termux-rootfs" "$ART/termux_rootfs"; do
    if [ -d "$candidate" ]; then
        TERMUX_ROOTFS="$candidate"
        break
    fi
done

if [ "$mode" = "auto" ]; then
    if [ -n "${ANDROID_SERIAL:-}" ]; then
        mode="android"
    else
        mode="static"
    fi
fi

case "$mode" in
    static)
        args=(--tests "$TESTS")
        [ -n "$FEXCORE" ] && args+=(--fexcore "$FEXCORE")
        [ -n "$DXVK" ] && args+=(--dxvk "$DXVK")
        [ -n "$WINE_PKG" ] && args+=(--wine "$WINE_PKG")
        exec "$CI_DIR/check-machine-types.sh" "${args[@]}" "$@"
        ;;
    android)
        [ -n "$WINE_PKG" ] || {
            echo "error: no wine-arm64-*.wcp in $ART" >&2
            echo "       the Android runtime test needs the wine package (build-wine job)" >&2
            exit 2
        }
        [ -n "${ANDROID_SERIAL:-}" ] || {
            echo "error: ANDROID_SERIAL is not set" >&2
            echo "       start an Android emulator/device first, or use the 'static' mode" >&2
            exit 2
        }
        args=(--serial "$ANDROID_SERIAL" --wine "$WINE_PKG" --tests "$TESTS")
        [ -n "$FEXCORE" ] && args+=(--fexcore "$FEXCORE")
        [ -n "$DXVK" ] && args+=(--dxvk "$DXVK")
        if [ -n "$TERMUX_ROOTFS" ]; then
            args+=(--termux-rootfs "$TERMUX_ROOTFS")
        else
            # The device/emulator is assumed to already have Termux installed.
            args+=(--no-rootfs)
        fi
        exec "$CI_DIR/android-run-tests.sh" "${args[@]}" "$@"
        ;;
    shell)
        exec bash "$@"
        ;;
    *)
        echo "unknown mode: $mode (auto|static|android|shell)" >&2
        exit 2
        ;;
esac
