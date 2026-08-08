#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
yellow='\033[0;33m'
nocolor='\033[0m'

info() { echo -e "${green}$1${nocolor}"; }
warn() { echo -e "${yellow}$1${nocolor}"; }
die() {
    echo -e "${red}$1${nocolor}"
    exit 1
}

# ── env ──────────────────────────────────────────────────────
setup_env() {
    PACKAGE_DIR="$(cd "$(dirname "$0")" && pwd)"
    ROOT_DIR="$(dirname "$PACKAGE_DIR")"

    export WORKDIR="$PACKAGE_DIR/workdir"
    PKG_NAME="wine"

    WINE_REPO=$(yq ".${PKG_NAME}.repo" "$ROOT_DIR/packages.yml")
    WINE_BRANCH=$(yq ".${PKG_NAME}.branch" "$ROOT_DIR/packages.yml")
    LLVM_MINGW_VER=$(yq ".${PKG_NAME}.mingw_ver" "$ROOT_DIR/packages.yml")
    SDK_VER=$(yq ".${PKG_NAME}.sdk_ver" "$ROOT_DIR/packages.yml")
    PRESET=$(yq ".${PKG_NAME}.preset" "$ROOT_DIR/packages.yml")

    REMOTE_PATCH_DIR=""
    PKG_REMOTE_PATCHES=$(yq ".${PKG_NAME}.remotePatches" "$ROOT_DIR/packages.yml")
    if [ -n "$PKG_REMOTE_PATCHES" ] && [ "$PKG_REMOTE_PATCHES" != "null" ]; then
        REMOTE_PATCH_DIR="$WORKDIR/wine/$PKG_REMOTE_PATCHES"
    fi

    LOCAL_PATCH_DIR=""
    PKG_LOCAL_PATCHES=$(yq ".${PKG_NAME}.localPatches" "$ROOT_DIR/packages.yml")
    if [ -n "$PKG_LOCAL_PATCHES" ] && [ "$PKG_LOCAL_PATCHES" != "null" ]; then
        LOCAL_PATCH_DIR="$ROOT_DIR/$PKG_LOCAL_PATCHES"
    fi

    CPU_FLAGS=""
    CXX_EXTRA=""
    LD_EXTRA=""
    if [ -n "$PRESET" ] && [ "$PRESET" != "null" ]; then
        PRESET_FILE="$ROOT_DIR/presets/${PRESET}.yml"
        if [ -f "$PRESET_FILE" ]; then
            CPU_FLAGS=$(yq ".cflags" "$PRESET_FILE")
            CXX_EXTRA=$(yq ".cxxflags" "$PRESET_FILE")
            LD_EXTRA=$(yq ".ldflags" "$PRESET_FILE")
            info "Preset: $PRESET"
        else
            warn "Preset file not found: $PRESET_FILE"
        fi
    fi

    HOST_ARCH="$(uname -m)"
    IS_AARCH64=$([ "$HOST_ARCH" = "aarch64" ] && echo 1 || echo 0)
    export IS_AARCH64
    export WIN_ARCH="arm64ec,aarch64,i386"
    export OUTPUT_DIR="$ROOT_DIR/compiled-files-aarch64"
    export RUNTIME_PATH="/data/data/com.termux/files/usr"
    export install_dir="$WORKDIR/wine-install"

    info "=== Wine 11 ARM64 Proton Builder ==="
}

# ── toolchain ────────────────────────────────────────────────
setup_toolchain() {
    LLVM_MINGW_URL="https://github.com/mstorsjo/llvm-mingw/releases/download/${LLVM_MINGW_VER}/llvm-mingw-${LLVM_MINGW_VER}-ucrt-ubuntu-22.04-${HOST_ARCH}.tar.xz"
    MINGW_DIR="$WORKDIR/toolchain"

    if [ -x "$ROOT_DIR/fexcore/workdir/toolchain/bin/aarch64-w64-mingw32-clang" ]; then
        MINGW_DIR="$ROOT_DIR/fexcore/workdir/toolchain"
    elif command -v aarch64-w64-mingw32-clang &> /dev/null; then
        MINGW_DIR="$(dirname "$(dirname "$(command -v aarch64-w64-mingw32-clang)")")"
    fi

    if [ ! -x "$MINGW_DIR/bin/aarch64-w64-mingw32-clang" ]; then
        echo "Downloading llvm-mingw ${LLVM_MINGW_VER}..."
        mkdir -p "$MINGW_DIR"
        curl -sL "$LLVM_MINGW_URL" | xz -d | tar -C "$MINGW_DIR" --strip-components=1 -xf -
        info "llvm-mingw cached"
    fi

    export PATH="$MINGW_DIR/bin:$PATH"
    [ ! -x "$MINGW_DIR/bin/dlltool" ] && ln -sf llvm-dlltool "$MINGW_DIR/bin/dlltool" 2> /dev/null || true
    export DLLTOOL="$MINGW_DIR/bin/llvm-dlltool"

    NDK_CLANG="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin"
    # Use NDK r27 if available (avoids r29 segfault on Snapdragon 8 Elite)
    if [ -d "/tmp/android-ndk-r27" ]; then
        NDK_CLANG="/tmp/android-ndk-r27/toolchains/llvm/prebuilt/linux-x86_64/bin"
        info "Using NDK r27 at $NDK_CLANG"
    fi
    export NDK_BIN="$NDK_CLANG"
    export TARGET="aarch64-linux-android${SDK_VER}"
    export SYSROOT="$NDK_CLANG/../sysroot"

    export CC="${NDK_CLANG}/${TARGET}-clang"
    export AS="$CC"
    export CXX="${NDK_CLANG}/${TARGET}-clang++"
    export AR="${NDK_CLANG}/llvm-ar"
    export LD="${NDK_CLANG}/ld"
    export RANLIB="${NDK_CLANG}/llvm-ranlib"
    export STRIP="${NDK_CLANG}/llvm-strip"
}

