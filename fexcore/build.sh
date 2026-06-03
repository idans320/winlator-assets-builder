#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

PACKAGE_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$PACKAGE_DIR")"
WORKDIR="$PACKAGE_DIR/workdir"
PKG_NAME="fexcore"

FEX_REPO=$(yq ".${PKG_NAME}.repo" "$ROOT_DIR/packages.yml")
FEX_BRANCH=$(yq ".${PKG_NAME}.branch" "$ROOT_DIR/packages.yml")
LLVM_MINGW_VER=$(yq ".${PKG_NAME}.mingw_ver" "$ROOT_DIR/packages.yml")
HOST_ARCH="$(uname -m)"
LLVM_MINGW_URL="https://github.com/mstorsjo/llvm-mingw/releases/download/${LLVM_MINGW_VER}/llvm-mingw-${LLVM_MINGW_VER}-ucrt-ubuntu-22.04-${HOST_ARCH}.tar.xz"

echo -e "${green}=== FEXCore Builder (llvm-mingw aarch64) ===${nocolor}"

# --- Toolchain setup ---
TOOLCHAIN_DIR="$WORKDIR/toolchain"
TOOLCHAIN_BIN="$TOOLCHAIN_DIR/bin"

if command -v aarch64-w64-mingw32-clang &>/dev/null; then
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
RANLIB="$TOOLCHAIN_BIN/aarch64-w64-mingw32-llvm-ranlib"

if [ ! -x "$CC" ]; then
    echo -e "${red}aarch64-w64-mingw32-clang not found${nocolor}"
    exit 1
fi

echo "CC  = $($CC --version | head -1)"

# --- Clone FEX ---
mkdir -p "$WORKDIR"
if [ ! -d "$WORKDIR/fex" ]; then
    echo "Cloning FEX (branch: $FEX_BRANCH)..."
    git clone --recurse-submodules --depth 1 --branch "$FEX_BRANCH" "$FEX_REPO" "$WORKDIR/fex" 2>&1
fi

FEX_VERSION="$(cd "$WORKDIR/fex" && git rev-parse --short HEAD)-experimental"
FEX_COMMIT_DATE=$(cd "$WORKDIR/fex" && git log -1 --format=%ct 2>/dev/null || date +%s)
echo -e "${green}FEX version: $FEX_VERSION${nocolor}"

# Apply local patches first
# --- Apply local patches ---
FEX_LOCAL_PATCHES=$(yq ".${PKG_NAME}.localPatches // \"\"" "$ROOT_DIR/packages.yml")
if [ -n "$FEX_LOCAL_PATCHES" ] && [ "$FEX_LOCAL_PATCHES" != "null" ]; then
    echo "Applying FEXCore patches from $FEX_LOCAL_PATCHES..."
    find "$ROOT_DIR/$FEX_LOCAL_PATCHES" -maxdepth 1 -name "*.patch" -type f 2>/dev/null | sort | while read -r patch; do
        echo "  Applying $(basename "$patch")..."
        git -C "$WORKDIR/fex" am "$patch" 2>&1 || {
            echo -e "${red}  Patch $(basename "$patch") FAILED — check $WORKDIR/fex git status${nocolor}"
            exit 1
        }
    done
fi

# FEX's WOW64 DLL links with -nostdlib/-nodefaultlibs which omits sincos.
# Provide a standalone stub to avoid pulling in all of mingwex (symbol conflicts).
SINCOS_STUB="$WORKDIR/sincos_stub.c"
SINCOS_OBJ="$WORKDIR/sincos_stub.o"
if [ ! -f "$SINCOS_OBJ" ]; then
    echo '#include <math.h>
void sincos(double x, double *s, double *c) { *s = sin(x); *c = cos(x); }' > "$SINCOS_STUB"
    "$CC" -c "$SINCOS_STUB" -o "$SINCOS_OBJ" || {
        echo -e "${red}Failed to compile sincos stub${nocolor}"
        exit 1
    }
fi

# Fix dlltool: generate aarch64 import libs, not x86_64
if ! grep -q -- '-m arm64' "$WORKDIR/fex/Source/Windows/CMakeLists.txt" 2>/dev/null; then
    sed -i 's|${CMAKE_DLLTOOL} -d|${CMAKE_DLLTOOL} -m arm64 -d|' \
        "$WORKDIR/fex/Source/Windows/CMakeLists.txt"
fi

# Use bundled fmt (system fmt causes IMPORTED_IMPLIB errors on cross-compile)
if grep -q 'find_package(fmt QUIET)' "$WORKDIR/fex/CMakeLists.txt" 2>/dev/null; then
    sed -i '/^find_package(fmt QUIET)/,/^endif()/c\# Use bundled fmt for cross-compile\nset(FMT_INSTALL OFF)\nadd_subdirectory(External/fmt/)' \
        "$WORKDIR/fex/CMakeLists.txt"
fi

