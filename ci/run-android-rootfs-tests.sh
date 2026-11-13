#!/bin/bash
# ci/run-android-rootfs-tests.sh — run the Android Wine DLL tests inside a
# prepared rootfs (see ci/build-android-rootfs.sh).
#
# The rootfs contains /system from an Android system image, the Termux prefix at
# /data/data/com.termux/files/usr and the extracted Wine/FEX/DXVK payload. The
# tests execute inside it with chroot, which is what makes the hardcoded
# /system/bin/linker64 interpreter and the absolute RUNPATHs resolve.
#
# Runners:
#   --runner chroot   native ARM64 host (GitHub ubuntu-24.04-arm, self-hosted
#                     ARM64 Linux, or an Android container image)
#   --runner qemu     x86_64 host with qemu-user-static and binfmt registered
#                     (docker run --privileged multiarch/qemu-user-static
#                     --reset -p yes). Documented as a slow fallback: FEX still
#                     executes ARM64 natively *inside* the emulated guest, but
#                     every syscall goes through qemu.
#
# Usage:
#   ci/run-android-rootfs-tests.sh --root ci-root [--runner chroot] \
#       [--log-dir ci-logs/android-rootfs] [--shell]
#
# Exit codes: 0 all tests passed, 1 test failure, 2 preflight/runner failure.
set -uo pipefail

ROOT=""
RUNNER="chroot"
LOG_DIR="$PWD/ci-logs/android-rootfs"
SHELL_MODE=0

while [ $# -gt 0 ]; do
    case "$1" in
        --root) ROOT="${2:?}"; shift ;;
        --runner) RUNNER="${2:?}"; shift ;;
        --log-dir) LOG_DIR="${2:?}"; shift ;;
        --shell) SHELL_MODE=1 ;;
        -h | --help)
            sed -n '2,27p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
    shift
done

die() {
    echo "error: $*" >&2
    exit 2
}

note() { printf '  %s\n' "$*"; }

[ -n "$ROOT" ] || die "--root is required"
ROOT="$(cd "$ROOT" 2> /dev/null && pwd)" || die "rootfs not found"

[ -x "$ROOT/system/bin/linker64" ] ||
    die "$ROOT/system/bin/linker64 missing: build the rootfs with ci/build-android-rootfs.sh"
[ -d "$ROOT/opt/wine" ] || die "$ROOT/opt/wine missing"
[ -f "$ROOT/opt/ci/run-in-rootfs.sh" ] || die "$ROOT/opt/ci/run-in-rootfs.sh missing"
[ -f "$ROOT/opt/prefix/.wine/system.reg" ] ||
    die "$ROOT/opt/prefix/.wine/system.reg missing (prefixPack.txz not extracted)"

mkdir -p "$LOG_DIR" || die "cannot create $LOG_DIR"

printf '== android rootfs test ==\n'
note "root:   $ROOT"
note "runner: $RUNNER"

# chroot needs root; the GitHub ARM64 runners have passwordless sudo.
CHROOT=(chroot)
if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo > /dev/null 2>&1 && sudo -n true 2> /dev/null; then
        CHROOT=(sudo -n chroot)
    else
        die "chroot needs root (or passwordless sudo)"
    fi
fi

if [ "$RUNNER" = "qemu" ]; then
    if [ "$(uname -m)" = "aarch64" ]; then
        note "host is aarch64; --runner qemu is unnecessary"
    fi
    [ -e /proc/sys/fs/binfmt_misc/qemu-aarch64 ] ||
        die "binfmt for qemu-aarch64 is not registered.
       Run: docker run --privileged --rm multiarch/qemu-user-static --reset -p yes
       or:  sudo update-binfmts --enable qemu-aarch64"
    note "qemu-aarch64 binfmt detected (slow fallback)"
elif [ "$RUNNER" != "chroot" ]; then
    die "--runner must be chroot or qemu"
fi

if [ "$SHELL_MODE" -eq 1 ]; then
    exec "${CHROOT[@]}" "$ROOT" /system/bin/sh
fi

RC=0
"${CHROOT[@]}" "$ROOT" /system/bin/sh /opt/ci/run-in-rootfs.sh 2>&1 |
    tee "$LOG_DIR/run.log"
RC=${PIPESTATUS[0]}

# Copy the in-rootfs logs out so a failed CI run keeps the LoadLibrary error.
if [ -d "$ROOT/opt/work/logs" ]; then
    mkdir -p "$LOG_DIR/logs"
    cp -a "$ROOT/opt/work/logs/." "$LOG_DIR/logs/" 2> /dev/null || true
    note "logs: $LOG_DIR/logs"
fi

echo "exit code: $RC"
exit "$RC"
