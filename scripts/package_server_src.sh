#!/usr/bin/env bash
# 在 Win11 开发机（Git Bash）上打包服务端部署源码，scp 到 Debian VM 后构建
# 只包含构建镜像必需的文件，跳过 node_modules / 产物 / 临时目录
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="itagent-server-src-$(date +%Y%m%d-%H%M).tar.gz"

chmod +x deploy/server/deploy.sh 2>/dev/null || true

tar -czvf "$OUT" \
  --exclude=deploy/server/configs/server.json \
  --exclude=deploy/server/data \
  go.mod go.sum \
  configs/server.json \
  cmd internal \
  web/package.json web/package-lock.json web/index.html web/vite.config.js web/src \
  deploy/server

echo "打包完成: $OUT"
echo ""
echo "传到 VM（把 <vm-ip> 换成实际地址）:"
echo "  scp $OUT user@<vm-ip>:~/"
echo "  ssh user@<vm-ip>"
echo "  mkdir -p itagent && tar -xzf ~/$OUT -C itagent"
echo "  cd itagent/deploy/server"
echo "  vi configs/server.json    # 改掉 CHANGE_ME 两个 token"
echo "  ./deploy.sh"
