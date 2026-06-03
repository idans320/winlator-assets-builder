#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

T_DIR="$(cd "$(dirname "$0")" && pwd)"
PKG_DIR="$(dirname $(dirname $(dirname "$T_DIR")))"
FEX_ROOT="$PKG_DIR/fexcore/workdir/fex"

NDK_CLANG="${NDK_CLANG:-$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin}"
SDK_VER="${BENCH_SDK_VER:-35}"
TARGET="aarch64-linux-android${SDK_VER}"
BUILD_DIR="$FEX_ROOT/build-android"
SYSROOT="$NDK/toolchains/llvm/prebuilt/linux-x86_64/sysroot"
CXX="$NDK_CLANG/${TARGET}-clang++"
STRIP="$NDK_CLANG/llvm-strip"

echo -e "${green}=== FEXCore Real Source Android NDK Build ===${nocolor}"
echo "Source: $FEX_ROOT"

rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

INCLUDES="-I$FEX_ROOT/FEXCore/include \
  -I$FEX_ROOT/FEXCore/Source \
  -I$FEX_ROOT \
  -I$FEX_ROOT/Source \
  -I$FEX_ROOT/include \
  -I$FEX_ROOT/External/fmt/include \
  -I$FEX_ROOT/External/FEXHeaderUtils \
  -I$FEX_ROOT/External/xxhash \
  -I$FEX_ROOT/External/robin-map/include \
  -I$FEX_ROOT/External/unordered_dense/include"

CXXFLAGS="--sysroot=$SYSROOT -std=c++20 -O2 -Wall -DARCHITECTURE_arm64=1 $INCLUDES"

# Compile the real FEXCore LogManager (with our stderr patch)
echo "1. Compiling real FEXCore source files..."
for src in \
    "$FEX_ROOT/FEXCore/Source/Utils/LogManager.cpp" \
    "$FEX_ROOT/FEXCore/Source/Utils/Allocator.cpp" \
    "$FEX_ROOT/External/fmt/src/format.cc" \
    "$FEX_ROOT/External/fmt/src/os.cc" \
    ; do
    echo "  $(basename $src)"
    "$CXX" -c $CXXFLAGS "$src" -o "$BUILD_DIR/$(basename ${src%.*}.o)" || exit 1
done

# Stub for ForcedAssert (only called in Throw::MFmt which we never invoke)
echo "  ForcedAssert_stub.cpp"
cat > "$BUILD_DIR/ForcedAssert_stub.cpp" << 'STUBEOF'
namespace FEXCore::Assert { void ForcedAssert() {} }
STUBEOF
"$CXX" -c $CXXFLAGS "$BUILD_DIR/ForcedAssert_stub.cpp" -o "$BUILD_DIR/ForcedAssert_stub.o"

echo ""
echo "2. Building test harness against real PatternCache.h + LogManager.o..."
cat > "$BUILD_DIR/test.cpp" << 'TESTEOF'
#define FEXCORE_PATTERN_CACHE_NDK_TEST

#include "FEXCore/Source/Interface/Core/JIT/PatternCache.h"

int main() {
    fprintf(stderr, "=== FEXCore REAL PatternCache (from actual source) ===\n\n");

    FEXCore::CPU::PatternCache cache;

    fprintf(stderr, "IsEnabled: %s\n", cache.IsEnabled() ? "YES" : "NO");
    fprintf(stderr, "IsWarmup:  %s\n\n", cache.IsWarmup() ? "YES" : "NO");

    if (!cache.IsEnabled()) {
        fprintf(stderr, "Set FEXCORE_PATTERN_CACHE=1 to enable.\n");
        fprintf(stderr, "=== SKIPPED ===\n");
        return 0;
    }

    int total_ops = 5000;
    int phase_hits = 0;

    for (int i = 0; i < total_ops; i++) {
        uint64_t hash = ((uint64_t)(i % 15) << 32) | (i & 1);
        if (cache.Lookup(hash)) {
            cache.RecordHit();
            if (!cache.IsWarmup()) phase_hits++;
        } else {
            cache.RecordMiss();
            FEXCore::CPU::PatternTemplate tmpl{};
            tmpl.Hash = hash;
            tmpl.ByteSize = 16;
            cache.Insert(tmpl);
        }
        cache.TickWarmup();
    }

    cache.LogStats();

    int steady = total_ops - 500;
    fprintf(stderr, "Steady-state hits: %d / %d (%.1f%%)\n",
        phase_hits, steady, 100.0 * phase_hits / steady);

    bool pass = phase_hits > steady * 0.8;
    fprintf(stderr, "\n=== %s ===\n", pass ? "PASSED" : "FAILED");
    return pass ? 0 : 1;
}
TESTEOF

"$CXX" --sysroot="$SYSROOT" -std=c++20 -O2 -static \
    -DARCHITECTURE_arm64=1 $INCLUDES \
    "$BUILD_DIR/test.cpp" \
    "$BUILD_DIR/LogManager.o" \
    "$BUILD_DIR/Allocator.o" \
    "$BUILD_DIR/format.o" \
    "$BUILD_DIR/os.o" \
    "$BUILD_DIR/ForcedAssert_stub.o" \
    -o "$BUILD_DIR/fex-real-test" -lm

"$STRIP" "$BUILD_DIR/fex-real-test"

echo -e "${green}Binary: $BUILD_DIR/fex-real-test${nocolor}"
ls -lh "$BUILD_DIR/fex-real-test"
echo ""
echo "Deploy:"
echo "  adb push $BUILD_DIR/fex-real-test /data/local/tmp/"
echo "  adb shell FEXCORE_PATTERN_CACHE=1 /data/local/tmp/fex-real-test"
