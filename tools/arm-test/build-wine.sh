#!/bin/bash
# Rebuild Wine (aarch64) incrementally inside the arm64 container.
#
#   tools/arm-test/build-wine.sh                          # everything
#   tools/arm-test/build-wine.sh dlls/ntdll/aarch64-windows/ntdll.dll
#
# This expects an ALREADY CONFIGURED build tree (WINE_BUILD). A from-scratch
# configure is not automated here because it needs the arm64ec cross toolchain
# that lives inside the existing build tree (/build/toolchain).
#
# Known wart: a full build fails to link dlls/winebus.sys (missing
# is_sdl_ignored_device) after everything useful is built; the exit status is
# reported rather than hidden.
source "$(dirname "$0")/lib.sh"

require_docker
build_image

[ -f "$WINE_BUILD/Makefile" ] \
  || { echo "no configured build tree in $WINE_BUILD (set WINE_BUILD)" >&2; exit 1; }

TARGETS="$*"

docker run --rm --platform linux/arm64 \
  -v "$(abs "$WINE_BUILD"):/build" \
  -v "$(abs "$WINE_SRC"):/src:ro" \
  -e TARGETS="$TARGETS" \
  "$IMAGE" bash -c '
    export PATH=/build/toolchain/bin:$PATH
    cd /build
    echo "== build ${TARGETS:-all} =="
    make -j"$(nproc)" $TARGETS > make.log 2>&1
    status=$?
    echo "make_exit=$status"
    grep -E "error:" make.log | head -5 || true
    exit $status
  '
