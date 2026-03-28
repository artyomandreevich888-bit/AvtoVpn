#!/bin/bash
set -e

export ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/27.0.12077973}"
export JAVA_HOME="${JAVA_HOME:-$HOME/Android/jdk-21.0.2}"
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$PATH"

ROOT="$(cd "$(dirname "$0")" && pwd)"
TARGETS="${1:-android/arm64}"
AAR="$ROOT/android/app/libs/mobile.aar"

# Step 1: Go mobile library
echo "==> [1/3] Building Go mobile library ($TARGETS)..."
TAGS="with_gvisor,with_utls,with_clash_api,with_quic"
gomobile bind -tags "$TAGS" -target="$TARGETS" -androidapi 26 -o "$AAR" ./mobile/
echo "    $(du -h "$AAR" | cut -f1)"

# Step 2: APK (clean build, no cache, no daemon)
echo "==> [2/3] Building APK (clean, no cache)..."
cd "$ROOT/android"
./gradlew clean assembleDebug \
    --no-daemon \
    --no-build-cache \
    --rerun-tasks \
    -q 2>&1
APK=$(find app/build/outputs/apk/debug -name "*.apk" | head -1)
echo "    $(du -h "$APK" | cut -f1) → $APK"

# Step 3: Install if device connected
echo "==> [3/3] Installing..."
if adb devices 2>/dev/null | grep -q "device$"; then
    adb install -r "$APK"
    echo "Done. Launch: adb shell am start -n com.autovpn/.MainActivity"
else
    echo "Done. No device. Install: adb install $APK"
fi
