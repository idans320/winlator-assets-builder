#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

T_DIR="$(cd "$(dirname "$0")" && pwd)"
FEX_ROOT="$HOME/winlator-cmod-builder/fexcore/workdir/fex"

NDK_CLANG="${NDK_CLANG:-$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin}"
SDK_VER="${BENCH_SDK_VER:-35}"
TARGET="aarch64-linux-android${SDK_VER}"
SYSROOT="$NDK/toolchains/llvm/prebuilt/linux-x86_64/sysroot"
CXX="$NDK_CLANG/${TARGET}-clang++"
STRIP="$NDK_CLANG/llvm-strip"
OUT="$T_DIR/fex-pattern-test"

echo -e "${green}=== FEXCore REAL Source NDK Build ===${nocolor}"
echo "Source: $FEX_ROOT"

CXXFLAGS="--sysroot=$SYSROOT -std=c++20 -O2 -Wall -static \
  -I$FEX_ROOT/FEXCore/Source \
  -I$FEX_ROOT/FEXCore/include \
  -I$FEX_ROOT"

"$CXX" $CXXFLAGS "$T_DIR/fex_pattern_test.cpp" -o "$OUT" -lm
"$STRIP" "$OUT"

echo -e "${green}Built: $OUT${nocolor}"
ls -lh "$OUT"
echo ""
echo "Deploy:"
echo "  adb push $OUT /data/local/tmp/"
echo "  adb shell FEXCORE_PATTERN_CACHE=1 /data/local/tmp/fex-pattern-test"