apply_16kb_pages() {
    [ "$ENABLE_16KB" -ne 1 ] && return
    warn "Enabling 16KB page size support..."
    export TARGET="aarch64-linux-android35"
    CPU_FLAGS="$CPU_FLAGS -DANDROID_SUPPORT_FLEXIBLE_PAGE_SIZES"
    LD_EXTRA="$LD_EXTRA -Wl,-z,max-page-size=16384"
    export CC="${NDK_BIN}/aarch64-linux-android28-clang"
    export CXX="${NDK_BIN}/aarch64-linux-android28-clang++"
    info "16KB page size support enabled"
}

# ── compiler flags ───────────────────────────────────────────
setup_compiler_flags() {
    export DEPS="${DEPS:-}"
    if [ -z "$DEPS" ] && [ -f "$WORKDIR/termux-rootfs/data/data/com.termux/files/usr/.deps-ready" ]; then
        export DEPS="$WORKDIR/termux-rootfs/data/data/com.termux/files/usr"
        info "Using cached Termux deps: $DEPS"
    fi

    C_OPTS="-Wno-declaration-after-statement -Wno-implicit-function-declaration -Wno-int-conversion"
    if [ "$NO_OPT" -eq 1 ]; then
        C_OPTS="$C_OPTS -O0"
        warn "Optimizations DISABLED (--no-opt)"
    else
        C_OPTS="$C_OPTS $CPU_FLAGS"
    fi
    export CFLAGS="$C_OPTS"
    if [ "$NO_OPT" -eq 1 ]; then
        export CXXFLAGS="$C_OPTS"
    else
        export CXXFLAGS="${CXX_EXTRA:-$C_OPTS}"
    fi
    export CPPFLAGS="${DEPS:+-I$DEPS/include }--sysroot=${SYSROOT}"
    export LDFLAGS="${DEPS:+-L$DEPS/lib }${LD_EXTRA} -Wl,-rpath=${RUNTIME_PATH}/lib -Wl,-rpath=\\\$\$ORIGIN/../../lib"
    export PKG_CONFIG_LIBDIR="${DEPS:+$DEPS/lib/pkgconfig:$DEPS/share/pkgconfig}"

    echo "HOST CC = $($CC --version 2> /dev/null | head -1 || echo "$CC")"
    echo "PE CC   = $(aarch64-w64-mingw32-clang --version 2> /dev/null | head -1 || echo aarch64-w64-mingw32-clang)"
    if [ -n "$DEPS" ]; then echo "DEPS    = $DEPS"; else warn "DEPS not set (no sysroot for --with-* features)"; fi

    if [ -n "$DEPS" ]; then
        export FREETYPE_CFLAGS="-I$DEPS/include/freetype2"
        export PULSE_CFLAGS="-I$DEPS/include/pulse"
        export PULSE_LIBS="-L$DEPS/lib/pulseaudio -lpulse"
        export SDL2_CFLAGS="-I$DEPS/include/SDL2"
        export SDL2_LIBS="-L$DEPS/lib -lSDL2"
        export FONTCONFIG_LIBS="-L$DEPS/lib -lfontconfig -lfreetype -lexpat"
        export X_CFLAGS="-I$DEPS/include"
        export X_LIBS="-lX11 -lXext -landroid-sysvshm"
        export GSTREAMER_CFLAGS="-I$DEPS/include/gstreamer-1.0 -I$DEPS/include/glib-2.0 -I$DEPS/lib/glib-2.0/include -I$DEPS/glib-2.0/include -I$DEPS/lib/gstreamer-1.0/include"
        export GSTREAMER_LIBS="-L$DEPS/lib -lgstgl-1.0 -lgstapp-1.0 -lgstvideo-1.0 -lgstaudio-1.0 -lglib-2.0 -lgobject-2.0 -lgio-2.0 -lgsttag-1.0 -lgstbase-1.0 -lgstreamer-1.0"
    else
        setup_nix_x11_compat
    fi
}

setup_nix_x11_compat() {
    NIX_X11_CFLAGS="$(
        pkg-config --cflags-only-I x11 2> /dev/null
        pkg-config --cflags-only-I xext 2> /dev/null | tr '\n' ' '
    )"
    XORGPROTO_DIR="$(pkg-config --variable=includedir xorgproto 2> /dev/null || pkg-config --variable=includedir xproto 2> /dev/null || echo "")"
    if [ -z "$XORGPROTO_DIR" ]; then
        warn "X11 headers not found in nix store"
        return
    fi
    X11_COMPAT="$WORKDIR/x11-compat/X11/extensions"
    mkdir -p "$X11_COMPAT"
    for h in shm XI XI2 render randr; do
        case $h in
            shm) src="shm.h" dst="XShm.h" ;;
            XI) src="XI.h" dst="XInput.h" ;;
            XI2) src="XI2.h" dst="XInput2.h" ;;
            render) src="render.h" dst="Xrender.h" ;;
            randr) src="randr.h" dst="Xrandr.h" ;;
        esac
        ln -sf "$XORGPROTO_DIR/X11/extensions/$src" "$X11_COMPAT/$dst" 2> /dev/null || true
    done
    ln -sf "$XORGPROTO_DIR/X11/extensions/shapeproto.h" "$X11_COMPAT/shape.h" 2> /dev/null || true
    for stub in Xcursor Xfixes Xinerama; do
        {
            echo "#ifndef _${stub^^}_H_"
            echo "#define _${stub^^}_H_"
            echo '#include <X11/Xlib.h>'
            echo "#endif"
        } > "$X11_COMPAT/${stub}.h"
    done
    NIX_X11_CFLAGS="$NIX_X11_CFLAGS -I$WORKDIR/x11-compat"
    export X_CFLAGS="$NIX_X11_CFLAGS"
    export X_LIBS=""
    export CPPFLAGS="$CPPFLAGS $NIX_X11_CFLAGS"
    export ac_cv_have_x="have_x=yes"
    info "X11 headers resolved via nix (pkg-config)"
}

