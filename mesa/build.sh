#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

PACKAGE_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$PACKAGE_DIR")"
WORKDIR="$PACKAGE_DIR/workdir"
PKG_NAME="mesa"

MESA_REPO=$(yq ".${PKG_NAME}.repo" "$ROOT_DIR/packages.yml")
MESA_BRANCH=$(yq ".${PKG_NAME}.branch" "$ROOT_DIR/packages.yml")
SDK_VER=$(yq ".${PKG_NAME}.sdk_ver" "$ROOT_DIR/packages.yml")
NDK_CLANG="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin"
MESA_BUILD_TYPE="${MESA_BUILD_TYPE:-$(yq ".${PKG_NAME}.buildtype" "$ROOT_DIR/packages.yml")}"
MESA_LOCAL_PATCHES=$(yq ".${PKG_NAME}.localPatches // \"\"" "$ROOT_DIR/packages.yml")
BUILD_DIR="build-android-${MESA_BUILD_TYPE}"
DRIVER_SO="$WORKDIR/mesa/${BUILD_DIR}/src/freedreno/vulkan/libvulkan_freedreno.so"

echo -e "${green}=== Mesa Turnip Builder ===${nocolor}"

if [ -z "$NDK" ] || [ -z "$ANDROID_HOME" ]; then
    echo -e "${red}Not inside Nix dev shell. Run: nix develop /app/mesa-builder#mesa${nocolor}"
    exit 1
fi

if [ ! -d "$NDK_CLANG" ]; then
    echo -e "${red}NDK toolchain not found at $NDK_CLANG${nocolor}"
    exit 1
fi

mkdir -p "$WORKDIR"
if [ ! -d "$WORKDIR/mesa" ]; then
    echo "Cloning Mesa (branch: $MESA_BRANCH)..."
    git clone --depth 1 --branch "$MESA_BRANCH" "$MESA_REPO" "$WORKDIR/mesa" 2>&1
fi
MESA_VERSION=$(cat "$WORKDIR/mesa/VERSION")
echo -e "${green}Mesa version: $MESA_VERSION${nocolor}"
echo -e "${green}Build type: $MESA_BUILD_TYPE${nocolor}"

if [ -n "$MESA_LOCAL_PATCHES" ] && [ "$MESA_LOCAL_PATCHES" != "null" ]; then
    echo "Applying local patches from $MESA_LOCAL_PATCHES ..."
    find "$ROOT_DIR/$MESA_LOCAL_PATCHES" -maxdepth 1 -name "*.patch" -type f 2>/dev/null | sort | while read -r patch; do
        echo "  Applying $(basename "$patch")..."
        git -C "$WORKDIR/mesa" am "$patch" 2>&1 || {
            echo "  Patch $(basename "$patch") failed — check $WORKDIR/mesa git status"
        }
    done
fi

if [ ! -f "$WORKDIR/mesa/${BUILD_DIR}/build.ninja" ]; then
    echo "Creating cross-file..."
    NDK_SYSROOT="$NDK/toolchains/llvm/prebuilt/linux-x86_64/sysroot"
    mkdir -p "$WORKDIR/pkgconfig"
    cat > "$WORKDIR/pkgconfig/zlib.pc" << ZLIBEOF
Name: zlib
Description: zlib compression library
Version: 1.3
Libs: -L${NDK_SYSROOT}/usr/lib/aarch64-linux-android -lz
Cflags: -I${NDK_SYSROOT}/usr/include
ZLIBEOF

    cat > "$WORKDIR/android-aarch64.txt" << CROSSEOF
[binaries]
c = ['$NDK_CLANG/aarch64-linux-android${SDK_VER}-clang', '-Wno-deprecated-declarations', '-Wno-gnu-alignof-expression']
cpp = ['$NDK_CLANG/aarch64-linux-android${SDK_VER}-clang++', '-fno-exceptions', '-fno-unwind-tables', '-fno-asynchronous-unwind-tables', '-static-libstdc++', '-Wno-deprecated-declarations', '-Wno-gnu-alignof-expression', '-Wno-c++11-narrowing']
ar = '$NDK_CLANG/llvm-ar'
strip = '$NDK_CLANG/llvm-strip'
c_ld = '$NDK_CLANG/ld.lld'
cpp_ld = '$NDK_CLANG/ld.lld'
pkg-config = 'pkg-config'

