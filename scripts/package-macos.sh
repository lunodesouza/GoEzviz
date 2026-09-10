#!/usr/bin/env bash
# Build GoEzviz.app on macOS using the committed assets/icon.icns.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

if [[ "$(uname -s)" != "Darwin" ]]; then
	echo "package-macos.sh must run on a Mac" >&2
	exit 1
fi

go build -ldflags "-s -w" -o GoEzviz .

app="GoEzviz.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp GoEzviz "$app/Contents/MacOS/GoEzviz"
cp assets/icon.icns "$app/Contents/Resources/icon.icns"

cat > "$app/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleExecutable</key>
	<string>GoEzviz</string>
	<key>CFBundleIconFile</key>
	<string>icon</string>
	<key>CFBundleIdentifier</key>
	<string>local.goezviz</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>GoEzviz</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>1.0.0</string>
	<key>CFBundleVersion</key>
	<string>1</string>
	<key>LSMinimumSystemVersion</key>
	<string>10.13</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSMicrophoneUsageDescription</key>
	<string>GoEzviz uses the microphone to send Talk audio to the camera speaker.</string>
</dict>
</plist>
EOF

# Finder caches icons; touch the bundle so it re-reads icon.icns.
touch "$app"
echo "Created $app — open with: open $app"
