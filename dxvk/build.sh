#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

PACKAGE_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$PACKAGE_DIR")"
WORKDIR="$PACKAGE_DIR/workdir"
PKG_NAME="dxvk"

DXVK_REPO=$(yq ".${PKG_NAME}.repo" "$ROOT_DIR/packages.yml")
DXVK_BRANCH=$(yq ".${PKG_NAME}.branch" "$ROOT_DIR/packages.yml")
LLVM_MINGW_VER=$(yq ".${PKG_NAME}.mingw_ver" "$ROOT_DIR/packages.yml")
HOST_ARCH="$(uname -m)"
LLVM_MINGW_URL="https://github.com/mstorsjo/llvm-mingw/releases/download/${LLVM_MINGW_VER}/llvm-mingw-${LLVM_MINGW_VER}-ucrt-ubuntu-22.04-${HOST_ARCH}.tar.xz"

echo -e "${green}=== DXVK GPLAsync-LowLatency Builder (aarch64 + i686 PE) ===${nocolor}"

# --- Toolchain setup ---
TOOLCHAIN_DIR="$WORKDIR/toolchain"
TOOLCHAIN_BIN="$TOOLCHAIN_DIR/bin"
FEX_TOOLCHAIN="$ROOT_DIR/fexcore/workdir/toolchain/bin"

if [ -x "$FEX_TOOLCHAIN/aarch64-w64-mingw32-clang" ]; then
    TOOLCHAIN_BIN="$FEX_TOOLCHAIN"
    echo -e "${green}llvm-mingw found in fexcore cache${nocolor}"
elif command -v aarch64-w64-mingw32-clang &>/dev/null; then
    TOOLCHAIN_BIN="$(dirname "$(command -v aarch64-w64-mingw32-clang)")"
    echo -e "${green}llvm-mingw found in PATH${nocolor}"
elif [ -x "$TOOLCHAIN_BIN/aarch64-w64-mingw32-clang" ]; then
    echo -e "${green}llvm-mingw found in cache${nocolor}"
else
    echo "Downloading llvm-mingw ${LLVM_MINGW_VER}..."
    mkdir -p "$TOOLCHAIN_DIR"
    curl -sL "$LLVM_MINGW_URL" | xz -d | tar -C "$TOOLCHAIN_DIR" --strip-components=1 -xf -
    echo -e "${green}llvm-mingw cached${nocolor}"
fi

CC="$TOOLCHAIN_BIN/aarch64-w64-mingw32-clang"
CXX="$TOOLCHAIN_BIN/aarch64-w64-mingw32-clang++"
AR="$TOOLCHAIN_BIN/aarch64-w64-mingw32-llvm-ar"
STRIP="$TOOLCHAIN_BIN/aarch64-w64-mingw32-strip"
WINDRES="$TOOLCHAIN_BIN/aarch64-w64-mingw32-windres"

if [ ! -x "$CC" ]; then
    echo -e "${red}aarch64-w64-mingw32-clang not found${nocolor}"
    exit 1
fi

if [ ! -x "$AR" ]; then
    AR="$TOOLCHAIN_BIN/aarch64-w64-mingw32-ar"
fi

echo "CC  = $($CC --version 2>/dev/null | head -1 || echo aarch64-w64-mingw32-clang)"

# --- Clone DXVK ---
mkdir -p "$WORKDIR"
if [ ! -d "$WORKDIR/dxvk" ]; then
    echo "Cloning DXVK GPLAsync-LowLatency (branch: $DXVK_BRANCH)..."
    git clone --recurse-submodules --depth 1 --branch "$DXVK_BRANCH" "$DXVK_REPO" "$WORKDIR/dxvk" 2>&1
fi

DXVK_VERSION=$(cd "$WORKDIR/dxvk" && grep -oP "version\s*:\s*'\K[^']+" meson.build | head -1)
DXVK_VERSION_NAME="${DXVK_VERSION}-arm64ec-gplasync"
echo -e "${green}DXVK version: $DXVK_VERSION_NAME${nocolor}"

