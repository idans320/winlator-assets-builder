#!/bin/bash -e
# build.sh — cross-compile prove_heresies for aarch64 Android
#
# Usage:
#   NDK=$HOME/Android/Sdk/ndk/28.2.13676358 ./build.sh
# Or:
#   export NDK=... && ./build.sh
#
# Output: prove_heresies (static aarch64 ELF, push to device and run)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NDK="${NDK:-$HOME/Android/Sdk/ndk/28.2.13676358}"
SDK_VER="${SDK_VER:-35}"
TARGET="aarch64-linux-android${SDK_VER}"
TOOLCHAIN="${NDK}/toolchains/llvm/prebuilt/linux-x86_64/bin"
CC="${TOOLCHAIN}/${TARGET}-clang"
STRIP="${TOOLCHAIN}/llvm-strip"

if [ ! -x "$CC" ]; then
    echo "ERROR: NDK compiler not found at $CC" >&2
    echo "Set NDK to your NDK root directory." >&2
    exit 1
fi

echo "=== Building prove_heresies ==="
echo "CC:      $CC"
echo "Target:  $TARGET"
echo

CFLAGS="-static -pie -fPIE -O2 -Wall -Wextra"
OUT="$SCRIPT_DIR/prove_heresies"

echo "Compiling prove_heresies.S + prove_heresies.c → $OUT"
"$CC" $CFLAGS -o "$OUT" "$SCRIPT_DIR/prove_heresies.S" "$SCRIPT_DIR/prove_heresies.c"

echo "Stripping..."
"$STRIP" --strip-debug "$OUT"

echo
echo "=== Build complete ==="
file "$OUT"
ls -lh "$OUT"
echo
echo "Push to device and run:"
echo "  adb push $OUT /data/local/tmp/"
echo "  adb shell /data/local/tmp/prove_heresies"
