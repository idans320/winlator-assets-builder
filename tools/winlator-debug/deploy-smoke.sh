#!/bin/bash
# Deploy the DXVK smoke shortcuts into a Winlator container.
#
#   deploy-smoke.sh [container]        default: 5
#
# Files are pushed through `run-as` + stdin (base64) because the app itself has
# no direct /sdcard read access under scoped storage, so `adb push` + `cp` does
# not work for anything the app must read.
#
# Each .desktop pins the DXVK version and Turnip driver as shortcut extras, the
# same mechanism ShortcutSettingsDialog uses, so the container's own settings
# and the app UI are left untouched.
set -euo pipefail

PKG=com.ludashi.benchmark
CONTAINER="${1:-5}"
CROOT="files/imagefs/home/xuser-$CONTAINER"
DESKTOP="$CROOT/.wine/drive_c/users/xuser/Desktop"
EXEDIR="$CROOT/.wine/drive_c/smoke"
EXE_WIN='C:\\\\smoke\\\\smoke.exe'
WINEPREFIX="/data/user/0/$PKG/files/imagefs/home/xuser/.wine/dosdevices/z:/home/xuser/.wine"
DXVK="${DXVK_VERSION:-3.1.1-arm64ec-upstream-1}"
SUFFIX="${SUFFIX:-}"
DRIVER='Mesa Turnip Driver 26.3.0-devel'
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOCAL="$ROOT/tools/arm-test/apps/d3d_smoke/smoke.exe"
TMP=/tmp/opencode/winlator-stage

ENVVARS='WRAPPER_MAX_IMAGE_COUNT=0 VKD3D_SHADER_MODEL=6_6 ZINK_DESCRIPTORS=lazy ZINK_DEBUG=compact MESA_SHADER_CACHE_DISABLE=false MESA_SHADER_CACHE_MAX_SIZE=512MB mesa_glthread=true WINEESYNC=1 TU_DEBUG=sysmem,noconform DXVK_HUD=devinfo,fps,gpuload,version DXVK_DISABLE_TIMELINE_SEMAPHORES=1 DXVK_LOG_LEVEL=info'
DRIVERCFG="vulkanVersion=1.3;version=$DRIVER;blacklistedExtensions=;maxDeviceMemory=0;presentMode=mailbox;syncFrame=0;disablePresentWait=0;resourceType=auto;bcnEmulation=auto;bcnEmulationType=compute;bcnEmulationCache=0;gpuName=Device"
DXCFG="version=$DXVK,framerate=0,async=0,asyncCache=0,maxFrameLatency=0,vkd3dVersion=None,vkd3dLevel=12_1,ddrawrapper=none,csmt=3,gpuName=NVIDIA GeForce GTX 480,videoMemorySize=2048,strict_shader_math=1,OffscreenRenderingMode=fbo,renderer=gl"

push_runas() { # <local file> <container-relative dest>
  base64 -w0 "$1" > "$TMP.b64"
  adb shell "run-as $PKG sh -c 'tr -d \"\\r\" | base64 -d > $2'" < "$TMP.b64"
}

make_desktop() { # <name> <args> <out>
  cat > "$3" <<EOF
[Desktop Entry]
Name=$1
Exec=env WINEPREFIX="$WINEPREFIX" wine $EXE_WIN
Type=Application
StartupNotify=true
Icon=smoke
container_id:$CONTAINER

[Extra Data]
execArgs=$2
screenSize=1280x720
# driver comes from the container, so a stale shortcut cannot shadow it
dxwrapper=dxvk+vkd3d
dxwrapperConfig=$DXCFG
audioDriver=pulseaudio
emulator=fexcore
inputType=4
controlsProfile=3
envVars=$ENVVARS
EOF
}

mkdir -p "$TMP"
adb shell "run-as $PKG mkdir -p '$EXEDIR' '$DESKTOP'"

echo "== exe"
push_runas "$LOCAL" "$EXEDIR/smoke.exe"
adb shell "run-as $PKG sh -c 'md5sum $EXEDIR/smoke.exe; ls -l $EXEDIR/smoke.exe'"

echo "== shortcuts"
for spec in "smoke:" "d3d9:--d3d9" "bench11:--bench11" "bench9:--bench9" "bench9vb:--bench9vb"; do
  name=${spec%%:*}
  args=${spec#*:}
  make_desktop "$name" "$args" "$TMP/$name$SUFFIX.desktop"
  push_runas "$TMP/$name$SUFFIX.desktop" "$DESKTOP/$name$SUFFIX.desktop"
  adb shell "run-as $PKG chmod 600 '$DESKTOP/$name$SUFFIX.desktop'"
done
adb shell "run-as $PKG ls -l '$DESKTOP'"
echo "== sample:"
adb shell "run-as $PKG cat '$DESKTOP/bench11$SUFFIX.desktop'"
