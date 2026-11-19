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

echo -e "${green}=== DXVK 3.x Builder (aarch64 + arm64ec + i686 PE) ===${nocolor}"

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
SOURCE_MARKER="$WORKDIR/dxvk.source"
CLEAN_TREE=0

if [ -d "$WORKDIR/dxvk/.git" ]; then
    current_origin=$(git -C "$WORKDIR/dxvk" remote get-url origin 2>/dev/null || echo "?")
    if [ "$current_origin" != "$DXVK_REPO" ]; then
        echo "workdir holds $current_origin, wanted $DXVK_REPO"
        CLEAN_TREE=1
    elif ! git -C "$WORKDIR/dxvk" rev-parse --verify --quiet "$DXVK_BRANCH^{commit}" > /dev/null; then
        echo "workdir has no ref $DXVK_BRANCH"
        CLEAN_TREE=1
    elif [ "$(git -C "$WORKDIR/dxvk" rev-parse HEAD)" != "$(git -C "$WORKDIR/dxvk" rev-parse "$DXVK_BRANCH^{commit}")" ]; then
        echo "workdir is not at $DXVK_BRANCH"
        CLEAN_TREE=1
    fi
fi

if [ -f "$SOURCE_MARKER" ] && [ "$(cat "$SOURCE_MARKER")" != "$DXVK_REPO $DXVK_BRANCH" ]; then
    echo "source changed ($(cat "$SOURCE_MARKER") -> $DXVK_REPO $DXVK_BRANCH)"
    CLEAN_TREE=1
fi

if [ "$CLEAN_TREE" = "1" ]; then
    echo "starting from a clean tree"
    rm -rf "$WORKDIR/dxvk" "$WORKDIR/build" "$WORKDIR/build-arm64ec" "$WORKDIR/build-i686" \
           "$WORKDIR/install" "$WORKDIR/install-arm64ec" "$WORKDIR/install-i686" "$SOURCE_MARKER"
fi

if [ ! -d "$WORKDIR/dxvk" ]; then
    echo "Cloning $DXVK_REPO (branch: $DXVK_BRANCH)..."
    git clone --depth 1 --branch "$DXVK_BRANCH" "$DXVK_REPO" "$WORKDIR/dxvk"
    printf '%s' "$DXVK_REPO $DXVK_BRANCH" > "$SOURCE_MARKER"
fi

