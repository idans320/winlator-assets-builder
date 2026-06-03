#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

TESTS_DIR="$(cd "$(dirname "$0")" && pwd)"
NDK_CLANG="${NDK_CLANG:-$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin}"
SDK_VER="${BENCH_SDK_VER:-35}"
TARGET="aarch64-linux-android${SDK_VER}"
OUTPUT="$TESTS_DIR/fp16-ndk-test"

echo -e "${green}=== DXVK FP16 NDK Test Builder ===${nocolor}"

if [ ! -x "$NDK_CLANG/${TARGET}-clang++" ]; then
    echo -e "${red}NDK clang++ not found: $NDK_CLANG/${TARGET}-clang++${nocolor}"
    exit 1
fi

SYSROOT="$NDK/toolchains/llvm/prebuilt/linux-x86_64/sysroot"

echo "Compiling fp16_ndk_test.cpp..."
"$NDK_CLANG/${TARGET}-clang++" \
    --sysroot="$SYSROOT" \
    -std=c++17 -static \
    -Wall -Wextra -O2 -flto \
    -DANDROID \
    -o "$OUTPUT" \
    "$TESTS_DIR/fp16_ndk_test.cpp" \
    -lm

"$NDK_CLANG/llvm-strip" "$OUTPUT"

echo -e "${green}Test binary: $OUTPUT${nocolor}"
ls -lh "$OUTPUT"

echo ""
echo "Deploy:"
echo "  adb push $OUTPUT /data/local/tmp/"
echo "  adb shell chmod +x /data/local/tmp/fp16-ndk-test"
echo "  adb shell /data/local/tmp/fp16-ndk-test"