# ── termux deps ──────────────────────────────────────────────
setup_deps() {
    [ "$DO_SETUP_DEPS" -ne 1 ] && return

    TERMUX_ROOT="$WORKDIR/termux-rootfs"
    TERMUX_PREFIX="$TERMUX_ROOT/data/data/com.termux/files/usr"
    TERMUX_REPO="https://packages.termux.dev/apt/termux-main"
    TERMUX_ARCH="aarch64"
    DEB_CACHE="$WORKDIR/deb-cache"

    if [ -f "$TERMUX_PREFIX/.deps-ready" ]; then
        info "Termux deps already installed at $TERMUX_PREFIX"
        export DEPS="$TERMUX_PREFIX"
        return
    fi

    echo "Bootstrapping Termux aarch64 deps into $TERMUX_PREFIX..."
    mkdir -p "$DEB_CACHE" "$TERMUX_PREFIX"

    PKGS_INDEX="$DEB_CACHE/Packages"
    [ ! -f "$PKGS_INDEX" ] && curl -sL "$TERMUX_REPO/dists/stable/main/binary-$TERMUX_ARCH/Packages" -o "$PKGS_INDEX"

    download_pkg() {
        local pkg="$1"
        local deb="$DEB_CACHE/${pkg}.deb"
        if [ -f "$deb" ]; then
            echo "  $pkg (cached)"
            return
        fi
        local url
        url=$(grep -A20 "^Package: $pkg\$" "$PKGS_INDEX" | grep "^Filename:" | head -1 | awk '{print $2}')
        if [ -n "$url" ]; then
            echo "  $pkg ..."
            curl -sL "$TERMUX_REPO/$url" -o "$deb"
        else echo "  $pkg (not found)"; fi
    }

    extract_pkg() {
        local pkg="$1"
        local deb="$DEB_CACHE/${pkg}.deb"
        [ ! -f "$deb" ] && return
        mkdir -p /tmp/termux-extract-$$
        (cd /tmp/termux-extract-$$ && ar x "$deb" 2> /dev/null && tar -xf data.tar.* -C "$TERMUX_ROOT" 2> /dev/null) || true
        rm -rf /tmp/termux-extract-$$
    }

    echo "Required Termux packages:"
    TERMUX_PACKAGES=(
        xorgproto libx11 libxext libxrender libxrandr libxfixes libxi libxcursor
        libxcb libxau libxdmcp libandroid-support
        freetype fontconfig libexpat
        alsa-lib pulseaudio gnutls libgnutls libgmp libnettle libidn2 libunistring
        glib gstreamer gst-plugins-base gst-plugins-bad libffi zlib libpng
        libglvnd
    )

    for pkg in "${TERMUX_PACKAGES[@]}"; do download_pkg "$pkg"; done
    echo "Extracting packages..."
    for pkg in "${TERMUX_PACKAGES[@]}"; do extract_pkg "$pkg"; done

    find "$TERMUX_PREFIX" -name "*.la" -delete 2> /dev/null || true
    find "$TERMUX_PREFIX" -type l -lname "/data/*" 2> /dev/null | while read -r link; do
        target=$(readlink "$link")
        [ -e "${TERMUX_ROOT}${target}" ] && ln -sf "${TERMUX_ROOT}${target}" "$link" 2> /dev/null
    done

    touch "$TERMUX_PREFIX/.deps-ready"
    export DEPS="$TERMUX_PREFIX"
    info "Termux deps bootstrap complete"
    info "DEPS=$DEPS"
}

# ── clone ─────────────────────────────────────────────────────
clone_wine() {
    [ "$DO_CLONE" -ne 1 ] && return
    mkdir -p "$WORKDIR"
    if [ -d "$WORKDIR/wine" ]; then
        warn "Wine source already exists, skipping clone"
    else
        echo "Cloning Valve wine (branch: $WINE_BRANCH)..."
        git clone --depth 1 --branch "$WINE_BRANCH" "$WINE_REPO" "$WORKDIR/wine"
    fi
    WINE_VERSION=$(cd "$WORKDIR/wine" && git rev-parse --short HEAD)
    info "Wine version: $WINE_VERSION"
}

# ── sysvshm ───────────────────────────────────────────────────
build_sysvshm() {
    [ "$DO_SYSVSHM" -ne 1 ] && return
    [ -z "$DEPS" ] && {
        warn "DEPS not set, skipping sysvshm build"
        return
    }

    # auto-clone if needed
    if [ ! -d "$WORKDIR/wine/android/android_sysvshm" ]; then
        mkdir -p "$WORKDIR"
        git clone --depth 1 --branch "$WINE_BRANCH" "$WINE_REPO" "$WORKDIR/wine" 2> /dev/null
    fi
    [ ! -d "$WORKDIR/wine/android/android_sysvshm" ] && {
        warn "android_sysvshm not found in wine source"
        return
    }

    SYSVSHM_DIR="$WORKDIR/wine/android/android_sysvshm"
    echo "Building android_sysvshm library..."
    $CC -Wall -std=gnu99 -shared -fPIC -I"$SYSVSHM_DIR" \
        -o "$SYSVSHM_DIR/libandroid-sysvshm.so" "$SYSVSHM_DIR/android_sysvshm.c"
    if [ -f "$SYSVSHM_DIR/libandroid-sysvshm.so" ]; then
        mkdir -p "$DEPS/lib"
        cp "$SYSVSHM_DIR/libandroid-sysvshm.so" "$DEPS/lib/"
        info "android_sysvshm built -> $DEPS/lib/"
    fi
}

