#!/bin/bash -e
# Build test_kgsl_sync natively and for aarch64 (via NDK/QEMU)
TESTS_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$TESTS_DIR")")"

green='\033[0;32m'
nocolor='\033[0m'

echo -e "${green}=== Building Turnip sync tests ===${nocolor}"

# --- Native build (fast) ---
echo "Building native test..."
gcc -Wall -Wextra -pthread -o "$TESTS_DIR/test_kgsl_sync" \
    "$TESTS_DIR/test_kgsl_sync.c"
echo -e "${green}Native test built${nocolor}"

# --- Aarch64 cross-build (for QEMU) ---
NDK_CLANG="${NDK_CLANG:-$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin}"
if [ ! -x "$NDK_CLANG/aarch64-linux-android35-clang" ]; then
    echo "NDK cross-compiler not found at $NDK_CLANG (skipping aarch64 build)"
    exit 0
fi

echo "Building aarch64 test..."
"$NDK_CLANG/aarch64-linux-android35-clang" \
    -static -pthread -Wall -Wextra \
    -o "$TESTS_DIR/test_kgsl_sync_aarch64" \
    "$TESTS_DIR/test_kgsl_sync.c"
echo -e "${green}Aarch64 test built${nocolor}"

echo -e "${green}All builds complete${nocolor}"