[properties]
sys_root = '${NDK_SYSROOT}'
pkg_config_libdir = '${WORKDIR}/pkgconfig'

[host_machine]
system = 'android'
cpu_family = 'aarch64'
cpu = 'armv8'
endian = 'little'
CROSSEOF

    echo "Creating native-file..."
    cat > "$WORKDIR/native.txt" << NATIVEEOF
[binaries]
c = 'cc'
cpp = 'c++'
ar = 'ar'
strip = 'strip'
pkg-config = 'pkg-config'
NATIVEEOF

    echo "Configuring build (type: $MESA_BUILD_TYPE)..."
    cd "$WORKDIR/mesa"
    meson setup "$BUILD_DIR" \
        --native-file "$WORKDIR/native.txt" \
        --cross-file "$WORKDIR/android-aarch64.txt" \
        -Dbuildtype="$MESA_BUILD_TYPE" \
        -Dplatforms=android \
        -Dplatform-sdk-version="$SDK_VER" \
        -Dandroid-stub=true \
        -Dgallium-drivers= \
        -Dvulkan-drivers=freedreno \
        -Dfreedreno-kmds=kgsl \
        -Degl=disabled \
        -Dspirv-tools=disabled \
        -Dzstd=disabled \
        -Dstrip=false &> "$WORKDIR/meson_log"
fi

echo "Building..."
cd "$WORKDIR/mesa"
if [ -f "$DRIVER_SO" ]; then
    echo -e "${green}Driver already built, skipping...${nocolor}"
else
    ninja -C "$BUILD_DIR" 2>&1 | tee "$WORKDIR/ninja_log"
fi

if [ ! -f "$DRIVER_SO" ]; then
    echo -e "${red}Build failed: libvulkan_freedreno.so not found${nocolor}"
    exit 1
fi
echo -e "${green}Build successful${nocolor}"

echo "Fixing SONAME with patchelf..."
patchelf --set-soname vulkan.turnip.so "$DRIVER_SO"

ICD_JSON=$(ls "$WORKDIR/mesa/${BUILD_DIR}/src/freedreno/vulkan/freedreno_icd."*.json 2>/dev/null | head -1)
if [ -z "$ICD_JSON" ]; then
    VK_API_VERSION=$(strings "$DRIVER_SO" | grep -oP '1\.\d+\.\d+' | sort -u | tail -1)
else
    VK_API_VERSION=$(python3 -c "import json; print(json.load(open('$ICD_JSON'))['ICD']['api_version'])")
fi
echo -e "${green}Vulkan API version: $VK_API_VERSION${nocolor}"

echo "Packaging..."
PKGDIR="$WORKDIR/package"
mkdir -p "$PKGDIR"
cp "$DRIVER_SO" "$PKGDIR/vulkan.turnip.so"

if [ "$MESA_BUILD_TYPE" != "release" ]; then
    MESA_VERSION="${MESA_VERSION}-${MESA_BUILD_TYPE}"
fi
MESA_VERSION="${MESA_VERSION}-experimental"

cat > "$PKGDIR/meta.json" << METAEOF
{
  "schemaVersion": 1,
  "name": "Mesa Turnip $MESA_VERSION",
  "description": "Freedreno Turnip Vulkan driver — SIMD heresies + bandwidth lite, Oryon optimized, Mesa $MESA_VERSION, Vulkan $VK_API_VERSION (build: $MESA_BUILD_TYPE)",
  "author": "Mesa",
  "packageVersion": "$MESA_VERSION",
  "vendor": "Mesa",
  "driverVersion": "Vulkan $VK_API_VERSION",
  "minApi": 27,
  "libraryName": "vulkan.turnip.so"
}
METAEOF

cat > "$PKGDIR/Config.json" << CONFEOF
{
  "env": {
    "MESA_DEBUG": "${MESA_DEBUG:-}",
    "TU_DEBUG": "${TU_DEBUG:-}",
    "FD_DEBUG": "${FD_DEBUG:-}",
    "VK_LOADER_DEBUG": "${VK_LOADER_DEBUG:-}"
  }
}
CONFEOF

OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$MESA_VERSION/")
WCP_FILE="$ROOT_DIR/$OUTPUT_FILE"
zip -j "$WCP_FILE" "$PKGDIR/vulkan.turnip.so" "$PKGDIR/meta.json" "$PKGDIR/Config.json"
echo -e "${green}Package created: $WCP_FILE${nocolor}"
ls -lh "$WCP_FILE"