# --- Generate Meson cross-file ---
CROSSFILE="$WORKDIR/dxvk-cross-aarch64.txt"
cat > "$CROSSFILE" << CROSSEOF
[binaries]
c     = '$CC'
cpp   = '$CXX'
ar    = '$AR'
strip = '$STRIP'
windres = '$WINDRES'

[properties]
needs_exe_wrapper = true

[built-in options]
c_args = ['-mcpu=oryon-1', '-O3', '-ffast-math', '-funroll-loops', '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections']
cpp_args = ['-mcpu=oryon-1', '-O3', '-ffast-math', '-funroll-loops', '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections', '-std=c++17']
c_link_args = ['-fuse-ld=lld', '-Wl,--gc-sections']
cpp_link_args = ['-fuse-ld=lld', '-Wl,--gc-sections']

[host_machine]
system = 'windows'
cpu_family = 'aarch64'
cpu = 'aarch64'
endian = 'little'
CROSSEOF

echo "Cross-file: $CROSSFILE"

# --- Configure & Build ---
BUILD_DIR="$WORKDIR/build"
INSTALL_DIR="$WORKDIR/install"

if [ ! -f "$BUILD_DIR/build.ninja" ]; then
    echo "Configuring Meson (aarch64-w64-mingw32)..."
    meson setup "$BUILD_DIR" "$WORKDIR/dxvk" \
        --cross-file "$CROSSFILE" \
        --buildtype release \
        --strip \
        --prefix "$INSTALL_DIR" \
        --bindir "x64" \
        --libdir "x64" \
        -Db_ndebug=if-release \
        &> "$WORKDIR/meson_log"
fi

echo "Building..."
set -o pipefail
ninja -C "$BUILD_DIR" -j"$(nproc)" 2>&1 | tee "$WORKDIR/build_log"
BUILD_RC=${PIPESTATUS[0]}
set +o pipefail
if [ "$BUILD_RC" -ne 0 ]; then
    echo -e "${red}Build failed (exit code $BUILD_RC)${nocolor}"
    echo "Check $WORKDIR/build_log"
    exit 1
fi

ninja -C "$BUILD_DIR" install

# --- i686 PE build for 32-bit games (syswow64) ---
I686_CC="$TOOLCHAIN_BIN/i686-w64-mingw32-clang"
I686_CXX="$TOOLCHAIN_BIN/i686-w64-mingw32-clang++"
I686_AR="$TOOLCHAIN_BIN/i686-w64-mingw32-llvm-ar"
I686_STRIP="$TOOLCHAIN_BIN/i686-w64-mingw32-strip"
I686_WINDRES="$TOOLCHAIN_BIN/i686-w64-mingw32-windres"

if [ ! -x "$I686_CC" ]; then
    echo -e "${red}i686-w64-mingw32-clang not found in toolchain${nocolor}"
    exit 1
fi

I686_CROSSFILE="$WORKDIR/dxvk-cross-i686.txt"
cat > "$I686_CROSSFILE" << CROSSEOF
[binaries]
c     = '$I686_CC'
cpp   = '$I686_CXX'
ar    = '$I686_AR'
strip = '$I686_STRIP'
windres = '$I686_WINDRES'

[properties]
needs_exe_wrapper = true

[built-in options]
c_args = ['-march=i686', '-O3', '-ffast-math', '-funroll-loops', '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections']
cpp_args = ['-march=i686', '-O3', '-ffast-math', '-funroll-loops', '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections', '-std=c++17']
c_link_args = ['-fuse-ld=lld', '-Wl,--gc-sections']
cpp_link_args = ['-fuse-ld=lld', '-Wl,--gc-sections']

[host_machine]
system = 'windows'
cpu_family = 'x86'
cpu = 'i686'
endian = 'little'
CROSSEOF

I686_BUILD_DIR="$WORKDIR/build-i686"
I686_INSTALL_DIR="$WORKDIR/install-i686"

if [ ! -f "$I686_BUILD_DIR/build.ninja" ]; then
    echo "Configuring Meson (i686-w64-mingw32)..."
    meson setup "$I686_BUILD_DIR" "$WORKDIR/dxvk" \
        --cross-file "$I686_CROSSFILE" \
        --buildtype release \
        --strip \
        --prefix "$I686_INSTALL_DIR" \
        --bindir "x32" \
        --libdir "x32" \
        -Db_ndebug=if-release \
        &> "$WORKDIR/meson_log_i686"
