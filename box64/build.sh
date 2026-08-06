#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

PACKAGE_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$PACKAGE_DIR")"
WORKDIR="$PACKAGE_DIR/workdir"
PKG_NAME="box64"

BOX64_REPO=$(yq ".${PKG_NAME}.repo" "$ROOT_DIR/packages.yml")
BOX64_BRANCH=$(yq ".${PKG_NAME}.branch" "$ROOT_DIR/packages.yml")
SDK_VER=$(yq ".${PKG_NAME}.sdk_ver" "$ROOT_DIR/packages.yml")
NDK_CLANG="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin"

echo -e "${green}=== Box64 Builder ===${nocolor}"

if [ -z "$NDK" ] || [ -z "$ANDROID_HOME" ]; then
    echo -e "${red}Not inside Nix dev shell. Run: nix develop /app/mesa-builder#box64${nocolor}"
    exit 1
fi

if [ ! -d "$NDK_CLANG" ]; then
    echo -e "${red}NDK toolchain not found at $NDK_CLANG${nocolor}"
    exit 1
fi

mkdir -p "$WORKDIR"
if [ ! -d "$WORKDIR/box64" ]; then
    echo "Cloning Box64 (branch: $BOX64_BRANCH)..."
    git clone --depth 1 --branch "$BOX64_BRANCH" "$BOX64_REPO" "$WORKDIR/box64" 2>&1
fi

BOX64_VERSION=$(cd "$WORKDIR/box64" && git rev-parse --short HEAD)
echo -e "${green}Box64 version: $BOX64_VERSION${nocolor}"

BUILD_DIR="$WORKDIR/box64/build-android"
mkdir -p "$BUILD_DIR"

if [ ! -f "$BUILD_DIR/CMakeCache.txt" ]; then
    echo "Configuring CMake..."
    cmake -B "$BUILD_DIR" -S "$WORKDIR/box64" \
        -DCMAKE_TOOLCHAIN_FILE="$NDK/build/cmake/android.toolchain.cmake" \
        -DANDROID_ABI=arm64-v8a \
        -DANDROID_PLATFORM="android-$SDK_VER" \
        -DCMAKE_BUILD_TYPE=Release \
        -DARM_DYNAREC=ON \
        -DLIBSDL2=OFF \
        -DNOGIT=ON \
        -DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=BOTH \
        -DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=BOTH &> "$WORKDIR/cmake_log"
fi

echo "Building..."
cmake --build "$BUILD_DIR" -j$(nproc) 2>&1 | tee "$WORKDIR/build_log"

BOX64_BIN="$BUILD_DIR/box64"
if [ ! -f "$BOX64_BIN" ]; then
    echo -e "${red}Build failed: box64 binary not found${nocolor}"
    exit 1
fi
echo -e "${green}Build successful${nocolor}"

echo "Stripping..."
"$NDK_CLANG/llvm-strip" "$BOX64_BIN"

echo "Packaging..."
PKGDIR="$WORKDIR/package"
mkdir -p "$PKGDIR"
cp "$BOX64_BIN" "$PKGDIR/box64"

cat > "$PKGDIR/meta.json" << METAEOF
{
  "schemaVersion": 1,
  "name": "Box64 $BOX64_VERSION",
  "description": "Box64 x86_64 emulator for ARM64 Android",
  "author": "ptitSeb",
  "packageVersion": "$BOX64_VERSION",
  "vendor": "Box64",
  "minApi": 27,
  "libraryName": "box64"
}
METAEOF

OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$BOX64_VERSION/")
WCP_FILE="$ROOT_DIR/$OUTPUT_FILE"
zip -j "$WCP_FILE" "$PKGDIR/box64" "$PKGDIR/meta.json"
echo -e "${green}Package created: $WCP_FILE${nocolor}"
ls -lh "$WCP_FILE"
