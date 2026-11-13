#!/bin/bash -e
# Fetch the ARM64EC-capable llvm-mingw toolchain.
#
# Upstream llvm-mingw does not ship ARM64EC runtime libraries.  The FEX project
# documents bylaws/llvm-mingw as the toolchain to use for ARM64EC, and its
# releases provide prebuilt Linux cross compilers for both host arches.
#
# Version and asset name are pinned in packages.yml under toolchain.arm64ec_mingw.
#
# After this runs the toolchain is at toolchain/arm64ec (symlink to the pinned
# version) and exported by the devbox shell hook as ARM64EC_MINGW_ROOT, with
# toolchain/arm64ec/bin prepended to PATH.

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PKG_FILE="$ROOT_DIR/packages.yml"

if [ ! -f "$PKG_FILE" ]; then
    echo "packages.yml not found" >&2
    exit 1
fi

VER=$(yq '.toolchain.arm64ec_mingw.version' "$PKG_FILE")
REPO=$(yq '.toolchain.arm64ec_mingw.repo' "$PKG_FILE")
ASSET_TPL=$(yq '.toolchain.arm64ec_mingw.asset' "$PKG_FILE")

if [ -z "$VER" ] || [ "$VER" = "null" ]; then
    echo "toolchain.arm64ec_mingw.version missing from packages.yml" >&2
    exit 1
fi

HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
    x86_64 | aarch64) ;;
    *)
        echo "Unsupported host architecture: $HOST_ARCH" >&2
        exit 1
        ;;
esac

ASSET="${ASSET_TPL//\{version\}/$VER}"
ASSET="${ASSET//\{host_arch\}/$HOST_ARCH}"
URL="$REPO/releases/download/$VER/$ASSET"

TOOLCHAIN_DIR="$ROOT_DIR/toolchain"
DEST="$TOOLCHAIN_DIR/llvm-mingw-arm64ec-$VER"
LINK="$TOOLCHAIN_DIR/arm64ec"

mkdir -p "$TOOLCHAIN_DIR"

if [ -x "$DEST/bin/arm64ec-w64-mingw32-clang" ]; then
    echo "ARM64EC toolchain already present: $DEST"
else
    echo "Downloading $URL"
    TMP="$(mktemp -d)"
    trap 'rm -rf "$TMP"' EXIT
    curl -fL --retry 3 -o "$TMP/toolchain.tar.xz" "$URL"
    mkdir -p "$DEST"
    tar -xJ -C "$DEST" --strip-components=1 -f "$TMP/toolchain.tar.xz"
    echo "Extracted to $DEST"
fi

ln -sfn "llvm-mingw-arm64ec-$VER" "$LINK"

for tool in arm64ec-w64-mingw32-clang arm64ec-w64-mingw32-clang++ \
            arm64ec-w64-mingw32-ar arm64ec-w64-mingw32-dlltool \
            arm64ec-w64-mingw32-windres; do
    if [ ! -x "$DEST/bin/$tool" ]; then
        echo "Missing toolchain binary: $DEST/bin/$tool" >&2
        exit 1
    fi
done

cat <<EOF

ARM64EC toolchain ready.

  ARM64EC_MINGW_ROOT=$LINK
  PATH entry:          $LINK/bin

To use it in the current shell:

  export ARM64EC_MINGW_ROOT="$LINK"
  export PATH="\$ARM64EC_MINGW_ROOT/bin:\$PATH"
EOF