fi

echo "Building i686..."
set -o pipefail
ninja -C "$I686_BUILD_DIR" -j"$(nproc)" 2>&1 | tee "$WORKDIR/build_log_i686"
BUILD_RC=${PIPESTATUS[0]}
set +o pipefail
if [ "$BUILD_RC" -ne 0 ]; then
    echo -e "${red}i686 build failed (exit code $BUILD_RC)${nocolor}"
    echo "Check $WORKDIR/build_log_i686"
    exit 1
fi

ninja -C "$I686_BUILD_DIR" install

# --- Package ---
echo "Packaging..."
PKGDIR="$WORKDIR/package"
mkdir -p "$PKGDIR/system32"
mkdir -p "$PKGDIR/syswow64"

cp -v "$INSTALL_DIR/x64/"*.dll "$PKGDIR/system32/"
cp -v "$I686_INSTALL_DIR/x32/"*.dll "$PKGDIR/syswow64/"

DLL_COUNT=$(find "$PKGDIR/system32" -name "*.dll" 2>/dev/null | wc -l)
I686_DLL_COUNT=$(find "$PKGDIR/syswow64" -name "*.dll" 2>/dev/null | wc -l)
if [ "$DLL_COUNT" -eq 0 ] || [ "$I686_DLL_COUNT" -eq 0 ]; then
    echo -e "${red}Build failed: missing DLLs (system32=$DLL_COUNT, syswow64=$I686_DLL_COUNT)${nocolor}"
    echo "Check $WORKDIR/build_log and $WORKDIR/build_log_i686"
    exit 1
fi
echo -e "${green}Found $DLL_COUNT aarch64 DLL(s), $I686_DLL_COUNT i686 DLL(s)${nocolor}"
ls -la "$PKGDIR/system32/"
ls -la "$PKGDIR/syswow64/"

# --- Config.json ---
cat > "$PKGDIR/Config.json" << 'CONFEOF'
{
  "DXVK_LOG_LEVEL": "none",
  "DXVK_ASYNC": "1",
  "DXVK_FRAME_PACE": "low-latency"
}
CONFEOF

# --- profile.json ---
DLL_FILES=""
COUNTER=0
TOTAL=$(($(find "$PKGDIR/system32" -name "*.dll" | wc -l) + $(find "$PKGDIR/syswow64" -name "*.dll" | wc -l)))
for dll in "$PKGDIR/system32"/*.dll; do
    [ -f "$dll" ] || continue
    COUNTER=$((COUNTER + 1))
    dll_name="$(basename "$dll")"
    COMMA=","
    [ "$COUNTER" -eq "$TOTAL" ] && COMMA=""
    DLL_FILES+=$'\n'"    {\"source\": \"system32/$dll_name\", \"target\": \"\${system32}/$dll_name\"}$COMMA"
done
for dll in "$PKGDIR/syswow64"/*.dll; do
    [ -f "$dll" ] || continue
    COUNTER=$((COUNTER + 1))
    dll_name="$(basename "$dll")"
    COMMA=","
    [ "$COUNTER" -eq "$TOTAL" ] && COMMA=""
    DLL_FILES+=$'\n'"    {\"source\": \"syswow64/$dll_name\", \"target\": \"\${syswow64}/$dll_name\"}$COMMA"
done

cat > "$PKGDIR/profile.json" << PROEOF
{
  "type": "DXVK",
  "versionName": "$DXVK_VERSION_NAME",
  "versionCode": 0,
  "description": "DXVK GPLAsync-LowLatency $DXVK_VERSION — Oryon optimized (aarch64 + i686 PE)",
  "files": [$DLL_FILES
  ]
}
PROEOF

OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$DXVK_VERSION/")
WCP_FILE="$ROOT_DIR/$OUTPUT_FILE"
cd "$PKGDIR" && tar -cf - system32/ syswow64/ profile.json Config.json | xz -9e > "$WCP_FILE"
echo -e "${green}Package created: $WCP_FILE${nocolor}"
ls -lh "$WCP_FILE"