# ── generate sources ──────────────────────────────────────────
generate_sources() {
    [ "$DO_GENERATE" -ne 1 ] && return
    [ ! -d "$WORKDIR/wine" ] && die "Wine source not found. Run --clone first."
    cd "$WORKDIR/wine"

    echo "Generating auto-generated sources..."

    echo "  -> server_protocol.def patch..."
    [ -f "android/patches/server_protocol.def.patch" ] \
        && patch -p1 -s < android/patches/server_protocol.def.patch 2> /dev/null \
        && echo "     server_protocol.def patched" && mv android/patches/server_protocol.def.patch android/patches/server_protocol.def.patch.applied \
        || echo "     server_protocol.def patch SKIPPED"

    echo "  -> make_vulkan..."
    python3 dlls/winevulkan/make_vulkan 2>&1 || echo "  make_vulkan completed with warnings"

    echo "  -> make_requests..."
    ./tools/make_requests 2>&1 || true

    echo "  -> make_specfiles..."
    perl tools/make_specfiles 2>&1 || true

    echo "  -> autoreconf..."
    autoreconf -fi 2>&1 || {
        autoconf 2>&1 || true
        autoheader 2>&1 || true
    }

    if [ ! -f "include/drm.h" ] && [ ! -f "include/drm/drm.h" ]; then
        mkdir -p include/drm
        touch include/drm/drm.h include/drm/drm_mode.h include/drm/drm_fourcc.h
        ln -sf ../include/drm/drm.h include/drm.h
    fi
    info "Source generation complete"
}

# ── native tools ──────────────────────────────────────────────
build_native_tools() {
    [ "$DO_TOOLS" -ne 1 ] && return
    [ ! -d "$WORKDIR/wine" ] && die "Wine source not found. Run --clone first."
    [ "$IS_AARCH64" -eq 1 ] && {
        info "Native build on aarch64 host (no wine-tools needed)"
        return
    }

    echo "Building native Wine tools..."
    rm -rf "$WORKDIR/wine-tools"
    mkdir -p "$WORKDIR/wine-tools"
    cd "$WORKDIR/wine-tools"
    (
        unset CC CXX AR AS LD RANLIB STRIP DLLTOOL
        unset CFLAGS CXXFLAGS LDFLAGS CPPFLAGS
        unset TARGET SYSROOT install_dir WIN_ARCH
        unset PKG_CONFIG_LIBDIR ACLOCAL_PATH
        unset FREETYPE_CFLAGS PULSE_CFLAGS PULSE_LIBS SDL2_CFLAGS SDL2_LIBS
        unset FONTCONFIG_LIBS X_CFLAGS X_LIBS GSTREAMER_CFLAGS GSTREAMER_LIBS
        unset FFMPEG_CFLAGS FFMPEG_LIBS DEPS RUNTIME_PATH
        "$WORKDIR/wine/configure" \
            --enable-win64 --disable-tests --enable-archs=x86_64 \
            --without-x --without-fontconfig --without-opengl --without-wayland --without-dbus \
            --without-udev --without-cups --without-sane --without-gstreamer --without-coreaudio \
            --without-capi --without-gphoto --without-inotify --without-krb5 \
            --without-opencl --without-oss --without-pcap --without-sdl --without-usb \
            --without-v4l2 --without-pcsclite --without-ffmpeg --without-pthread \
            2>&1 | tee "$WORKDIR/tools_configure_log"
        make tools tools/winebuild/winebuild tools/winegcc/winegcc \
            tools/wrc/wrc tools/widl/widl tools/wmc/wmc \
            tools/wine/wine tools/sfnt2fon/sfnt2fon tools/make_xftmpl \
            2>&1 | tee "$WORKDIR/tools_build_log"
    )
    info "Native tools build complete"
}

# ── configure ─────────────────────────────────────────────────
configure_wine() {
    [ "$DO_CONFIGURE" -ne 1 ] && return
    [ ! -d "$WORKDIR/wine" ] && die "Wine source not found. Run --clone first."
    cd "$WORKDIR/wine"

    WINE_TOOLS_OPT=""
    if [ "$IS_AARCH64" -eq 0 ] && [ -d "$WORKDIR/wine-tools" ]; then
        WINE_TOOLS_OPT="--with-wine-tools=$WORKDIR/wine-tools"
    fi

    echo "Configuring Wine (aarch64-linux-android)..."
    ./configure \
        --enable-archs="$WIN_ARCH" --host="$TARGET" \
        --prefix "$install_dir" --bindir "$install_dir/bin" \
        --libdir "$install_dir/lib" --exec-prefix "$install_dir" \
        --with-mingw=clang ${WINE_TOOLS_OPT:+"$WINE_TOOLS_OPT"} \
        --enable-win64 --disable-win16 --enable-nls \
        --disable-amd_ags_x64 --enable-wineandroid_drv=no --disable-tests \
        --with-alsa --without-capi --without-coreaudio --without-cups \
        --without-dbus --without-ffmpeg --with-fontconfig --with-freetype \
        --without-gcrypt --without-gettext --with-gettextpo=no --without-gphoto \
        --with-gnutls --without-gssapi --with-gstreamer --without-inotify \
        --without-krb5 --without-netapi --without-opencl --with-opengl \
        --without-osmesa --without-oss --without-pcap --without-pcsclite \
        --without-piper --with-pthread --with-pulse --without-sane \
        --without-sdl --without-udev --without-unwind --without-usb \
        --without-v4l2 --without-vosk --with-vulkan --without-wayland \
        --without-xcomposite --without-xfixes --without-xinerama \
        --without-xrandr --without-xrender --without-xshape --with-xshm \
        --without-xxf86vm \
        2>&1 | tee "$WORKDIR/configure_log"
    info "Configure complete"

    apply_patches
    fixup_makefile
}

