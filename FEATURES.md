# IT Agent 功能地图

## ✅ P0 - 服务端基础（已完成）

- [x] 服务端 ingest/register/query API

- [x] SQLite 存储层（Store 抽象接口，支持代码层双数据库平滑迁移 PostgreSQL/MySQL）

- [x] 数据协议（Envelope/Heartbeat/FullPayload/SMART）

- [x] 设备 token 签发与鉴权

## ✅ P1 - Windows Agent 基础采集（已完成）

- [x] 采集器（CPU/内存/磁盘/GPU/网卡/软件/SMART），平台抽象支持 macOS

- [x] Device ID 生成（主板 SN + MAC，垃圾序列号过滤）

- [x] 本地 spool 队列（成功即删零残留）

- [x] 主备服务器切换 + 自动恢复嗅探

- [x] 心跳 10min + 全量 1h，服务端动态下发间隔

- [x] **AD 与本地账号的登录用户识别（`logon_domain\logon_user`）**

- [x] 品牌主板序列号垃圾值过滤(`Default string` / `To be filled by O.E.M.`等)

## ✅ P2 - 告警与管理界面（已完成）

- [x] 硬件变更引擎：数量增减 + 序列号换件（等数量时才对比）

- [x] SMART 健康规则（整体失败/待定扇区/重映射扇区/寿命≥80%/温度持续超 60℃)

- [x] 变更中心 API（查询未读/已读、ack）

- [x] Vue3 + Element Plus 管理 UI（嵌入二进制，DevOps 式部署）

- [x] 变更事件实时推送 / UI 角标未读计数

## ✅ P3 - 防卸载与终端守护（已完成）

- [x] Windows 托盘（tray.exe 查询状态、询问退出密码后退出）

- [x] Argon2id 密码哈希存储

- [x] IPC（named pipe）与 core-agent 通信

- [x] watchdog 守护（sc.exe 服务拉起）

- [x] 安装脚本（sc.exe create + 计划任务拉托盘）

## 🔄 P4 - 隐蔽性和完整部署体验（接近完成）

- [x] 不列在“控制面板/程序和功能”里（sc.exe 注册服务，不走 MSI，天然满足）

- [x] 卸载脚本需要密码验证（tools/hash.go + Argon2id 比对，已实测走通）

- [x] One-shot 部署 zip（itagent-agent-v0.2.5.zip：bin + configs + scripts + tools，安装脚本强制清空 device_id/token 防身份继承）

- [ ] 无控制托盘版本（unattended，默认推荐）

- [x] 服务器二进制 build（server.exe 一体化：/api + UI 同端口）

- [x] 服务端容器化：Dockerfile 多阶段构建 + docker-compose（Debian 13 VM /opt/itagent 实测部署）

- [x] compose 网段固定 192.168.240.0/24（Docker 默认抢占 172.20.0.0/16 会与办公网冲突，曾导致 VM 整机失联）

- [x] agent 韧性修复：注册 60s 重试直到成功（原先启动失败即永久无 token）；网卡 IPv4/CIDR 采集；托盘运行时间人性化显示

- [x] 双机验证：物理机（cmp001）+ ESXi 虚机（DESKTOP-R6QDB7E）独立身份上链

- [x] 断链演练：服务端停机 → agent spool 堆积零丢失 → 恢复后自动补传、本地零残留

- [x] 重启自愈：ITAgentService 随系统自启 + 托盘登录自启 + 心跳自动恢复

## 🧱 技术债（已知，排期处理）

- [ ] agent 版本号常量未随包迭代（一直上报 0.1.0，UI 无法区分新旧 agent）
- [ ] ingest 收到 401 时应清空本地 device_token 触发重注册（目前靠重启进程）
- [ ] 主机序列号垃圾值（"Default string"）在变更事件里未过滤展示
- [ ] 服务端无数据清理/归档策略（reports 无限增长）
- [ ] 部署 runbook：daemon.json default-address-pools 需人工配置（要重启 docker，影响同机容器）

## 📋 P5 - Mac 支持（计划）

- [ ] macOS 采集器（system\_profiler + smartctl）

- [ ] LaunchDaemon 安装包

- [ ] .pkg 安装包，级别与 Windows 一致

## 📋 P6 - 自动更新与灰度（计划）

- [ ] 服务器响应端驱动 `/api/v1/agent/config`

- [ ] 版本升级 + zip 下载 + sha256 签名校验

- [ ] 灰度规则控制（canary/stable）

- [ ] 自动回滚（10 分钟无心跳切回）

## 📋 P7 - 远程文件/脚本推送（计划）

- [ ] fetch\_file

- [ ] run\_script

- [ ] collect\_file

- [ ] 任务执行 ack 机制

## 📋 P8 - 生态集成（计划）

- [ ] ad-selfservice 集成接口（通知路由此系统）

- [ ] 人↔设备关联图谱接口

- [ ] Snipe-IT 数据导入/同步

- [ ] Webhook 通知到 ad-selfservice 内网 channels

## 📋 P9 - USB 管控（计划）

- [ ] 移动存储审计

- [ ] USBSTOR 策略生成

- [ ] VID/PID 白名单

## 📋 P10 - 高可用与灾难性更新（远期）

- [ ] 备份服务器部署（响应 ingest 转发）

- [ ] 浏览器侧异步查询优化

- [ ] 消息队列/企业微信通知中心

## 当前状态

- **已实现**：P0、P1、P2、P3（数据上报 + 告警 + 排查）；P4 仅剩"无控制托盘版本"
- **测试环境**：服务端跑在 Debian 13 VM（10.1.1.96:8443，docker compose，数据在 /opt/itagent/deploy/server/data）；agent 双机在线（Win11 物理机 + ESXi 虚机）
- **基础设施**：100% 依赖开源 + 拥有 Docker 部署（deploy/server/ 一键部署，含源码打包脚本）
- **代码仓库**：github.com/tseng2/it-agent（main）

## 文档索引

- `docs/architecture.md` — 技术方案设计（架构/协议/流程/安全）
- `FEATURES.md` — 本文件，功能地图与阶段进度
- `deploy/server/README.md` — 服务端部署 runbook（ESXi Debian VM 全流程 + 验收清单）

