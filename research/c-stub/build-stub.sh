#!/bin/bash
# Build stub_gpu_client.c into a shared library for LD_PRELOAD
# Cross-compiles for aarch64 Android using NDK
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
RESEARCH_DIR="$(dirname "$DIR")"

STUB_SRC="$DIR/stub_gpu_client.c"
OUT_DIR="$RESEARCH_DIR/output"

mkdir -p "$OUT_DIR"

echo "=== Building stub_gpu_client for LD_PRELOAD ==="

# Try NDK first
if [ -n "$NDK_CLANG" ]; then
    CLANG="$NDK_CLANG/aarch64-linux-android35-clang"
elif [ -n "$NDK" ] && [ -d "$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin" ]; then
    CLANG="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android35-clang"
else
    echo "NDK not found, building for host"
    CLANG="cc"
fi

echo "Compiler: $CLANG"
echo "Source:   $STUB_SRC"

# Build aarch64 shared library
$CLANG -shared -fPIC -Wall -Wextra \
    -o "$OUT_DIR/libstub_gpu_client_aarch64.so" \
    "$STUB_SRC" \
    -ldl

echo "  → $OUT_DIR/libstub_gpu_client_aarch64.so"

# Also build native for local testing
echo ""
echo "Building native stub for local testing..."
cc -shared -fPIC -Wall -Wextra \
    -o "$OUT_DIR/libstub_gpu_client.so" \
    "$STUB_SRC" \
    -ldl

echo "  → $OUT_DIR/libstub_gpu_client.so"
echo ""
echo "Done. Usage:"
echo "  TU_STUB_GPU=1 TU_STUB_SOCKET=/tmp/tu_stub_gpu.sock \\"
echo "  LD_PRELOAD=$OUT_DIR/libstub_gpu_client_aarch64.so \\"
echo "  vulkan.turnip.so"
