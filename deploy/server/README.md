# itagent 服务端部署（Debian 13 VM / VMware ESXi）

把 itagent 服务端（ingest API + SQLite + 管理 UI）以 Docker Compose 方式跑在 Debian 13 虚拟机上。
Win11 开发机只负责打包源码。

## 拓扑

```
Win11 物理机 (agent 测试端) ──► Debian13 VM:8443 (桥接网卡，同网段)
                                 └─ docker compose
                                      └─ itagent-server 容器 (UID 10001, 非 root)
                                           ├─ /app/data       ← 挂载 ./data (SQLite)
                                           └─ /app/configs    ← 挂载 ./configs/server.json (只读)
```

## 网络规划（重要，别跳过）

本项目的 compose 网络**固定使用 `192.168.240.0/24`**，原因是 Docker 默认按 172.17→172.18→… 的顺序抢占 /16 网段，会在内网环境里和真实办公网段（172.20.0.0/16 等）撞车，导致宿主机路由错乱、整机失联——这套环境里撞过一次。

部署时同时建议在 VM 的 `/etc/docker/daemon.json` 加 `default-address-pools`，把 Docker 以后自动建网的池子圈死在安全段（`deploy.sh` 不会自动改这个，属宿主机层面配置）：

```json
{ "default-address-pools": [{ "base": "192.168.248.0/22", "size": 24 }] }
```

## 第 1 步：VM 准备（ESXi 侧）

1. Debian 13 (trixie) 最小化安装即可，不需要桌面
2. VM 网卡选**桥接**（ESSXi 端口组能拿到内网 IP 的那个），装好后 `ip a` 确认拿到与 Win11 同网段地址
3. `sudo apt install -y open-vm-tools`（ESXi 官方推荐，替代 VMware Tools ISO）

## 第 2 步：装 Docker

```bash
sudo apt update && sudo apt install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/debian $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo usermod -aG docker $USER    # 注销重登生效
```

> `download.docker.com` 不通时，换阿里云源：`https://mirrors.aliyun.com/docker-ce/linux/debian`（上面 URL 两处都换）。

**基础镜像拉取慢**：配 registry 加速器，写 `/etc/docker/daemon.json`：

```json
{ "registry-mirrors": ["https://docker.m.daocloud.io"] }
```

`sudo systemctl restart docker` 生效。失效就搜"docker 镜像加速 2026"换当前可用的地址。

## 第 3 步：传源码到 VM

在 **Win11 开发机的 Git Bash** 里：

```bash
cd E:/ai_work/trae/itagent
bash scripts/package_server_src.sh         # 产出 itagent-server-src-*.tar.gz
scp itagent-server-src-*.tar.gz user@<vm-ip>:~/
```

在 **VM** 上：

```bash
mkdir -p ~/itagent && tar -xzf ~/itagent-server-src-*.tar.gz -C ~/itagent
cd ~/itagent/deploy/server
```

（有内网 Git 的话直接 `git clone` 也行。）

## 第 4 步：配置 + 启动

```bash
vi configs/server.json    # 把 CHANGE_ME_install_token / CHANGE_ME_admin_token 改成真实值
./deploy.sh               # 校验 → 构建 → 启动 → 打印状态
```

`deploy.sh` 会拒绝带 `CHANGE_ME` 占位符的配置，并自动把 `./data` 属主改为容器 UID 10001。

## 第 5 步：验收清单

VM 上：

```bash
curl -s http://127.0.0.1:8443/api/v1/devices     # 返回 401 = HTTP 栈 + 鉴权正常
docker compose ps                                 # STATUS 应为 healthy
docker compose logs -f                            # 观察启动日志
```

Win11 侧：

1. 浏览器开 `http://<vm-ip>:8443`，UI 里填 `admin_token` 能进设备列表
2. 改 agent 的 `configs/agent.json`，把服务端地址指向 `http://<vm-ip>:8443`，重装/重启 agent
3. 设备出现在 UI → **注册/心跳/全量上报链路通**
4. **断链演练**：`docker compose stop` → 等 2 个心跳周期（agent spool 应堆积、零丢失）→ `docker compose start` → 确认补传成功且 spool 本地零残留
5. **持久化演练**：`docker compose down && docker compose up -d` → 设备数据还在（SQLite 在 `./data` 卷里）
6. 高可用演练（可选）：把 VM 挂起/断网 3 次以上，验证 agent 主备切换状态机

## 运维速查

```bash
docker compose logs -f            # 日志（json-file, 10MB×3 滚动）
docker compose restart            # 改 configs/server.json 后重启生效
docker compose down               # 停（数据在 ./data，不丢）
docker compose up -d --build      # 代码更新后重建
# 备份：SQLite 开了 WAL 模式，只拷 server.db 会丢最新数据！
# 先停服务（连接关闭时自动 checkpoint 合并），再拷三个文件：
docker compose stop && cp -a data ~/backup/itagent-$(date +%F)/ && docker compose start
```

## 完全无网环境的备选方案

如果 VM 连镜像加速器都够不着：

1. 在 **装了 Docker Desktop 的 Win11** 上同样 `docker compose build`
2. `docker save itagent-server:0.1.0 | gzip > itagent-server.tar.gz`
3. scp 到 VM：`gunzip -c itagent-server.tar.gz | docker load`
4. VM 上只留 `docker-compose.yml` + `configs/` + `data/`，**删掉/注释 compose 里的 build 段**，`docker compose up -d`

## 安全提醒

- `configs/server.json` 两个 token 是管理面钥匙：改强随机值，别把改过的文件提交回 git
- 管理 UI 按架构设计**只允许内网**，公网入口（如果接 NPM 反代）只放 `/api/v1/*`
- VM 侧如有 ufw：`sudo ufw allow 8443/tcp`（最小化安装默认无 ufw，可跳过）
