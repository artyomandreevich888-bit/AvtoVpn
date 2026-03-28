#!/bin/bash
set -e

export ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/27.0.12077973}"
export JAVA_HOME="${JAVA_HOME:-$HOME/Android/jdk-21.0.2}"
export PATH="$JAVA_HOME/bin:$PATH"

TARGETS="${1:-android/arm64}"
AAR="android/app/libs/mobile.aar"

echo "==> Building Go mobile library ($TARGETS)..."
TAGS="with_gvisor,with_utls,with_clash_api,with_quic"
gomobile bind -v -tags "$TAGS" -target="$TARGETS" -androidapi 26 -o "$AAR" ./mobile/
echo "    $(ls -lh "$AAR" | awk '{print $5}') → $AAR"

echo "==> Building APK..."
cd android
./gradlew assembleDebug --no-daemon -q
APK=$(find app/build/outputs/apk/debug -name "*.apk" | head -1)
echo "    $(ls -lh "$APK" | awk '{print $5}') → $APK"
echo ""
echo "Done. Install: adb install $APK"
