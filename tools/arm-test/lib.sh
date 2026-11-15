#!/bin/bash
# Shared configuration for the ARM64 Wine test harness.
#
# Everything is overridable from the environment; the build tree stays outside
# the repository (it is several GB) while the scripts themselves live in it.

set -euo pipefail

ARM_TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$ARM_TEST_DIR/../.." && pwd)"

# Wine source the build is configured against. Mounted read-only: the prefix
# symlinks its NLS files from here, so it must be present at run time.
WINE_SRC="${WINE_SRC:-$REPO_ROOT/wine/workdir/wine}"

# Configured build tree. There is no from-scratch configure here: the arm64ec
# cross toolchain lives inside that tree. Falls back to an existing tree in
# $HOME so the tool works without configuration on this machine.
WINE_BUILD="${WINE_BUILD:-${XDG_CACHE_HOME:-$HOME/.cache}/wine-arm-test/build}"
if [ ! -f "$WINE_BUILD/Makefile" ] && [ -f "$HOME/wine-arm64-native/Makefile" ]; then
  WINE_BUILD="$HOME/wine-arm64-native"
fi

PREFIX_DIR="${PREFIX_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/wine-arm-test/prefix}"
LOG_DIR="${LOG_DIR:-$REPO_ROOT/artifacts/arm-test}"

IMAGE="${IMAGE:-wine-arm-test}"

require_docker() {
  command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
  docker info >/dev/null 2>&1 || { echo "cannot talk to the docker daemon" >&2; exit 1; }
}

image_exists() {
  docker image inspect "$IMAGE" >/dev/null 2>&1
}

build_image() {
  image_exists && return
  echo "building $IMAGE (one-time)"
  docker build --platform linux/arm64 -t "$IMAGE" "$ARM_TEST_DIR"
}

# Host paths must be absolute for -v to behave predictably inside the container.
abs() { (cd "$1" && pwd); }
