#!/bin/sh
# Builds distributable installers into dist/:
#   - Casebook.app           (universal macOS app bundle, double-click to run)
#   - Casebook-<ver>.dmg     (drag-to-Applications installer)
#   - Casebook-windows-<ver>.zip
# Run from the repo root: ./package.sh [version]
set -e
VERSION="${1:-dev}"
APP="dist/Casebook.app"

echo "==> building binaries"
mkdir -p dist
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "dist/casebook-macos-arm64" .
GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "dist/casebook-macos-intel" .
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "dist/casebook-windows.exe" .

echo "==> assembling universal macOS binary"
if command -v lipo >/dev/null 2>&1; then
  lipo -create -output "dist/casebook-macos-universal" "dist/casebook-macos-arm64" "dist/casebook-macos-intel"
  MACBIN="dist/casebook-macos-universal"
else
  echo "    (lipo not found — using arm64 binary only)"
  MACBIN="dist/casebook-macos-arm64"
fi

echo "==> building $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$MACBIN" "$APP/Contents/MacOS/casebook"
chmod +x "$APP/Contents/MacOS/casebook"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Casebook</string>
  <key>CFBundleDisplayName</key><string>Casebook</string>
  <key>CFBundleIdentifier</key><string>app.casebook.local</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundleExecutable</key><string>casebook</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>10.15</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

echo "==> building DMG"
DMG="dist/Casebook-$VERSION.dmg"
rm -f "$DMG"
if command -v hdiutil >/dev/null 2>&1; then
  STAGE="$(mktemp -d)"
  cp -R "$APP" "$STAGE/"
  ln -s /Applications "$STAGE/Applications"
  hdiutil create -volname "Casebook" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
  rm -rf "$STAGE"
  echo "    $DMG"
else
  echo "    (hdiutil not found — skipping DMG)"
fi

echo "==> building Windows zip"
WINDIR="$(mktemp -d)/Casebook"
mkdir -p "$WINDIR"
cp "dist/casebook-windows.exe" "$WINDIR/Casebook.exe"
cat > "$WINDIR/READ ME FIRST.txt" <<TXT
Casebook $VERSION

1. Double-click Casebook.exe.
2. Choose a folder for your data when prompted (e.g. Documents\Casebook).
3. Your browser opens to the app. Keep the small window in the background while you use it.
4. To let teammates connect, see the project README (run with the -lan option).

All your data stays in the folder you pick. Nothing is uploaded anywhere.
TXT
( cd "$(dirname "$WINDIR")" && zip -qr -X "$OLDPWD/dist/Casebook-windows-$VERSION.zip" "Casebook" )
rm -rf "$(dirname "$WINDIR")"
echo "    dist/Casebook-windows-$VERSION.zip"

echo "==> cleaning intermediate binaries"
rm -f dist/casebook-macos-arm64 dist/casebook-macos-intel dist/casebook-macos-universal dist/casebook-windows.exe

echo ""
echo "done — dist/ contains:"
ls -lh dist/
