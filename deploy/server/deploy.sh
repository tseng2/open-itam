#!/usr/bin/env bash
# itagent 服务端 - Debian VM 上一键构建部署
# 用法: 在仓库根目录的 deploy/server/ 下执行 ./deploy.sh
set -euo pipefail
cd "$(dirname "$0")"

if ! docker info >/dev/null 2>&1; then
  echo "docker 不可用：请先安装 docker（见 README.md），或用 sudo 运行本脚本"
  exit 1
fi

if [ ! -f configs/server.json ]; then
  if [ -f configs/server.json.template ]; then
    echo "缺少 configs/server.json：先  cp configs/server.json.template configs/server.json  再把两个 token 改掉"
  else
    echo "缺少 configs/server.json，参考模板创建，并把两个 token 改掉"
  fi
  exit 1
fi
if grep -q "CHANGE_ME" configs/server.json; then
  echo "configs/server.json 里还有 CHANGE_ME 占位符，先把 install_token / admin_token 改掉"
  exit 1
fi

mkdir -p data
# 容器内进程以 UID 10001 运行，数据目录属主要对上，否则 SQLite 写不进去
sudo chown -R 10001:10001 data 2>/dev/null || chown -R 10001:10001 data || true

echo "=== 构建镜像（首次较慢：拉基础镜像 + npm/go 依赖） ==="
docker compose build

echo "=== 启动 ==="
docker compose up -d

sleep 3
docker compose ps
echo
echo "完成。验证："
echo "  curl -s http://127.0.0.1:8443/api/v1/devices   # 返回 401/未授权 即为正常"
echo "  docker compose logs -f                         # 看日志"
