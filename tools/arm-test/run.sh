#!/bin/bash
# Run a Windows executable under the locally built ARM64 Wine, headlessly.
#
#   tools/arm-test/run.sh --app-dir /path/to/game --exe bin/app.exe --args "-flag"
#
# Options:
#   --app-dir DIR     directory mounted as Z:\app (required)
#   --exe PATH        path inside --app-dir, or a Windows path (C:\...)
#   --args "..."      command line arguments
#   --dxvk DIR        copy DXVK dlls from DIR into system32 and force native
#   --fex DIR         copy FEX dlls (libarm64ecfex, libwow64fex, libFEXCore)
#   --prefix-dir DIR  override the prefix location
#   --recreate        throw the prefix away and boot a fresh one
#   --timeout SEC     kill after SEC seconds (default 300)
#   --log FILE        copy the run log here as well
#
# The full output is always written to $LOG_DIR/run-<timestamp>.log.
# Exit status is the app's, 124 on timeout, 125 if the run could not start.
source "$(dirname "$0")/lib.sh"

APP_DIR=""; EXE=""; ARGS=""; DXVK_DIR=""; FEX_DIR=""
RECREATE=0; TIMEOUT=300; LOG=""

while [ $# -gt 0 ]; do
  case "$1" in
    --app-dir) APP_DIR="$2"; shift 2 ;;
    --exe) EXE="$2"; shift 2 ;;
    --args) ARGS="$2"; shift 2 ;;
    --dxvk) DXVK_DIR="$2"; shift 2 ;;
    --fex) FEX_DIR="$2"; shift 2 ;;
    --prefix-dir) PREFIX_DIR="$2"; shift 2 ;;
    --recreate) RECREATE=1; shift ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --log) LOG="$2"; shift 2 ;;
    -h|--help) sed -n '2,19p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[ -n "$APP_DIR" ] && [ -d "$APP_DIR" ] || { echo "--app-dir is required" >&2; exit 2; }
[ -n "$EXE" ] || { echo "--exe is required" >&2; exit 2; }
[ -x "$WINE_BUILD/loader/wine" ] || { echo "no build in $WINE_BUILD - run build-wine.sh first" >&2; exit 2; }

require_docker
build_image
mkdir -p "$PREFIX_DIR" "$LOG_DIR"

STAMP=$(date +%Y%m%d-%H%M%S)
RUN_LOG="$LOG_DIR/run-$STAMP.log"

MOUNTS=(
  -v "$(abs "$WINE_BUILD"):/build"
  -v "$(abs "$WINE_SRC"):/src:ro"
  -v "$(abs "$APP_DIR"):/app:ro"
  -v "$(abs "$PREFIX_DIR"):/prefix"
)
[ -n "$DXVK_DIR" ] && MOUNTS+=(-v "$(abs "$DXVK_DIR"):/dxvk:ro")
[ -n "$FEX_DIR" ] && MOUNTS+=(-v "$(abs "$FEX_DIR"):/fex:ro")

docker run --rm -i --platform linux/arm64 "${MOUNTS[@]}" \
  -e EXE="$EXE" -e ARGS="$ARGS" -e TIMEOUT="$TIMEOUT" -e RECREATE="$RECREATE" \
  -e HAVE_DXVK="$([ -n "$DXVK_DIR" ] && echo 1 || echo 0)" \
  -e HAVE_FEX="$([ -n "$FEX_DIR" ] && echo 1 || echo 0)" \
  -e WINEDEBUG_OVERRIDE="${WINEDEBUG_OVERRIDE:-}" \
  -e WINEDLLOVERRIDES_EXTRA="${WINEDLLOVERRIDES_EXTRA:-}" \
  "$IMAGE" bash -s 2>&1 <<'CONTAINER' | tee "$RUN_LOG"
set -o pipefail

Xvfb :99 -screen 0 1280x720x24 >/dev/null 2>&1 &
sleep 2

export WINEPREFIX=/prefix
SYS=$WINEPREFIX/drive_c/windows/system32

if [ "$RECREATE" = "1" ]; then
  echo "== recreating prefix =="
  rm -rf "$WINEPREFIX"
fi

# A prefix is created once and then reused. Wine copies its DLLs into it at
# that point, so a rebuilt Wine leaves stale copies behind: refresh the one
# that matters most (ntdll) instead of paying for a full wineboot.
NTDLL=/build/dlls/ntdll/aarch64-windows/ntdll.dll
if [ ! -f "$SYS/ntdll.dll" ]; then
  mkdir -p "$WINEPREFIX"
  chown root:root "$WINEPREFIX"
  cd /build
  echo "== wineboot =="
  ./loader/wine wineboot -i >/build/boot.log 2>&1
  # wineboot returns before services finish writing system32
  ./loader/wine wineserver -w >/dev/null 2>&1 || true
elif [ -f "$NTDLL" ] && [ "$NTDLL" -nt "$SYS/ntdll.dll" ]; then
  cp -f "$NTDLL" "$SYS/ntdll.dll"
  echo "prefix kept, ntdll refreshed"
fi

if [ "$HAVE_FEX" = "1" ]; then
  cp /fex/*.dll "$SYS"/ 2>/dev/null && echo "fex dlls installed"
fi

if [ "$HAVE_DXVK" = "1" ]; then
  cp /dxvk/*.dll "$SYS"/ 2>/dev/null && echo "dxvk dlls installed"
  export WINEDLLOVERRIDES="d3d11,dxgi=n${WINEDLLOVERRIDES_EXTRA:+ }${WINEDLLOVERRIDES_EXTRA}"
elif [ -n "$WINEDLLOVERRIDES_EXTRA" ]; then
  export WINEDLLOVERRIDES="$WINEDLLOVERRIDES_EXTRA"
fi

export WINEDEBUG="${WINEDEBUG_OVERRIDE:--all}"

cd /app
echo "== run: $EXE $ARGS =="
timeout "$TIMEOUT" /build/loader/wine "$EXE" $ARGS
status=$?
echo "exit=$status"
exit $status
CONTAINER

status=${PIPESTATUS[0]}
echo "log: $RUN_LOG"
if [ -n "$LOG" ]; then
  mkdir -p "$(dirname "$LOG")" && cp "$RUN_LOG" "$LOG"
fi
exit "$status"
