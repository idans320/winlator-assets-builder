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
    git clone --recurse-submodules --depth 1 --branch "$WINLATOR_BRANCH" "$WINLATOR_REPO" "$WORKDIR/winlator" 2>&1
fi

# Clone submodules if not already present
(
    cd "$WORKDIR/winlator"
    if [ ! -f "app/src/main/cpp/adrenotools/CMakeLists.txt" ]; then
        git submodule update --init --recursive 2>&1 || true
    fi
)

WINLATOR_VERSION=$(cd "$WORKDIR/winlator" && git rev-parse --short HEAD)
echo -e "${green}Winlator version: ${WINLATOR_VERSION}${nocolor}"

BUILD_DIR="$WORKDIR/winlator"

echo "Building APK..."
(
    cd "$BUILD_DIR"

    # Nix SDK is read-only — create writable overlay for SDK downloads.
    # Download cmdline-tools if not present, then install platforms via sdkmanager.
    WRITABLE_SDK="$WORKDIR/android-sdk-overlay"
    if [ ! -d "$WRITABLE_SDK/platforms/android-34" ]; then
        mkdir -p "$WRITABLE_SDK/licenses"
        printf '\n8933bad161af4178b1185d1a37fbf41ea5269c55\nd56f5187479451eabf01fb78af6dfcb131a6481e\n24333f8a63b6825ea9c5514f83c2829b004d1fee\n' > "$WRITABLE_SDK/licenses/android-sdk-license"
        # Symlink Nix NDK + platform-tools
        for dir in ndk ndk-bundle platform-tools; do
            [ -e "$ANDROID_HOME/$dir" ] && ln -s "$ANDROID_HOME/$dir" "$WRITABLE_SDK/$dir" 2>/dev/null
        done
        # Download cmdline-tools if not available
        CMDSDK="$WRITABLE_SDK/cmdline-tools/latest"
        if [ ! -f "$CMDSDK/bin/sdkmanager" ]; then
            echo "Downloading Android cmdline-tools..."
            mkdir -p "$CMDSDK"
            curl -sL "https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip" -o /tmp/cmdline.zip
            unzip -qo /tmp/cmdline.zip -d /tmp/cmdline && mv /tmp/cmdline/cmdline-tools/* "$CMDSDK/" && rm -rf /tmp/cmdline /tmp/cmdline.zip
        fi
        # Install platform and build-tools
        echo "Installing Android platform 34..."
        yes | "$CMDSDK/bin/sdkmanager" --sdk_root="$WRITABLE_SDK" "platforms;android-34" "build-tools;34.0.0" 2>&1 | tail -3
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

    # Fix corrupted PNGs: some files are GIF/JPEG with .png extension.
    # AAPT2 rejects them. Convert to real PNGs.
    echo "Fixing misnamed image files..."
    find app/src/main/res -name "*.png" -exec file {} \; 2>/dev/null | \
        grep -v "PNG image" | cut -d: -f1 | while read f; do
        case $(file -b "$f") in
            *GIF*)  convert "$f" "${f}.tmp.png" && mv "${f}.tmp.png" "$f" 2>/dev/null ;;
            *JPEG*) convert "$f" PNG:"$f" 2>/dev/null ;;
        esac
    done

    # Fix duplicate layout ID (line 268 collides with line 217)
    sed -i '268s/"@\+id\/settingsLayout"/"@+id\/settingsLayoutContent"/' app/src/main/res/layout/big_picture_activity.xml

    # Build debug APK (fork — skip release lint overhead)

    # Build debug APK (fork, skip release lint overhead)
    chmod +x gradlew
    rm -rf .gradle/configuration-cache
    ./gradlew assembleDebug 2>&1 | tail -20
)

APK_PATH=$(find "$BUILD_DIR/app/build/outputs/apk/debug" -name "*.apk" 2>/dev/null | head -1)

if [ -z "$APK_PATH" ]; then
    echo -e "${red}Build failed: APK not found${nocolor}"
    exit 1
fi

OUTPUT_FILE=$(yq ".${PKG_NAME}.output" "$ROOT_DIR/packages.yml" | sed "s/{version}/$WINLATOR_VERSION/")
cp "$APK_PATH" "$ROOT_DIR/$OUTPUT_FILE"

echo -e "${green}Package created: $ROOT_DIR/$OUTPUT_FILE${nocolor}"
ls -lh "$ROOT_DIR/$OUTPUT_FILE"
