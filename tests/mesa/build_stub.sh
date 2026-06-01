#!/bin/bash -e
# Build stub_kgsl LD_PRELOAD library for both native and aarch64
TESTS_DIR="$(cd "$(dirname "$0")" && pwd)"

green='\033[0;32m'
nocolor='\033[0m'

echo -e "${green}=== Building stub_kgsl ===${nocolor}"

# --- Native stub ---
echo "Building native stub..."
gcc -shared -fPIC -Wall -Wextra -Wno-incompatible-pointer-types \
    -o "$TESTS_DIR/libstub_kgsl.so" \
    "$TESTS_DIR/stub_kgsl.c"
echo -e "${green}libstub_kgsl.so built${nocolor}"

# --- Aarch64 stub ---
NDK_CLANG="${NDK_CLANG:-$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin}"
if [ -x "$NDK_CLANG/aarch64-linux-android35-clang" ]; then
    echo "Building aarch64 stub..."
    "$NDK_CLANG/aarch64-linux-android35-clang" \
        -shared -fPIC -static -Wall -Wextra -Wno-incompatible-pointer-types \
        -o "$TESTS_DIR/libstub_kgsl_aarch64.so" \
        "$TESTS_DIR/stub_kgsl.c"
    echo -e "${green}libstub_kgsl_aarch64.so built${nocolor}"
fi

echo -e "${green}Stub build complete${nocolor}"