# 3.x moved the shader compiler to subprojects/dxbc-spirv, which carries its own
# SPIRV-Headers submodule. A plain recursive clone does not reach that one.
echo "Updating submodules..."
git -C "$WORKDIR/dxvk" submodule update --init --depth 1
for sub in "$WORKDIR/dxvk"/subprojects/*/; do
    if [ -f "$sub/.gitmodules" ]; then
        git -C "$sub" submodule update --init --depth 1
    fi
done

DXVK_LOCAL_PATCHES=$(yq ".${PKG_NAME}.localPatches // \"\"" "$ROOT_DIR/packages.yml")
if [ -n "$DXVK_LOCAL_PATCHES" ] && [ "$DXVK_LOCAL_PATCHES" != "null" ]; then
    echo "Applying DXVK patches from $DXVK_LOCAL_PATCHES..."
    while read -r patch; do
        echo "  $(basename "$patch")..."
        if git -C "$WORKDIR/dxvk" apply --check "$patch" 2> /dev/null; then
            git -C "$WORKDIR/dxvk" apply "$patch"
        elif git -C "$WORKDIR/dxvk" apply --reverse --check "$patch" 2> /dev/null; then
            echo "    already applied"
        else
            echo -e "${red}  Patch $(basename "$patch") FAILED${nocolor}"
            exit 1
        fi
    done < <(find "$ROOT_DIR/$DXVK_LOCAL_PATCHES" -maxdepth 1 -name "*.patch" -type f | sort)
fi

DXVK_VERSION=$(cd "$WORKDIR/dxvk" && grep -oP "version\s*:\s*'\K[^']+" meson.build | head -1)
DXVK_VERSION_NAME="${DXVK_VERSION}-arm64ec"
echo -e "${green}DXVK version: $DXVK_VERSION_NAME${nocolor}"

# --- Generate Meson cross-file ---
A64_OPT="-mcpu=oryon-1 -O3 -funroll-loops"
A64_OPT_LIST=$(printf "'%s', " $A64_OPT)
A64_OPT_LIST="${A64_OPT_LIST%, }"
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
c_args = [$A64_OPT_LIST, '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections']
cpp_args = [$A64_OPT_LIST, '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections', '-std=c++17']
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

A64_FLAGS_STAMP="$BUILD_DIR/.dxvk_a64_opt"
if [ ! -f "$BUILD_DIR/build.ninja" ] || [ "$(cat "$A64_FLAGS_STAMP" 2>/dev/null)" != "$A64_OPT" ]; then
    echo "Configuring Meson (aarch64-w64-mingw32) with $A64_OPT..."
    rm -rf "$BUILD_DIR" "$INSTALL_DIR"
    meson setup "$BUILD_DIR" "$WORKDIR/dxvk" \
        --cross-file "$CROSSFILE" \
        --buildtype release \
        --strip \
        --prefix "$INSTALL_DIR" \
        --bindir "x64" \
        --libdir "x64" \
        -Db_ndebug=if-release \
        &> "$WORKDIR/meson_log"
    printf '%s' "$A64_OPT" > "$A64_FLAGS_STAMP"
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

# --- ARM64EC (x86_64 guest) build ---
# Wine's ARM64X system32 expects ARM64EC (or ARM64X) images for x64 games.
# Plain ARM64 is rejected by the PE loader with STATUS_INVALID_IMAGE_FORMAT.
SYSTEM32_ARCH="${DXVK_SYSTEM32_ARCH:-arm64ec}"
if [ "$SYSTEM32_ARCH" = "arm64ec" ]; then
    if [ -z "${ARM64EC_MINGW_ROOT:-}" ] || [ ! -x "$ARM64EC_MINGW_ROOT/bin/arm64ec-w64-mingw32-clang" ]; then
        echo "ARM64EC toolchain not in environment, fetching it..."
        "$ROOT_DIR/scripts/setup-arm64ec-toolchain.sh" > /dev/null
        export ARM64EC_MINGW_ROOT="$ROOT_DIR/toolchain/arm64ec"
        export PATH="$ARM64EC_MINGW_ROOT/bin:$PATH"
    fi

    EC_CROSSFILE="$WORKDIR/dxvk-cross-arm64ec.txt"
    cat > "$EC_CROSSFILE" << 'ECEOF'
[binaries]
c       = 'arm64ec-w64-mingw32-gcc'
cpp     = 'arm64ec-w64-mingw32-g++'
ar      = 'arm64ec-w64-mingw32-ar'
ld      = 'arm64ec-w64-mingw32-ld'
strip   = 'arm64ec-w64-mingw32-strip'
windres = 'arm64ec-w64-mingw32-windres'

[properties]
needs_exe_wrapper = true

[built-in options]
# LTO bitcode does not resolve the #-prefixed ARM64EC symbols from the static
# runtime, so keep it off even though 3.x no longer enables it by default.
c_args = ['-O2', '-fno-lto', '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections']
cpp_args = ['-O2', '-fno-lto', '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections', '-std=c++17']
c_link_args = ['-fuse-ld=lld', '-Wl,--gc-sections']
cpp_link_args = ['-fuse-ld=lld', '-Wl,--gc-sections']

[host_machine]
system = 'windows'
cpu_family = 'aarch64'
cpu = 'aarch64'
endian = 'little'
ECEOF

    EC_BUILD_DIR="$WORKDIR/build-arm64ec"
    EC_INSTALL_DIR="$WORKDIR/install-arm64ec"
    if [ ! -f "$EC_BUILD_DIR/build.ninja" ]; then
        echo "Configuring Meson (ARM64EC)..."
        meson setup "$EC_BUILD_DIR" "$WORKDIR/dxvk" \
            --cross-file "$EC_CROSSFILE" \
            --buildtype release \
            --strip \
            --prefix "$EC_INSTALL_DIR" \
            --bindir "x64" \
            --libdir "x64" \
            -Db_ndebug=if-release \
            &> "$WORKDIR/meson_log_arm64ec"
    fi
    echo "Building ARM64EC..."
    if ! ninja -C "$EC_BUILD_DIR" -j"$(nproc)" 2>&1 | tee "$WORKDIR/build_log_arm64ec"; then
        echo -e "${red}ARM64EC build failed${nocolor}"
        exit 1
    fi
    ninja -C "$EC_BUILD_DIR" install
    SYSTEM32_DIR="$EC_INSTALL_DIR/x64"
else
    SYSTEM32_DIR="$INSTALL_DIR/x64"
fi

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
# The 32-bit DLLs run under FEX, so the right optimisation target is the
# emulator, not the host CPU: fewer guest instructions and fewer basic blocks
# mean less to translate and a smaller code cache.  Override to A/B on device:
#   DXVK_I686_OPT="-O3 -funroll-loops" ./dxvk/build.sh
I686_OPT="${DXVK_I686_OPT:--O2 -fno-unroll-loops}"
I686_OPT_LIST=$(printf "'%s', " $I686_OPT)
I686_OPT_LIST="${I686_OPT_LIST%, }"
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
c_args = ['-march=i686', $I686_OPT_LIST, '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections']
cpp_args = ['-march=i686', $I686_OPT_LIST, '-fomit-frame-pointer', '-ffunction-sections', '-fdata-sections', '-std=c++17']
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

I686_FLAGS_STAMP="$I686_BUILD_DIR/.dxvk_i686_opt"
if [ ! -f "$I686_BUILD_DIR/build.ninja" ] || [ "$(cat "$I686_FLAGS_STAMP" 2>/dev/null)" != "$I686_OPT" ]; then
    echo "Configuring Meson (i686-w64-mingw32) with $I686_OPT..."
    rm -rf "$I686_BUILD_DIR" "$I686_INSTALL_DIR"
    meson setup "$I686_BUILD_DIR" "$WORKDIR/dxvk" \
        --cross-file "$I686_CROSSFILE" \
        --buildtype release \
        --strip \
        --prefix "$I686_INSTALL_DIR" \
        --bindir "x32" \
        --libdir "x32" \
        -Db_ndebug=if-release \
        &> "$WORKDIR/meson_log_i686"
    printf '%s' "$I686_OPT" > "$I686_FLAGS_STAMP"
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

cp -v "$SYSTEM32_DIR/"*.dll "$PKGDIR/system32/"
cp -v "$I686_INSTALL_DIR/x32/"*.dll "$PKGDIR/syswow64/"

READOBJ=$(command -v llvm-readobj 2> /dev/null || echo "$TOOLCHAIN_BIN/llvm-readobj")
if [ -x "$READOBJ" ]; then
    SYS32_MACHINE=$("$READOBJ" --file-headers "$PKGDIR/system32/d3d11.dll" 2> /dev/null | grep -m1 "Machine:")
    case "$SYS32_MACHINE" in
        *ARM64EC*|*ARM64X*) ;;
        *)
            echo -e "${red}system32/d3d11.dll is $SYS32_MACHINE, expected ARM64EC or ARM64X${nocolor}"
            exit 1
            ;;
    esac
    echo -e "${green}system32 machine check: $SYS32_MACHINE${nocolor}"
fi

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
# 3.x has no DXVK_ASYNC / DXVK_FRAME_PACE; shader compilation always runs on
# worker threads and the frame limiter is dxvk.maxFrameRate in dxvk.conf.
cat > "$PKGDIR/Config.json" << 'CONFEOF'
{
  "DXVK_LOG_LEVEL": "none"
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
  "versionCode": 1,
  "description": "DXVK $DXVK_VERSION upstream (dxbc-spirv) — arm64ec + i686 PE. Needs Turnip 26.3+ (Vulkan 1.4).",
  "files": [$DLL_FILES
  ]
}
PROEOF

OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$DXVK_VERSION/")
WCP_FILE="$ROOT_DIR/$OUTPUT_FILE"
cd "$PKGDIR" && tar -cf - system32/ syswow64/ profile.json Config.json | xz -9e > "$WCP_FILE"
echo -e "${green}Package created: $WCP_FILE${nocolor}"
ls -lh "$WCP_FILE"