apply_patches() {
    echo "Applying patches..."

    apply_patch_file() {
        local patch_file="$1"
        local patch_name="$(basename "$patch_file")"
        echo "----------------------------------------"
        echo "Applying: $patch_name"
        if git apply --check "$patch_file" > /dev/null 2>&1; then
            git apply "$patch_file" && { echo "SUCCESS: $patch_name applied"; return 0; }
            echo "FAILED: error applying $patch_name"
            return 1
        fi
        if grep -q "^new file\|^--- /dev/null\|already exists" "$patch_file" 2> /dev/null; then
            DST_FILE=$(grep "^+++ b/" "$patch_file" | head -1 | sed 's|^+++ b/||' | sed 's/\t.*//')
            [ -n "$DST_FILE" ] && [ -e "$DST_FILE" ] && rm -f "$DST_FILE"
            patch -p1 -s < "$patch_file" 2> /dev/null && { echo "SUCCESS: $patch_name applied (new file)"; return 0; }
        fi
        echo "SKIPPED: $patch_name does not apply cleanly"
        return 1
    }

    # remote patches directory (defined in packages.yml, relative to wine source)
    if [ -n "$REMOTE_PATCH_DIR" ] && [ -d "$REMOTE_PATCH_DIR" ]; then
        local patch_count=0
        for patch_file in "$REMOTE_PATCH_DIR"/*.patch; do
            [ -f "$patch_file" ] || continue
            apply_patch_file "$patch_file" || true
            patch_count=$((patch_count + 1))
        done
        echo "  ($patch_count remote patches)"
    fi

    # local patches directory (defined in packages.yml, relative to repo root)
    if [ -n "$LOCAL_PATCH_DIR" ] && [ -d "$LOCAL_PATCH_DIR" ]; then
        echo "--- local patches ---"
        local patch_count=0
        for patch_file in "$LOCAL_PATCH_DIR"/*.patch; do
            [ -f "$patch_file" ] || continue
            apply_patch_file "$patch_file" || true
            patch_count=$((patch_count + 1))
        done
        echo "  ($patch_count local patches)"
    fi

    info "Done applying patches."
}

fixup_makefile() {
    # Makefile-only fixes applied after configure (generated file, not patchable)
    echo "Disabling winedmo Unix build (no ffmpeg)..."
    sed -i '/dlls\/winedmo\/winedmo.so:/,/^$/d' Makefile
    sed -i '/winedmo\/winedmo.so/d' Makefile
    # -Wl,-Ttext=0x7d400000 produces 2GB sparse ELF with LLD 21.
    # build_preloader() uses --image-base instead (compact output).
    sed -i 's|-Wl,-Ttext=0x7d400000||g' Makefile
}

# ── build ─────────────────────────────────────────────────────
build_wine() {
    [ "$DO_BUILD" -ne 1 ] && return
    [ ! -d "$WORKDIR/wine" ] && die "Wine source not found. Run --clone first."
    cd "$WORKDIR/wine"

    echo "Building Wine..."
    set -o pipefail
    WINE_JOBS="${WINE_JOBS:-$(nproc)}"

    echo "  -> make depend..."
    make depend 2>&1 | tee -a "$WORKDIR/build_log"

    echo "  -> make all..."
    make -j"$WINE_JOBS" 2>&1 | tee -a "$WORKDIR/build_log"
    BUILD_RC=${PIPESTATUS[0]}
    set +o pipefail

    if [ "$BUILD_RC" -ne 0 ]; then
        die "Build FAILED (make exit code: $BUILD_RC) - check $WORKDIR/build_log"
    fi
    info "Wine binary built successfully"

    if [ "$IS_AARCH64" -eq 0 ]; then
        build_aarch64_unix_libs
        echo "Fixing rpath..."
        find . -name "*.so" -type f | while read -r so; do
            patchelf --remove-rpath "$so" 2>/dev/null || true
        done
    fi

    echo "Key binaries:"
    ls -la loader/wine loader/wine-preloader server/wineserver 2>&1
}

build_aarch64_unix_libs() {
    echo "Building aarch64-unix libraries..."
    rm -f server/fsync.o server/wineserver loader/main.o loader/wine loader/wine-preloader
    make -j"$WINE_JOBS" server/wineserver loader/wine 2>&1 | tee -a "$WORKDIR/build_log"

    grep "\.so:" Makefile | grep -v "i386-\|x86_64-\|aarch64-windows\|arm64ec-" | awk -F: '{print $1}' | while IFS= read -r so_target; do
        [ -f "$so_target" ] || make -j"$WINE_JOBS" "$so_target" 2>&1 | tee -a "$WORKDIR/build_log" || true
    done

    build_winex11_so
    build_wine_loader
    build_preloader
    build_wineserver
    build_missing_so
    info "Unix libraries build complete"
}

build_winex11_so() {
    [ -f "dlls/winex11.drv/winex11.so" ] && return
    ls dlls/winex11.drv/*.o > /dev/null 2>&1 || return
    echo "Linking winex11.so manually..."
    $CC -std=gnu23 -shared -fPIC -Wl,-soname,winex11.so -Wl,-Bsymbolic -Wl,-z,defs \
        -o dlls/winex11.drv/winex11.so dlls/winex11.drv/*.o \
        dlls/ntdll/ntdll.so dlls/win32u/win32u.so \
        $X_LIBS -lm $LDFLAGS \
        -Wl,-rpath=\$ORIGIN/../../lib --sysroot="$SYSROOT" \
        2>&1 | tee -a "$WORKDIR/build_log"
}

build_wine_loader() {
    [ -f "loader/wine" ] && return
    echo "Building loader/wine manually..."
    $CC -std=gnu23 -c -o loader/main.o loader/main.c -Iloader -Iinclude \
        -D__WINESRC__ -DWINE_UNIX_LIB -fPIE -Wall -pipe \
        -fcf-protection=none -fvisibility=hidden -fno-stack-protector -fno-strict-aliasing \
        -I$DEPS/include --sysroot="$SYSROOT" $CFLAGS 2>&1 | tee -a "$WORKDIR/build_log"
    $CC -std=gnu23 -o loader/wine loader/main.o \
        -Wl,--export-dynamic -Wl,-pie $LDFLAGS 2>&1 | tee -a "$WORKDIR/build_log"
}

build_preloader() {
    echo "Building loader/wine-preloader with custom linker script..."
    $CC -std=gnu23 -c -o loader/preloader.o loader/preloader.c -Iloader -Iinclude \
        -D__WINESRC__ -fno-builtin -Wall -pipe \
        -fcf-protection=none -fvisibility=hidden -fno-stack-protector -fno-strict-aliasing \
        -I$DEPS/include --sysroot="$SYSROOT" $CFLAGS 2>&1 | tee -a "$WORKDIR/build_log"
    $CC -std=gnu23 -static -nostartfiles -nodefaultlibs \
        -Wl,--image-base=0x7d400000 \
        -o loader/wine-preloader loader/preloader.o \
        2>&1 | tee -a "$WORKDIR/build_log"
}

build_wineserver() {
    [ -f "server/wineserver" ] && return
    echo "Building server/wineserver manually..."
    [ ! -f "server/fsync.o" ] \
        && $CC -std=gnu23 -c -o server/fsync.o server/fsync.c -Iserver -Iinclude \
            -D__WINESRC__ -DWINE_UNIX_LIB -Wall -pipe \
            -fcf-protection=none -fvisibility=hidden -fno-stack-protector -fno-strict-aliasing \
            -I$DEPS/include --sysroot="$SYSROOT" $CFLAGS 2>&1 | tee -a "$WORKDIR/build_log"
    $CC -std=gnu23 -o server/wineserver server/*.o \
        -Wl,-pie $LDFLAGS --sysroot="$SYSROOT" \
        2>&1 | tee -a "$WORKDIR/build_log"
}

build_missing_so() {
    for pair in \
        "ws2_32:ws2_32.so:dlls/ntdll/ntdll.so" \
        "wineps.drv:wineps.so:dlls/ntdll/ntdll.so dlls/win32u/win32u.so" \
        "winepulse.drv:winepulse.so:dlls/ntdll/ntdll.so -L$DEPS/lib/pulseaudio -lpulse" \
        "winevulkan:winevulkan.so:dlls/ntdll/ntdll.so dlls/win32u/win32u.so" \
        "winspool.drv:winspool.so:dlls/ntdll/ntdll.so"; do
        dname="${pair%%:*}" rest="${pair#*:}"
        sname="${rest%%:*}" deps="${rest#*:}"
        target="dlls/$dname/$sname"
        [ -f "$target" ] && continue
        ls "dlls/$dname"/*.o > /dev/null 2>&1 || continue
        echo "Linking $target manually..."
        $CC -std=gnu23 -shared -fPIC -Wl,-Bsymbolic -Wl,-soname,$sname -Wl,-z,defs \
            -o "$target" dlls/$dname/*.o $deps -lm $LDFLAGS \
            -Wl,-rpath=\$ORIGIN/../../lib --sysroot="$SYSROOT" \
            2>&1 | tee -a "$WORKDIR/build_log" || warn "  (non-fatal) $sname skipped"
    done
}

# ── programs ──────────────────────────────────────────────────
build_programs() {
    [ "$DO_BUILD_PROGRAMS" -ne 1 ] && return
    [ ! -d "$WORKDIR/wine" ] && die "Wine source not found. Run --clone first."
    cd "$WORKDIR/wine"

    echo "Building program .exe files..."
    for pe_arch in aarch64 i386; do
        grep -oP 'programs/\S+/'"$pe_arch"'-windows/\S+\.exe(?=:)' Makefile | sort -u | while IFS= read -r target; do
            [ -f "$target" ] && continue
            make "$target" 2> /dev/null && echo "  $target"
        done
    done
    info "Program .exe files built"
}

# ── package ───────────────────────────────────────────────────
package_wine() {
    [ "$DO_PACKAGE" -ne 1 ] && return
    [ ! -d "$WORKDIR/wine" ] && die "Wine source not found. Run --clone first."
    cd "$WORKDIR/wine"

    WINE_VERSION=$(git rev-parse --short HEAD)
    PKGDIR="$WORKDIR/package"
    rm -rf "$PKGDIR"
    mkdir -p "$PKGDIR/bin"
    for pe_arch in aarch64 i386; do
        mkdir -p "$PKGDIR/lib/wine/${pe_arch}-windows"
        mkdir -p "$PKGDIR/lib/wine/${pe_arch}-unix"
    done

    echo "Packaging..."

    # core binaries
    cp loader/wine "$PKGDIR/lib/wine/aarch64-unix/" 2> /dev/null || true
    cp loader/wine-preloader "$PKGDIR/lib/wine/aarch64-unix/" 2> /dev/null || true
    cp server/wineserver "$PKGDIR/bin/" 2> /dev/null || true

    # PE files from dlls + libs
    for pe_arch in aarch64 i386; do
        arch_dir="${pe_arch}-windows"
        dest="$PKGDIR/lib/wine/$arch_dir"
        find dlls -name "$arch_dir" -type d 2> /dev/null | while IFS= read -r dll_dir; do
            for f in "$dll_dir"/*; do
                [ -f "$f" ] || continue
                case "$f" in *.dll | *.drv | *.exe | *.ocx | *.cpl | *.acm | *.ax | *.tlb | *.sys) cp "$f" "$dest/" ;; esac
            done
        done
        find libs -name "$arch_dir" -type d 2> /dev/null | while IFS= read -r lib_dir; do
            for f in "$lib_dir"/*; do
                [ -f "$f" ] || continue
                case "$f" in *.dll | *.drv | *.exe | *.ocx | *.cpl | *.acm | *.ax | *.tlb | *.sys) cp "$f" "$dest/" ;; esac
            done
        done
    done

    # program symlinks in bin/
    for pe_arch in aarch64 i386; do
        arch_dir="${pe_arch}-windows"
        find dlls programs -name "$arch_dir" -type d 2> /dev/null | while IFS= read -r f; do
            for dll in "$f"/*.dll; do
                [ -f "$dll" ] || continue
                dllname=$(basename "$dll" .dll)
                case "$dllname" in
                    winecfg | notepad | regedit | winefile | wineconsole | winebrowser | winepath | \
                        winedbg | winemine | msiexec | regsvr32 | wineboot | taskmgr | progman | \
                        control | cmd | start | xcopy | uninstaller | explorer | iexplore | \
                        wordpad | write | oleview | winhlp32 | view | servicemode | \
                        wmic | rundll32 | msidb | cscript | wscript | schtasks)
                        ln -sf wine "$PKGDIR/bin/$dllname" 2> /dev/null
                        ;;
                esac
            done
        done
    done

    # all .so files
    find . -name "*.so" -type f 2> /dev/null | while IFS= read -r f; do
        cp "$f" "$PKGDIR/lib/wine/aarch64-unix/"
    done

    # program .exe files
    for pe_arch in aarch64 i386; do
        for prog_dir in programs/*/${pe_arch}-windows; do
            for exe in "$prog_dir"/*.exe; do
                [ -f "$exe" ] && cp "$exe" "$PKGDIR/lib/wine/${pe_arch}-windows/"
            done
        done
    done

    # share (fonts, nls, wine.inf)
    mkdir -p "$PKGDIR/share/wine"
    for sub in fonts nls; do
        [ -d "$sub" ] && cp -a "$sub" "$PKGDIR/share/wine/"
    done
    [ -f "wine.inf" ] && cp wine.inf "$PKGDIR/share/wine/"
    [ -f "loader/wine.inf" ] && cp loader/wine.inf "$PKGDIR/share/wine/"

    # nls double-path fix (BIN_TO_DATADIR=../share/wine/ from lib/wine/aarch64-unix/)
    echo "Fixing data_dir path (nls files)..."
    mkdir -p "$PKGDIR/lib/wine/share/wine/nls"
    cp -a "$PKGDIR/share/wine/nls"/*.nls "$PKGDIR/lib/wine/share/wine/nls/" 2> /dev/null || true
    [ -f "$PKGDIR/share/wine/wine.inf" ] && cp "$PKGDIR/share/wine/wine.inf" "$PKGDIR/lib/wine/share/wine/" 2> /dev/null || true
    [ -d "$PKGDIR/share/wine/fonts" ] && cp -a "$PKGDIR/share/wine/fonts" "$PKGDIR/lib/wine/share/wine/" 2> /dev/null || true

    # prefix pack
    REF_WCP_URL="https://github.com/GameNative/proton-wine/releases/download/build-p11-20260509-sdk28/proton-11.0-1-arm64ec.wcp"
    REF_PREFIX="$WORKDIR/ref-prefixPack.txz"
    if [ ! -f "$REF_PREFIX" ]; then
        echo "Downloading reference prefixPack.txz..."
        curl -sL "$REF_WCP_URL" | tar -xJ -O prefixPack.txz > "$REF_PREFIX" 2> /dev/null
    fi
    if [ -f "$REF_PREFIX" ] && [ -s "$REF_PREFIX" ]; then
        cp "$REF_PREFIX" "$PKGDIR/prefixPack.txz"
        echo "Using reference prefix pack ($(du -h "$REF_PREFIX" | cut -f1))"
    else
        warn "Creating minimal prefix pack..."
        PREFIX_DIR="$WORKDIR/prefix"
        rm -rf "$PREFIX_DIR"
        mkdir -p "$PREFIX_DIR/.wine/dosdevices" "$PREFIX_DIR/.wine/drive_c/windows"
        touch "$PREFIX_DIR/.wine/.update-timestamp"
        for reg in system.reg user.reg userdef.reg; do
            echo "WINE REGISTRY Version 2" > "$PREFIX_DIR/.wine/$reg"
        done
        tar -cJf "$PKGDIR/prefixPack.txz" -C "$PREFIX_DIR" .wine
        rm -rf "$PREFIX_DIR"
    fi

    # bundled X11 runtime libs
    if [ -n "$DEPS" ] && [ -d "$DEPS/lib" ]; then
        echo "Bundling X11 runtime libraries..."
        mkdir -p "$PKGDIR/lib"
        for lib in libX11.so libX11.so.6 libXext.so libXext.so.6 \
            libxcb.so libxcb.so.1 libXau.so libXau.so.6 \
            libXdmcp.so libXdmcp.so.6 libandroid-support.so; do
            [ -f "$DEPS/lib/$lib" ] && cp -a "$DEPS/lib/$lib" "$PKGDIR/lib/" 2> /dev/null
        done
    fi

    echo "Skipping strip (debug symbols retained)..."

    # resolve external symlinks
    echo "Resolving symlinks..."
    find "$PKGDIR" -type l 2> /dev/null | while IFS= read -r f; do
        target=$(readlink -f "$f" 2> /dev/null)
        if [ -n "$target" ] && ! echo "$target" | grep -q "^$PKGDIR"; then
            [ -f "$target" ] && {
                rm "$f"
                cp "$target" "$f"
            }
        fi
    done

    ln -sf ../lib/wine/aarch64-unix/wine "$PKGDIR/bin/wine"
    ln -sf ../lib/wine/aarch64-unix/wine-preloader "$PKGDIR/bin/wine-preloader"

    # metadata
    cat > "$PKGDIR/profile.json" << 'PROEOF'
{
  "type": "Wine",
  "versionName": "11-arm64ec",
  "versionCode": 6,
  "description": "Wine 11 Proton ARM64EC (ESYNC/FSYNC, Oryon optimized)",
  "files": [],
  "wine": { "binPath": "bin", "libPath": "lib", "prefixPack": "prefixPack.txz" }
}
PROEOF
    cat > "$PKGDIR/Config.json" << 'CONFEOF'
{
  "env": {
    "WINEESYNC": "1", "WINEFSYNC": "1", "WINE_LARGE_ADDRESS_AWARE": "1",
    "PROTON_USEWOW64": "1", "WINEDEBUG": "-all",
    "WINE_CPU_TOPOLOGY": "8:0,1,2,3,4,5,6,7",
    "WINE_FULLSCREEN_FSR": "1", "WINE_FULLSCREEN_FSR_STRENGTH": "2",
    "DXVK_LOG_LEVEL": "none", "DXVK_ASYNC": "1",
    "VKD3D_DEBUG": "none", "VKD3D_SHADER_DEBUG": "none",
    "VKD3D_CONFIG": "no_upload_hvv"
  }
}
CONFEOF

    OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$WINE_VERSION/")
    WCP_FILE="$ROOT_DIR/$OUTPUT_FILE"
    echo "Compressing package..."
    (cd "$PKGDIR" && tar --format=gnu --owner=0 --group=0 -cf - \
        bin lib share prefixPack.txz profile.json Config.json 2> /dev/null \
        | xz -3 -T1 -c > "$WCP_FILE")
    info "Package created: $WCP_FILE ($(find "$PKGDIR" -type f | wc -l) files)"
    ls -lh "$WCP_FILE"

    echo "Verifying interpreter..."
    readelf -l "${PKGDIR}/bin/wine" 2> /dev/null | grep interpreter || true
}

# ── main (argument parse + dispatch) ──────────────────────────
DO_CLONE=0
DO_GENERATE=0
DO_TOOLS=0
DO_CONFIGURE=0
DO_BUILD=0
DO_BUILD_PROGRAMS=0
DO_PACKAGE=0
DO_SYSVSHM=0
DO_SETUP_DEPS=0
ENABLE_16KB=0
NO_OPT=0

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo "  --setup          Full pipeline: deps -> clone -> generate -> tools -> sysvshm -> configure -> build -> programs -> package"
    echo "  --setup-deps     Bootstrap Termux aarch64 dependencies"
    echo "  --clone          Clone Wine source"
    echo "  --generate       Generate auto-generated sources + autoreconf"
    echo "  --build-tools    Build native Wine tools (x86_64 only)"
    echo "  --build-sysvshm  Build Android SysV shared memory library"
    echo "  --configure      Run ./configure +apply patches +fix Makefile"
    echo "  --build          Build Wine and Unix libraries"
    echo "  --build-programs Build PE .exe program files"
    echo "  --package        Create .wcp package"
    echo "  --enable-16kb-pages  Enable 16KB page size support"
    echo "  --no-opt         Disable CPU optimizations (-O0)"
    exit 0
}

[ $# -eq 0 ] && usage

for arg in "$@"; do
    case "$arg" in
        --setup)
            DO_SETUP_DEPS=1
            DO_CLONE=1
            DO_GENERATE=1
            DO_TOOLS=1
            DO_SYSVSHM=1
            DO_CONFIGURE=1
            DO_BUILD=1
            DO_BUILD_PROGRAMS=1
            DO_PACKAGE=1
            ;;
        --setup-deps) DO_SETUP_DEPS=1 ;;
        --clone) DO_CLONE=1 ;;
        --generate) DO_GENERATE=1 ;;
        --build-tools) DO_TOOLS=1 ;;
        --build-sysvshm) DO_SYSVSHM=1 ;;
        --configure) DO_CONFIGURE=1 ;;
        --build) DO_BUILD=1 ;;
        --build-programs) DO_BUILD_PROGRAMS=1 ;;
        --package) DO_PACKAGE=1 ;;
        --enable-16kb-pages) ENABLE_16KB=1 ;;
        --no-opt) NO_OPT=1 ;;
        -h | --help) usage ;;
        *) die "Unknown argument: $arg" ;;
    esac
done

setup_env
setup_toolchain
apply_16kb_pages
setup_deps
clone_wine
build_sysvshm
setup_compiler_flags
generate_sources
build_native_tools
configure_wine
build_wine
build_programs
package_wine
