#!/bin/sh
# Builds release binaries into dist/. Run from the repo root: ./build.sh [version]
set -e
VERSION="${1:-dev}"
mkdir -p dist
echo "building casebook $VERSION"
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "dist/casebook-macos-arm64" .
GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "dist/casebook-macos-intel" .
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "dist/casebook-windows.exe" .
ls -lh dist/
echo "done"
