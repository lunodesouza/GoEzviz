#!/usr/bin/env bash
# Regenerate assets/icon.icns from assets/icon.png (run on macOS).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

png="assets/icon.png"
setdir="assets/icon.iconset"
rm -rf "$setdir"
mkdir -p "$setdir"

sips -z 16 16     "$png" --out "$setdir/icon_16x16.png" >/dev/null
sips -z 32 32     "$png" --out "$setdir/icon_16x16@2x.png" >/dev/null
sips -z 32 32     "$png" --out "$setdir/icon_32x32.png" >/dev/null
sips -z 64 64     "$png" --out "$setdir/icon_32x32@2x.png" >/dev/null
sips -z 128 128   "$png" --out "$setdir/icon_128x128.png" >/dev/null
sips -z 256 256   "$png" --out "$setdir/icon_128x128@2x.png" >/dev/null
sips -z 256 256   "$png" --out "$setdir/icon_256x256.png" >/dev/null
sips -z 512 512   "$png" --out "$setdir/icon_256x256@2x.png" >/dev/null
sips -z 512 512   "$png" --out "$setdir/icon_512x512.png" >/dev/null
sips -z 1024 1024 "$png" --out "$setdir/icon_512x512@2x.png" >/dev/null

iconutil -c icns "$setdir" -o assets/icon.icns
rm -rf "$setdir"
echo "Wrote assets/icon.icns — commit assets/icon.icns (and icon.png / icon.ico if changed)"
