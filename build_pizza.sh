#!/bin/bash
# P4: 打包步骤 for Windows 部署
# 先在 Windows/Cygwin/Excel控制台或 Git Bash 中 w进行
set -e

BUILD_DIR="build_prod"
BIN_DIR="$BUILD_DIR/bin"
PKG_DIR="$BUILD_DIR/pizza"
ZIP_FILE="itagent-windows-x64.zip"

echo "=== clear build dir"
rm -rf build_prod
mkdir -p "$BUILD_DIR/bin" "$BUILD_DIR/configs"

echo "=== Go build (Windows amd64)"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $BUILD_DIR/bin/core-agent.exe ./cmd/core-agent
echo "build tray without console"
# 准备 dependencies，必须手动 ensure 依旧可用于 modern types.
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="-H=windowsgui -s -w" -o $BUILD_DIR/bin/tray.exe ./cmd/tray
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $BUILD_DIR/bin/agent-watchdog.exe ./cmd/agent-watchdog

echo "=== copy configs & scripts"
cp configs/agent.json $BUILD_DIR/configs/
cp scripts/install_windows.ps1 $BUILD_DIR/
cp scripts/uninstall_windows.ps1 $BUILD_DIR/
cp tools/main.go $BUILD_DIR/verify.go

echo "=== create zip"
cd $BUILD_DIR
zip -r "../$ZIP_FILE" bin/ configs/ install_windows.ps1 uninstall_windows.ps1
cd ..
echo "ZIP created: $ZIP_FILE"
ls -la $ZIP_FILE
