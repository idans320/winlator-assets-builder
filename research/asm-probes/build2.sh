#!/bin/bash -e
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NDK="${NDK:-$HOME/Android/Sdk/ndk/28.2.13676358}"
SDK_VER="${SDK_VER:-35}"
TARGET="aarch64-linux-android${SDK_VER}"
TOOLCHAIN="${NDK}/toolchains/llvm/prebuilt/linux-x86_64/bin"
CC="${TOOLCHAIN}/${TARGET}-clang"
STRIP="${TOOLCHAIN}/llvm-strip"

if [ ! -x "$CC" ]; then
    echo "ERROR: NDK compiler not found at $CC" >&2
    exit 1
fi

echo "=== Building prove_heresies2 ==="
OUT="$SCRIPT_DIR/prove_heresies2"
"$CC" -static -pie -fPIE -O2 -Wall -Wextra -o "$OUT" "$SCRIPT_DIR/prove_heresies2.S" "$SCRIPT_DIR/prove_heresies2.c" 2>&1
"$STRIP" --strip-debug "$OUT" 2>/dev/null || true
echo "=== Built: $(ls -lh "$OUT" | awk '{print $5}') ==="
file "$OUT"