# --- Configure & Build ---
BUILD_DIR="$WORKDIR/fex/build-mingw"
mkdir -p "$BUILD_DIR"

ORYON_FLAGS="-mcpu=oryon-1 -O3 -ffast-math -funroll-loops -fomit-frame-pointer"

if [ ! -f "$BUILD_DIR/CMakeCache.txt" ]; then
    echo "Configuring CMake (aarch64-w64-mingw32)..."

    # FEX WOW64 DLL links with -nostdlib which omits sincos.
    # Inject our stub object into the WOW64 target link command.
    if ! grep -q -- 'sincos_stub.o' "$WORKDIR/fex/Source/Windows/WOW64/CMakeLists.txt" 2>/dev/null; then
        sed -i "s|target_link_options(wow64fex PRIVATE -static|target_link_options(wow64fex PRIVATE ${SINCOS_OBJ} -static|" \
            "$WORKDIR/fex/Source/Windows/WOW64/CMakeLists.txt"
    fi

    cmake -B "$BUILD_DIR" -S "$WORKDIR/fex" -G Ninja \
        -DCMAKE_SYSTEM_NAME="Windows" \
        -DCMAKE_SYSTEM_PROCESSOR="aarch64" \
        -DCMAKE_CROSSCOMPILING=ON \
        -DCMAKE_C_COMPILER="$CC" \
        -DCMAKE_CXX_COMPILER="$CXX" \
        -DCMAKE_AR="$AR" \
        -DCMAKE_STRIP="$STRIP" \
        -DCMAKE_RANLIB="$RANLIB" \
        -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_C_FLAGS="$ORYON_FLAGS" \
        -DCMAKE_CXX_FLAGS="$ORYON_FLAGS" \
        -DENABLE_LTO=OFF \
         -DUSE_LINKER=lld \
          -DBUILD_TESTING=OFF \
          -DBUILD_FEXCONFIG=OFF \
          -DTUNE_CPU=oryon-1 \
          -DBUILD_THUNKS=OFF \
         -DENABLE_ASSERTIONS=OFF \
         -DENABLE_JEMALLOC_GLIBC_ALLOC=ON \
        &> "$WORKDIR/cmake_log"
fi

echo "Building..."
set +e
set -o pipefail
cmake --build "$BUILD_DIR" -j"$(nproc)" 2>&1 | tee "$WORKDIR/build_log"
BUILD_RC=${PIPESTATUS[0]}
set -o pipefail
set -e
if [ "$BUILD_RC" -ne 0 ]; then
    echo -e "${red}Build failed (exit code $BUILD_RC)${nocolor}"
fi

# --- Package ---
echo "Packaging..."
PKGDIR="$WORKDIR/package"
mkdir -p "$PKGDIR/system32"

find "$BUILD_DIR" -name "*.dll" -exec cp -v {} "$PKGDIR/system32/" \; 2>&1

DLL_COUNT=$(find "$PKGDIR/system32" -name "*.dll" 2>/dev/null | wc -l)
if [ "$DLL_COUNT" -eq 0 ]; then
    echo -e "${red}Build failed: no DLLs found in build output${nocolor}"
    echo "Check $WORKDIR/build_log"
    exit 1
fi
echo -e "${green}Found $DLL_COUNT DLL(s)${nocolor}"

# Performance hacks config — enables FEXCore speed hacks at runtime
cat > "$PKGDIR/Config.json" << 'CONFEOF'
{
  "Hacks": {
    "SMCChecks": "none",
    "TSOEnabled": false,
    "X87ReducedPrecision": true,
    "HideHypervisorBit": true
  }
}
CONFEOF

# CMOD manifest
DLL_FILES=""
COUNTER=0
TOTAL=$(find "$PKGDIR/system32" -name "*.dll" | wc -l)
for dll in "$PKGDIR/system32"/*.dll; do
    [ -f "$dll" ] || continue
    COUNTER=$((COUNTER + 1))
    dll_name="$(basename "$dll")"
    COMMA=","
    [ "$COUNTER" -eq "$TOTAL" ] && COMMA=""
    DLL_FILES+=$'\n'"    {\"source\": \"system32/$dll_name\", \"target\": \"\${system32}/$dll_name\"}$COMMA"
done

cat > "$PKGDIR/profile.json" << PROEOF
{
  "type": "FEXCore",
  "versionName": "FEXCore",
  "versionCode": $FEX_COMMIT_DATE,
  "description": "FEXCore $FEX_VERSION — Oryon optimized (llvm-mingw aarch64)",
  "files": [$DLL_FILES
  ]
}
PROEOF

OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$FEX_VERSION/")
WCP_FILE="$ROOT_DIR/$OUTPUT_FILE"
cd "$PKGDIR" && tar -cf - system32/ profile.json Config.json | xz -9e > "$WCP_FILE"
echo -e "${green}Package created: $WCP_FILE${nocolor}"
ls -lh "$WCP_FILE"
