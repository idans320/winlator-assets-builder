#!/bin/bash -e

green='\033[0;32m'
red='\033[0;31m'
nocolor='\033[0m'

PACKAGE_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$PACKAGE_DIR")"
WORKDIR="$PACKAGE_DIR/workdir"
PKG_NAME="winlator"

WINLATOR_REPO=$(yq ".${PKG_NAME}.repo" "$ROOT_DIR/packages.yml")
WINLATOR_BRANCH=$(yq ".${PKG_NAME}.branch" "$ROOT_DIR/packages.yml")

echo -e "${green}=== Winlator APK Builder ===${nocolor}"

mkdir -p "$WORKDIR"
if [ ! -d "$WORKDIR/winlator" ]; then
    echo "Cloning Winlator (branch: $WINLATOR_BRANCH)..."
    git clone --depth 1 --branch "$WINLATOR_BRANCH" "$WINLATOR_REPO" "$WORKDIR/winlator" 2>&1
fi

WINLATOR_VERSION=$(cd "$WORKDIR/winlator" && git rev-parse --short HEAD)
echo -e "${green}Winlator version: ${WINLATOR_VERSION}${nocolor}"

BUILD_DIR="$WORKDIR/winlator"

echo "Building APK..."
(
    cd "$BUILD_DIR"

    # Nix SDK is read-only — create writable overlay for licenses + sdkmanager
    WRITABLE_SDK="$WORKDIR/android-sdk-overlay"
    if [ ! -d "$WRITABLE_SDK/licenses" ]; then
        mkdir -p "$WRITABLE_SDK/licenses"
        # Accept SDK licenses into writable directory
        printf '\n8933bad161af4178b1185d1a37fbf41ea5269c55\nd56f5187479451eabf01fb78af6dfcb131a6481e\n24333f8a63b6825ea9c5514f83c2829b004d1fee\n' > "$WRITABLE_SDK/licenses/android-sdk-license"
        # Symlink SDK components from Nix store
        for dir in ndk ndk-bundle platform-tools platforms build-tools cmdline-tools tools add-ons; do
            [ -e "$ANDROID_HOME/$dir" ] && ln -s "$ANDROID_HOME/$dir" "$WRITABLE_SDK/$dir" 2>/dev/null
        done
    fi

    export ANDROID_HOME="$WRITABLE_SDK"
    export ANDROID_SDK_ROOT="$WRITABLE_SDK"
    echo "sdk.dir=$ANDROID_HOME" > local.properties

    # Find JDK 17+ (required by Android Gradle Plugin 8.x)
    if [ -d "/usr/lib/jvm/java-17-openjdk" ]; then
        export JAVA_HOME="/usr/lib/jvm/java-17-openjdk"
    elif [ -d "/usr/lib/jvm/java-21-openjdk" ]; then
        export JAVA_HOME="/usr/lib/jvm/java-21-openjdk"
    fi

    # Build release APK
    chmod +x gradlew
    ./gradlew assembleRelease 2>&1 | tail -20
)

APK_PATH=$(find "$BUILD_DIR/app/build/outputs/apk/release" -name "*.apk" 2>/dev/null | head -1)

if [ -z "$APK_PATH" ]; then
    # Try debug build if release failed
    echo "Release build not found, trying debug..."
    (
        cd "$BUILD_DIR"
        ./gradlew assembleDebug 2>&1 | tail -10
    )
    APK_PATH=$(find "$BUILD_DIR/app/build/outputs/apk/debug" -name "*.apk" 2>/dev/null | head -1)
fi

if [ -z "$APK_PATH" ]; then
    echo -e "${red}Build failed: APK not found${nocolor}"
    exit 1
fi

OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$WINLATOR_VERSION/")
cp "$APK_PATH" "$ROOT_DIR/$OUTPUT_FILE"

echo -e "${green}Package created: $ROOT_DIR/$OUTPUT_FILE${nocolor}"
ls -lh "$ROOT_DIR/$OUTPUT_FILE"
