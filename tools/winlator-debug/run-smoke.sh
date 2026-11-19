#!/bin/bash
# Launch a Winlator smoke shortcut headlessly and collect its log.
#
#   run-smoke.sh <shortcut> [timeout]
#
# Requires the phone unlocked and the screen on: a locked screen pauses the
# activity and tears the guest down mid-run (that is how the first attempt died).
#
# The guest's stdout/stderr is mirrored into DebugDialog's file under
# /sdcard/Winlator/logs/, so that file is the whole record of the run.
set -euo pipefail

PKG=com.ludashi.benchmark
NAME="${1:?usage: run-smoke.sh <smoke|d3d9|bench11|bench9> [timeout]}"
TIMEOUT="${2:-240}"
CPATH="/data/user/0/$PKG/files/imagefs/home/xuser-5/.wine/drive_c/users/xuser/Desktop/$NAME.desktop"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUTDIR="$ROOT/artifacts/device"
mkdir -p "$OUTDIR"

adb shell input keyevent KEYCODE_WAKEUP >/dev/null 2>&1 || true
if adb shell "dumpsys window | grep -q 'isKeyguardShowing=true'"; then
  echo "device is locked - unlock it, the run would be killed mid-flight" >&2
  exit 3
fi

adb shell am force-stop "$PKG"
sleep 2

BEFORE=$(adb shell "ls /sdcard/Winlator/logs/ 2>/dev/null | wc -l")
echo "== launching $NAME"
adb shell am start -n "$PKG/com.winlator.cmod.XServerDisplayActivity" \
  --es shortcut_path "$CPATH" --ei container_id 5 >/dev/null

LOG=""
for _ in $(seq 1 "$TIMEOUT"); do
  sleep 1
  # Newest log file that did not exist before the launch.
  CAND=$(adb shell "ls -t /sdcard/Winlator/logs/smoke_*.txt 2>/dev/null | head -1" | tr -d '\r')
  if [ -n "$CAND" ] && [ "$CAND" != "$LOG" ]; then
    LOG="$CAND"
  fi
  if [ -n "$LOG" ] && adb shell "grep -q RESULT '$LOG'" 2>/dev/null; then
    break
  fi
done

if [ -z "$LOG" ]; then
  echo "no log file appeared" >&2
  exit 1
fi

sleep 2
DEST="$OUTDIR/$NAME-$(date +%Y%m%d-%H%M%S).txt"
adb shell "cat '$LOG'" > "$DEST"
adb shell am force-stop "$PKG" >/dev/null 2>&1 || true

echo "== log: $DEST"
grep -E "DXVK: v|Build:|Found device|Driver *:|Pipeline libraries|Binding model|Maximum supported feature level|compiler threads|mode:|BENCH|RESULT|FAIL|adapter" "$DEST" || true
