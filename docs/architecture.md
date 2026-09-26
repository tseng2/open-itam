# IT Agent 技术方案设计文档

**版本**：v1.1
**工作区**：E:\ai_work\trae\itagent
**目标规模**：东莞 536 + 苏州 89 + macOS 47（持续增长）≈ 672 终端，预留 2000 台余量
**适用平台**：Windows 10/11（主）、macOS 12+（次）

---

## 1. 总体架构

```
┌──────────────────────────── Windows / macOS 终端 ────────────────────────────┐
│                                                                              │
│  tray (用户态托盘)          core-agent (系统服务/守护进程)                   │
│  ┌───────────────┐ IPC    ┌──────────────────────────────────┐               │
│  │ 悬停气泡 UI   │◄──────►│ collector   hardware/os/software │               │
│  │ 内网IP/公网IP │        │             /SMART               │               │
│  │ 退出需密码    │        │ reporter    本地队列+断点重传     │               │
│  └───────────────┘        │ watchdog    服务守护/自修复       │               │
│                           │ config      主备server/密钥管理   │               │
│                           └───────────────┬──────────────────┘               │
└───────────────────────────────────────────┼──────────────────────────────────┘
                                            │
                ┌──────────── 内网直连（优先） ───────────┐
                ▼                                         ▼
   ┌──────────────────────┐                ┌──────────────────────────┐
   │ 主服务器 (内网)      │   数据同步     │ 备用服务器               │
   │ agent.it.local:8443  │ ◄───────────── │ NPM反代: agent.xxx.com   │
   │ ├ ingest API         │  (rsync/每晚)  │ (腾讯云香港 + NPM)       │
   │ ├ 变更告警引擎       │                │ 只读 ingest 转发/直写    │
   │ ├ SMART 健康预警     │                └──────────────────────────┘
   │ ├ 超期/失联告警引擎 │ (A4: Ticker 扫描→WebHook + 站内信双通道)
   │ ├ 折旧规则引擎     │ (P0-β: 按规则定时刷资产净值)
   │ ├ 许可到期提醒引擎 │ (阶段五收官: 每小时扫描 ExpiringDays 窗口→站内信)
   │ ├ 软件合规引擎     │ (阶段三: 每小时受控池×终端软件比对→超用站内信)
   │ ├ Web 管理界面       │
   │ ├ Webhook 通知(预留) │ (P7 插件框架/IM 通道)
   │ └ Snipe-IT 连接器    │
   └──────────────────────┘
```

**双服务器策略**
- Agent 配置主备两个地址，默认主（内网）
- 主地址连续失败 3 次（含超时）→ 切备用，切换状态落盘持久
- 备用连通后每 30 分钟探测主服务器一次，恢复即回切
- 备用服务器只做 ingest 转发到主库，非接入高峰不承载 UI

---

## 2. 技术选型

| 层 | 选型 | 说明 |
|---|------|------|
| Agent 语言 | Go 1.22+ | 单文件、跨平台、原生并发 |
| Agent-Windows API | WMI (go-ole / 内置调用) + registry | 采集主力 |
| Agent-macOS API | `system_profiler -json` + `sysctl` + `/Applications` | shell out 封装 |
| 托盘 | getlantern/systray | 跨平台、无外部依赖 |
| 本地队列 | 文件 spool 目录（JSON 文件，成功即删） | 零依赖、断电安全、本地零残留 |
| IPC | Windows: named pipe `\\.\pipe\itagent`；macOS: unix socket `/var/run/itagent.sock` | 托盘 <-> core-agent |
| 服务端框架 | Agent 通道 net/http 标准库 mux + 管理 API Gin（v1，JWT 鉴权） | 双栈并存，见 §7 路由挂载约定 |
| 服务端存储 | GORM + MySQL/MariaDB（生产，AutoMigrate 建表）；SQLite 仅历史开发默认 | 切换记录见 git 历史 |
| 服务端 UI | Vue3 + Element Plus（构建后嵌入 Go 二进制） | 运维友好 |
| 部署 | Windows: 安装脚本 + `sc.exe create`；macOS: .pkg + LaunchDaemon plist | |

### 2.1 数据库平滑迁移设计（SQLite → PostgreSQL / MySQL）

- **代码层**：所有读写走 `internal/server/store` 的 `Store` 接口（RegisterDevice/Authenticate/SaveReport/查询族）。
  SQLite 实现为 `SQLiteStore`；未来新增 `PGStore`（pgx）/ `MySQLStore`（go-sql-driver/mysql），业务代码零改动。
- **约束**：Store 方法只定义业务语义，不暴露 SQL；分页统一 limit/offset；错误统一 `ErrNotFound/ErrUnauthorized`。
- **schema 兼容**：表结构保持三种引擎方言兼容（TEXT/INT/DATETIME 基础类型，无 SQLite 特有函数），迁移工具 `migrate` 子命令按表导出导入。
- **切换方式**：`server.json` 增加 `db_driver: sqlite|postgres|mysql` + `db_dsn`，停机窗口内跑迁移工具 → 改配置 → 重启。
- **迁移触发点**：>1500 台、高并发 UI 查询、或需要与 Snipe-IT/MySQL 报表体系联表时。

---

## 3. 目录结构

```
itagent/
├── cmd/
│   ├── core-agent/          # 后台服务入口
│   │   └── main.go
│   ├── tray/                # 托盘入口
│   │   └── main.go
│   └── server/              # 服务端入口
│       └── main.go
├── internal/
│   ├── agent/
│   │   ├── collector/
│   │   │   ├── collector.go         # 接口定义
│   │   │   ├── hardware_windows.go
│   │   │   ├── hardware_darwin.go
│   │   │   ├── osinfo_windows.go
│   │   │   ├── osinfo_darwin.go
│   │   │   ├── software_windows.go
│   │   │   ├── software_darwin.go
│   │   │   └── smart.go             # SMART 采集
│   │   ├── reporter/
│   │   │   ├── spool.go             # 文件队列（成功即删）
│   │   │   ├── uploader.go          # 上报+主备切换
│   │   │   └── backoff.go           # 指数退避
│   │   ├── watchdog/
│   │   │   ├── service_windows.go
│   │   │   └── daemon_darwin.go
│   │   ├── ipc/
│   │   │   ├── server_windows.go
│   │   │   └── server_darwin.go
│   │   └── config/
│   │       └── config.go            # 配置文件+服务端下发覆盖
│   ├── tray/
│   │   ├── ui.go
│   │   └── password.go
│   ├── server/
│   │   ├── api/
│   │   │   ├── ingest.go
│   │   │   ├── register.go
│   │   │   └── query.go
│   │   ├── alert/
│   │   │   ├── diff.go              # 快照对比（数量/序列号变更）
│   │   │   └── smart.go             # SMART 健康规则
│   │   ├── store/
│   │   │   ├── models.go
│   │   │   └── migrations.go
│   │   ├── connector/
│   │   │   └── snipeit.go           # 预留
│   │   └── ui/
│   └── shared/
│       ├── protocol/
│       │   └── types.go
│       └── crypto/
├── configs/
│   ├── agent.json                   # Agent 默认配置
│   └── server.json                  # 服务端配置（含告警阈值）
├── web/                             # 前端源码
├── scripts/
│   ├── install_windows.ps1
│   ├── install_macos.sh
│   └── uninstall_windows.ps1
├── deploy/
│   ├── server/docker-compose.yml
│   └── agent/
├── docs/
│   └── architecture.md              # 本文档
├── go.mod
└── Taskfile.yml
```

---

## 4. 核心数据协议

### 4.1 设备身份

```
device_id = sha256( motherboard_serial + first_mac )[:16]
```
- Windows `Win32_BaseBoard.SerialNumber`、`Win32_NetworkAdapter.MACAddress`
- macOS `ioreg -rd1 -c IOPlatformExpertDevice` 取 `IOPlatformUUID`
- 首次启动生成后写入本地 `config.dat`，不再变化
- 服务端用 device_id 作为数据主键

**主机序列号采集（多来源容错）**
- `Win32_ComputerSystemProduct.IdentifyingNumber`（首选）+ `Win32_BIOS.SerialNumber`（备选，`bios_serial` 字段）
- 组装机常见垃圾值（`Default string` / `To be filled by O.E.M.` / `System Serial Number` 等）服务端过滤
- macOS：`IOPlatformSerialNumber`

**最后登录用户采集（AD + 本地混合场景）**
- 心跳内携带 `logon`：`logon_user` / `logon_domain` / `logon_type`（`ad`|`local`）/ `logon_at`
- Windows 来源：`Win32_ComputerSystem.UserName`（当前登录者，自带 `DOMAIN\user`）+ LogonUI 注册表（`LastLoggedOnUser`，含已注销者）补 `logon_at`
- `logon_type` 判定：`logon_domain` 命中服务端配置的域名清单 → `ad`，否则 `local`（笔记本未加域场景自动正确）
- macOS：`last` 命令取最后登录，统一 `local`

### 4.2 上报报文（统一格式）

```
POST /api/v1/ingest
Authorization: Bearer <device_token>

{
  "device_id": "a1b2c3d4e5f60708",
  "agent_version": "1.0.0",
  "report_type": "full | heartbeat",
  "reported_at": "2026-09-04T10:23:45+08:00",
  "payload": { ... }
}
```

**heartbeat**：
```json
{
  "hostname": "DG-FIN-0231",
  "os": {"name":"Windows 11 Pro","version":"23H2","build":"22631.4168"},
  "logon": {"logon_user":"zhangsan","logon_domain":"DG","logon_type":"ad","logon_at":"2026-09-04T08:05:11+08:00"},
  "boot_time": "2026-09-04T08:12:00+08:00",
  "uptime_sec": 7865,
  "network": {
    "interfaces": [
      {"name":"以太网","mac":"AA:BB:CC:DD:EE:01","ips":["10.8.12.34/24"],"is_up":true}
    ],
    "public_ip": "120.24.36.18"
  }
}
```

**full**：在 heartbeat 基础上扩展 hardware + software + SMART：

```json
{
  "hardware": {
    "brand": "Dell Inc.",
    "model": "OptiPlex 7010",
    "serial": "7XKQ1P3",
    "bios_serial": "7XKQ1P3",
    "cpu": [{"model":"Intel Core i7-13700","cores":16,"threads":24}],
    "memory_total_mb": 32768,
    "memory_modules": [
      {"slot":"DIMM1","size_mb":16384,"type":"DDR5","speed_mhz":5600,"serial":"A1B2C3"}
    ],
    "disks": [
      {"model":"Samsung SSD 990 PRO 1TB","size_gb":1000,"type":"NVMe","serial":"S6Y4NY0W123456","removable":false}
    ],
    "gpus": [{"model":"NVIDIA RTX 4060","vram_mb":8192}],
    "nics": [{"name":"Intel I225-V","mac":"AA:BB:CC:DD:EE:01","speed_mbps":2500}],
    "disk_smart_health": [
      {"disk_serial":"S6Y4NY0W123456","model":"Samsung SSD 990 PRO 1TB","overall_health":"PASSED",
       "reallocated_sectors":0,"pending_sectors":0,"power_on_hours":12345,
       "temperature_c":41,"percent_lifetime_used":12}
    ]
  },
  "software": [
    {"name":"Microsoft Office LTSC 2024","version":"16.0.14332.20456","install_path":"C:\\Program Files\\Microsoft Office"}
  ]
}
```

### 4.3 服务端响应

```json
{
  "code": 0,
  "message": "ok",
  "server_time": "2026-09-04T10:23:46+08:00",
  "next_heartbeat_sec": 600,
  "next_full_sec": 3600
}
```

- `5xx` / `401` / `403` 触发重试；`200` 才删除本地 spool 文件（**本地零残留**）。
- `next_*_sec` 支持服务端动态调整采集节奏。

### 4.4 采集频率（可配置，非硬编码）

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `heartbeat_interval_sec` | 600（10 分钟） | 心跳上报间隔 |
| `full_interval_sec` | 3600（60 分钟） | 全量采集间隔 |
| `spool_scan_sec` | 30 | 离线队列扫描间隔 |
| `failover_probe_sec` | 1800 | 备用链路下探测主服务器间隔 |
| `offline_threshold_sec`（服务端） | 900（15 分钟） | 资产联系状态的离线判定阈值，容忍一次心跳丢失；A2 失联分层与 A4 超期/失联告警共用 |
| `jwt_secret`（服务端） | 未配置回落内置默认并启动告警 | JWT 签名密钥（任务 B 配置化）：`server.json` 优先、环境变量 `ITAGENT_JWT_SECRET` 兜底；GenerateToken/ParseToken/AuthMiddleware 签名零波及。**secret 变更后存量 token 全失效（401 → 前端跳登录）属预期**，生产必须显式配置 |
| `license_expiring_days`（服务端） | 30 | 软件许可到期提醒窗口（天）：licensealert 引擎每小时扫描「到期日在 (now, now+N] 且未终止」的许可并提醒公司管理员（窗口口径与 licenses 列表 expiring_days 过滤同源） |
| `software_overuse_cooldown_hours`（服务端） | 24 | 软件超用提醒冷却窗口（小时）：softwareaudit 合规引擎每小时比对受控池 × 终端软件安装，同一池项冷却窗内只投一次超用提醒 |

优先级：**服务端下发 > 本地配置文件 > 内置默认**。服务端响应中的 `next_*_sec` 覆盖本地。

### 4.5 注册流程

```
agent: 无 token → POST /api/v1/register
        body: { device_id, hostname, os, agent_version, install_token }
server: 校验 install_token → 签发 device_token 返回
agent: 持久化 device_token，后续上报走 Bearer
```

---

## 5. 变更告警规则（v1.1 修订）

**告警范围（白名单，仅以下进入变更中心）**：
- 磁盘、内存、CPU、GPU、网卡的**数量增减**
- 磁盘/内存的**序列号更换**（同数量换件也能发现）
- 品牌机型号/SN 变更

**不告警（仅采集展示）**：
- SMART 读写次数、通电小时数、温度等动态指标
- 可移动磁盘（U 盘）拔插
- 软件安装明细（量大，先进历史，不告警）

**SMART 健康预警规则（服务端可配置阈值）**：

| 条件 | 级别 |
|------|------|
| `reallocated_sectors > 0` | 警告 |
| `pending_sectors > 0` | 严重 |
| `overall_health != PASSED` | 紧急 |
| `percent_lifetime_used >= 80` | 提示轮换 |
| `temperature_c >= 60`（持续 3 次采样） | 警告 |

Web UI 设备卡片显示磁盘健康红绿灯；变更中心独立"健康预警"页签。

---

## 6. 关键流程

### 6.1 Agent 启动

```mermaid
flowchart TD
    A[服务启动] --> B{首次运行?}
    B -- 是 --> C[生成 device_id 写入 config]
    B -- 否 --> D[加载配置]
    C --> E[用 install_token 注册]
    E --> F[保存 device_token]
    D --> F
    F --> G[启动采集循环]
    G --> H[数据写入 spool 目录]
    H --> I{网络可用?}
    I -- 否 --> J[30s 后重扫 spool]
    I -- 是 --> K[批量上报]
    K --> L{成功?}
    L -- 是 --> M[删除 spool 文件<br>本地零残留]
    L -- 否 --> N{主失败>3次?}
    N -- 否 --> O[指数退避重试主]
    N -- 是 --> P[切备用 NPM 公网]
```

### 6.2 防退出 / 防卸载密码模块化 + 随机验证码卸载

密码归服务端 Web UI 集中管理（安装包不烧入任何密码），两个独立模块各带启用开关：

- **模块配置**：`protection_modules` 表 `{module_key: quit|uninstall, enabled, password_hash}`（Argon2id，永不落明文）
- **下发**：`GET /api/v1/agent/config` 响应携带 `quit_protection` / `uninstall_protection` 字段；Agent 经心跳（≤10 分钟）拉取后按注册表双写模式持久化（HKLM→HKCU 降级）
- **门禁逻辑（QAX 同款）**：模块关闭 → 该操作免验证；开启 → 需密码（本地 hash 校验）；开启但未设密码 → fail-closed 拒绝操作；两模块都关闭 → 全部免验证
- **随机验证码卸载（在线验证）**：防卸载开启时，卸载可输密码**或**在线验证码（二选一）。管理员在 Web UI 生成验证码（`POST /api/v1/protection/uninstall-code`）→ 存库绑定 device_id + 10 分钟过期 + 单次使用 → 转告终端用户；终端经 `POST /api/v1/agent/uninstall-code/verify`（device token）校验通过即标记已用
- **卸载 Go 化**：`core-agent --uninstall`（替代 ps1 卸载脚本，绿盾环境不再被解释器拦截）：验证门禁 → 停看门狗 → 停删服务（SCM）→ 删 tray 计划任务 → 清注册表 → 自删安装目录，全过程写 `data/uninstall.log`
- **离线终端**：防卸载开启时只能用本地密码（在线验证不可达，模型固有特性）

```mermaid
flowchart TD
    A[用户尝试退出 tray] --> B{防退出模块开启?}
    B -- 否 --> G[免验证直接退出]
    B -- 是 --> B2{已设密码?}
    B2 -- 否 --> E2[fail-closed 拒绝]
    B2 -- 是 --> C[密码 Argon2id 校验]
    C --> D{匹配?}
    D -- 否 --> E[拒绝并记录]
    D -- 是 --> G
    H[执行 core-agent --uninstall] --> I{防卸载模块开启?}
    I -- 否 --> K[直接卸载]
    I -- 是 --> I2{凭据: 密码/验证码?}
    I2 -- 密码 --> C
    I2 -- 验证码 --> J2[在线校验 单次/过期/设备绑定]
    J2 -- 通过标记已用 --> K
    J2 -- 拒绝 --> E
    H --> L[停删服务 → 清注册表 → 自删安装目录]
```

### 6.3 数据流

```mermaid
sequenceDiagram
    participant T as Tray
    participant C as Core-Agent
    participant Q as Spool
    participant S as Server
    participant W as Web UI
    C->>C: 采集 hardware/software/smart
    C->>Q: 写入 spool
    Q->>S: POST /api/v1/ingest
    S->>S: diff 快照 + SMART 规则判定
    S->>S: 命中规则 → change_event
    S-->>C: 200 + next_*_sec
    C->>Q: 删除 spool 文件
    W->>S: 查询
    T->>C: IPC get_status
    C-->>T: 内网IP/公网IP
```

## 6.5 边界声明：不要做成准入设备

**Agent 不做 NAC**。你现有链路上已有专用准入/在网行为设备：

```
无线上行 → 信锐 NMC6820（802.1X/Portal/MAC 认证，准入是它的本职）
有线 → 接入交换机 → 核心 → 深信服 AC（在线式，原生阻断）→ 奇安信防火墙
```

正确分工：
- **Agent = 上报健康状态**（杀软/补丁/屏幕锁/硬盘加密/硬件变更）
- **服务端 = 合规评分 + 判定**，坐决策
- **信锐 / 深信服 = 执行点**，拿到决策后把自己的黑名单改了

为什么 Agent 不适合准入：
1. 先有鸡先有蛋：agent 要有网才能上报，如果该拦它已经拦不住了
2. 本地可绕过（本机管理员权限就能撤）
3. 信锐 NMC6820 和深信服 AC 原生能干这事，Agent 去做是重复造轮子

**所以集成的 correct 姿势是**：服务端加 `/api/v1/posture/{device_id}` 供网络设备/深信服拉，或事件 webhook 推给信锐。

---

## 7. 服务端 API 清单

**Agent 通道（device_token / install_token）：**

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| POST | `/api/v1/register` | 设备注册 | install_token |
| POST | `/api/v1/ingest` | 数据上报 | device_token |
| GET | `/api/v1/agent/config` | Agent 拉配置（含防护模块策略） | device_token |
| POST | `/api/v1/agent/uninstall-code/verify` | 卸载验证码在线校验（单次/过期/设备绑定） | device_token |

**管理 API（JWT，gin 引擎）：**

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/login` | 登录签发 JWT |
| GET/POST/GET:id | `/api/v1/companies` | 公司（多租户）：列表/详情登录可读；新增/更新/删除（PUT/DELETE `/{id}`）**仅 admin**——名称全局唯一 409（Unscoped 查重与 DB 唯一索引同口径）、**删除活跃引用拦截 409 带明细**（companyRefTables 表清单单源，**只算活跃行——软删行视为已死不阻塞**，否则「删资产再删公司」被死锁）、零引用才可删且**硬删**（软删会永久占名致同名无法重建） |
| GET/POST | `/api/v1/assets` | 资产列表（company_id/status/asset_tag/off_book + 维度外键 manufacturer_id/model_id/supplier_id/location_id 筛选+分页）/ 建账登记（支持四个维度外键挂接） |
| GET/PUT | `/api/v1/assets/{id}` | 台账明细 / 台账字段维护（含账面规格与维度外键：>0 挂接、0 解除挂接） |
| GET | `/api/v1/assets/{id}/events` | 资产履历时轴 |
| GET | `/api/v1/assets/{id}/versions` | 硬件基线版本列表 |
| POST | `/api/v1/assets/{id}/events/{eid}/approve` | 硬件变更审核（更新基线） |
| GET/POST | `/api/v1/assets/{id}/repairs` | 外寄维修列表 / 送修登记（联动资产状态机） |
| PUT | `/api/v1/assets/{id}/repairs/{rid}` | 维修寄回/结果登记 |
| GET | `/api/v1/assets/export` | 台账 Excel 导出（与列表共用筛选口径，≤5000 行；仅 admin，xlsx 二进制流） |
| GET | `/api/v1/assets/import-template` | 导入模板下载（表头 + 示例行 + 类别/状态下拉；仅 admin） |
| POST | `/api/v1/assets/import` | 台账 Excel 批量导入（multipart：file + company_id 必填 + depreciation_id 可选；仅 admin） |
| GET/POST/PUT:id | `/api/v1/storage-lendings` | 移动存储领用/归还登记（阶段一遗留清欠：department/borrower 模糊 + company_id 过滤+分页；**PUT 指针字段：未传保持、显式 null 清空**；无删除端点；读面登录可读，**写面 POST/PUT 仅 admin**） |
| GET/POST | `/api/v1/part-records` | 配件出入库流水（阶段一遗留清欠：direction/part_type 精确 + asset_tag 模糊过滤+分页；direction 必填 in/out，操作人服务端取 JWT；追加式无编辑删除面；读面登录可读，**POST 仅 admin**） |
| GET | `/api/v1/devices` | 设备列表（net/http 栈，admin_token） |
| GET | `/api/v1/devices/{id}` | 设备详情（同上） |
| GET | `/api/v1/devices/{id}/history` | 上报历史（同上） |
| GET | `/api/v1/changes` | 变更事件列表（同上） |
| POST | `/api/v1/changes/{id}/ack` | 标记已读（同上） |
| GET | `/api/v1/protection/modules` | 防护模块状态（防退出/防卸载，JWT+RoleMiddleware） |
| PUT | `/api/v1/protection/modules/{key}` | 更新模块开关与密码（Argon2id，仅 admin） |
| POST | `/api/v1/protection/uninstall-code` | 生成随机卸载验证码（绑定设备+10 分钟过期+单次使用） |
| GET/POST | `/api/v1/dispatches` | 外派登记列表（company_id/asset_id/status/overdue 过滤+分页）/ 外派登记（仅 admin） |
| POST | `/api/v1/dispatches/{id}/return` | 外派归还：记 returned_at → 状态 20，联动 AssetEvent(dispatch_return) |
| POST | `/api/v1/dispatches/{id}/cancel` | 外派作废（误登记修正）→ 状态 30，不记归还时间 |
| GET/PUT | `/api/v1/webhook-alerts/config` | 超期/失联 Webhook 告警配置（单例：开关/URL/secret/冷却窗口，仅 admin；secret 只回 secret_set） |
| POST | `/api/v1/webhook-alerts/test` | 手动连通性测试：推送 itam.test 载荷（未启用也可测，接收端非 2xx 报 502） |
| GET/POST | `/api/v1/stocktakes` | 盘点任务列表（company_id/status 过滤+分页，带 item_total/item_checked 进度）/ 新建（圈定范围快照明细，仅 admin） |
| GET | `/api/v1/stocktakes/{id}` | 任务详情 + 五段位结果计数（10 待盘/20 正常/30 丢失/40 损坏/50 报废） |
| POST | `/api/v1/stocktakes/{id}/start` | 草稿→盘点中，一次性返回扫码盘点码明文（库中只存 SHA-256） |
| POST | `/api/v1/stocktakes/{id}/finish` | 盘点中→已完成；盘点码即刻失效（未核明细保留为漏盘清单） |
| POST | `/api/v1/stocktakes/{id}/cancel` | 草稿/盘点中→已取消 |
| POST | `/api/v1/stocktakes/{id}/rotate-token` | 重新生成盘点码（泄露止血），旧码即刻失效 |
| GET | `/api/v1/stocktakes/{id}/items` | 盘点明细分页（result/keyword 过滤，预载资产） |
| POST | `/api/v1/stocktakes/{id}/items` | 管理端批量核对/修正（盘点中允许改判重核） |
| POST | `/api/v1/stocktakes/labels` | 渲染资产标签 PDF（按任务明细或手选 asset_tags，≤500 枚；QR 直达移动核对页） |
| GET/POST | `/api/v1/asset-requests` | 设备申请列表（company_id/status/applicant_id/asset_id 过滤+分页；user 角色强制只看自己）/ 提交申请（登录用户；admin 可代录 applicant_id） |
| GET | `/api/v1/asset-requests/{id}` | 申请详情（user 仅限自己的申请，跨人按 404） |
| POST | `/api/v1/asset-requests/{id}/approve` | 审批通过（仅 admin）：**单事务**完成申请流转 + 资产绑定领用人 + 台账 10→20 + AssetEvent(assign) |
| POST | `/api/v1/asset-requests/{id}/reject` | 驳回（仅 admin，记决策人与原因，不动资产） |
| POST | `/api/v1/asset-requests/{id}/cancel` | 撤回（申请人本人或 admin，仅待审批状态） |
| GET | `/api/v1/depreciations` | 折旧规则列表（company_id 必填+分页；**登录用户可读**——资产表单规则下拉依赖） |
| GET | `/api/v1/depreciations/{id}` | 规则详情（company_id 必填，公司边界 404） |
| POST | `/api/v1/depreciations` | 新建规则（仅 admin；stages/months/残值口径校验） |
| PUT | `/api/v1/depreciations/{id}` | 更新规则（仅 admin） |
| DELETE | `/api/v1/depreciations/{id}` | 删除规则（仅 admin；**被资产引用时 409 并返回引用数**，需先解除挂接） |
| POST | `/api/v1/depreciations/recalculate` | 手动重算净值（仅 admin；返回本轮更新资产数，无需等定时任务） |
| POST | `/api/v1/assets/{id}/off-book` | 财务销账转列管（仅 admin；重复销账 409、已报废 400；联动 `off_book` AssetEvent 留痕） |
| POST | `/api/v1/assets/{id}/restore-book` | 恢复在册（仅 admin；未销账 409；联动 `off_book_restore` AssetEvent） |
| GET/POST/PUT/DELETE | `/api/v1/manufacturers` | 厂商维度库（P1）：列表登录可读（keyword+分页，资产表单下拉依赖）/ 新建/更新/删除仅 admin；名称同公司唯一 409；**被资产或型号引用时删除 409 带引用数** |
| GET/POST/PUT/DELETE | `/api/v1/suppliers` | 供应商维度库（P1）：同上（联系人/电话字段；被资产引用时删除 409） |
| GET/POST/PUT/DELETE | `/api/v1/locations` | 位置维度库（P1）：同上（ParentID 树形，父级须同公司、禁自引/成环 400；被资产引用或有子位置时删除 409） |
| GET/POST/PUT/DELETE | `/api/v1/asset-models` | 型号库（P1）：列表另支持 category_id 过滤；类别/厂商/折旧规则外键校验（跨公司 404）；被资产引用时删除 409 |
| GET | `/api/v1/operation-logs` | 操作日志审计列表（P2：company_id 0=全部含全局/user_id/keyword/action/resource/resource_id/start_time+end_time RFC3339 闭区间过滤+分页；**仅 admin**，只读无写面） |
| GET | `/api/v1/notifications` | 我的站内信收件箱（P2 消息中心：unread/type 过滤+分页；user_id 取 JWT 本人，传参无效；company_id 可选 0=不限公司） |
| GET | `/api/v1/notifications/unread-count` | 未读消息计数（铃铛徽标轮询；company_id 可选） |
| POST | `/api/v1/notifications/{id}/read` | 标记单条已读（幂等；跨用户/不存在 404） |
| POST | `/api/v1/notifications/read-all` | 全部已读（仅影响本人未读，返回受影响数） |
| GET/POST | `/api/v1/licenses` | 软件许可列表（P2：company_id 必填 + keyword/expiring_days 到期窗口过滤+分页；登录可读，响应带派生 status 与 used_seats）/ 登记授权（仅 admin） |
| GET/POST/PUT/DELETE | `/api/v1/software-pools` | 受控软件池（阶段三）：列表登录可读（company_id 必填 + keyword 模糊+分页，带挂接许可名富化）/ 增改删仅 admin；名称同公司唯一 409（软删不占名）；license_id 挂接校验同公司 404、0/null 解除挂接，**池项不做席位余量校验**（超用正是引擎要发现的） |
| GET | `/api/v1/software-compliance` | 软件合规报表（阶段三，**仅 admin**，报表中心先例）：company_id 必填 → 汇总卡 + 池项全量（含超用标记）+ 未受控商业软件分页（安装数降序）；比对口径单源 softwareaudit.BuildCompliance |
| GET | `/api/v1/dashboard/summary` | 总览大盘轻聚合（**全员登录可读**，全集团跨公司口径，区别于 admin 单公司的 /reports/summary）：资产状态计数 + changes 未 ack 计数 + 终端活跃/失联三数 + Agent 版本分布 Top5 |
| GET/PUT/DELETE | `/api/v1/licenses/{id}` | 授权详情 / 更新 / 删除（仅 admin；**席位被资产挂接时删除 409 带计数**） |
| GET/POST | `/api/v1/consumables` | 耗材列表（P2：keyword/low_stock 库存预警过滤+分页，登录可读，带 low_stock 派生标记）/ 新增耗材（仅 admin；**建账库存恒 0**） |
| PUT/DELETE | `/api/v1/consumables/{id}` | 编辑元数据（名称/规格/单位/预警线；**库存不经编辑面**）/ 删除（仅 admin；**有出入库流水时 409 带计数**） |
| POST | `/api/v1/consumables/{id}/stock-in` | 入库流水（仅 admin；quantity>0，操作人快照取 JWT；原子加库存） |
| POST | `/api/v1/consumables/{id}/stock-out` | 出库领用（仅 admin；quantity>0 服务端换算负增量；**击穿零库存 409**） |
| POST | `/api/v1/consumables/{id}/adjust` | 库存调整（仅 admin；带符号 delta 非零，盘盈盘亏） |
| GET | `/api/v1/consumables/{id}/txns` | 出入库流水台账（type 过滤+分页，追加式不可变，登录可读） |
| GET | `/api/v1/portal/summary` | 员工自助门户个人统计四卡（P2：我的设备数/待审批申请/持有天数/30 天内到期归期；JWT 本人收口） |
| GET | `/api/v1/portal/my-assets` | 我的设备（使用中+维修中，报废不计；带维度富化；传 user_id 无效） |
| GET | `/api/v1/portal/my-requests` | 我的申请（全部状态；传 applicant_id 无效） |
| GET | `/api/v1/reports/summary` | 报表汇总 8 卡（P2：总数/在用/库存/维修/报废/列管计数 + 在册口径原值/净值合计；**仅 admin**） |
| GET | `/api/v1/reports/annual-value` | 年度资产价值（years 窗口默认 6；近 N 年购入聚合，空年零填充，在册口径） |
| GET | `/api/v1/reports/monthly-trend` | 月度建账趋势（months 窗口默认 12；空月零填充，建账口径含报废） |
| GET | `/api/v1/reports/distribution` | 资产分布环图（dimension=status 默认 / category；label+count+percent，计数降序） |

**免登录公开面（阶段五 P0-β 移动扫码，无 JWT）：**

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| GET | `/api/public/stocktakes/{token}` | 盘点任务概要（名称 + 结果计数） | 扫码令牌 + IP 限流 |
| GET | `/api/public/stocktakes/{token}/assets/{tag}` | 扫码资产白名单视图 + 本次明细状态（范围外只回 tag） | 同上 |
| POST | `/api/public/stocktakes/{token}/check` | 批量核对（≤100 条，scanned_by 必填；只按 asset_tag 定位） | 同上 |

> Snipe-IT 同步接口已按 implementation_plan.md 的 Deprecations 作废移除。

**外派登记契约（阶段五 A1）**：`asset_dispatches` 表 `{company_id, asset_id, borrower_name, destination, dispatched_at, expected_return_at, returned_at, isolation_offline, expect_wipe, status, remark}`；状态机 `10 外派中 → 20 已归还 / 30 已作废`；**一个资产同时仅允许一条 status=10 记录**（重复登记返回 409）；**超期为计算属性**（status=10 且 now > expected_return_at），不设独立状态位；`isolation_offline` 是保密现场"预期内离线"依据（A2 失联分层），`expect_wipe` 标记涉密客户格式化归还要求；创建/归还动作联动 AssetEvent 留痕（event_type：`dispatch` / `dispatch_return`，Title 汇总目的地与归期）；所有读写带 company_id 公司边界（跨公司按 404 处理）。

**联系状态分层契约（阶段五 A2）**：资产列表响应带 `presence` 计算字段（不落库，`gorm:"-"`），由服务端按 **外派登记 × LastSeenAt × 离线阈值** 实时计算，前端只做渲染映射。五态判定顺序即优先级：`overdue 超期未归(高危)`（外派中且已过预计归期，催归优先于存活确认）→ `dispatch_offline 外派离线(预期内)`（外派中+IsolationOffline+未超期且离线，免告警）→ `missing 疑似失联`（无豁免且离线，保守报警）→ `roaming 漫游中`（在线且 PublicIP 非空粗判）→ `online 在线`。无 Agent 终端（Device 为空）不参与判定，presence 留空；外派未标隔离而离线归入疑似失联（管理员应核实或补登隔离标记）；判定核心为纯函数 `model.ResolvePresence`（A4 Webhook 扫描可复用）；离线阈值读 `offline_threshold_sec` 配置，禁止硬编码。

**硬件 Diff 自动比对契约（阶段五 A3）**：ingest full 上报时经 `syncToAssetLedger` 与 AssetVersion 当前基线快照比对，核心为纯函数 `api.compareHardware`（固定顺序输出变更描述）：内存总量（GB）→ 内置磁盘数量 → 磁盘序列号集合差（换盘检测：数量/容量相同仅 SN 变化的偷换场景；任一侧存在空 SN 视为采集不完整，跳过本轮 SN 比对，宁漏报不误报）→ CPU 数量 → CPU 型号集合差。**可移动介质（U 盘等 Removable）不参与比对**（日常插拔非硬件变更）。检测到变更且无待审核事件时自动生成 `hardware_change` AssetEvent（`ReviewStatus=20 待审核`，Description 含变更详情与当前快照）；**幂等语义**：待审核事件存在期间同一资产保持单条，管理员审核通过后基线更新、后续变更才会再次检测。审核流复用现有 `/api/v1/assets/{id}/events/{eid}/approve`。

**超期/失联 Webhook 告警契约（阶段五 A4，P0-α 收官；阶段五收官扩站内信双通道）**：告警配置存 DB 单例表 `webhook_alert_config`（`enabled` / `webhook_url` / `secret` / `cooldown_minutes` 冷却窗口，Web UI 配置，**严禁塞 server.json**）；冷却状态表 `webhook_alert_states` 按 `(company_id, asset_id, alert_type)` 唯一键记录最近推送时间（进程重启不丢）。服务端 goroutine + time.Ticker（默认每分钟，**启动即扫一轮**——重启后未处理告警立即补投不空等）定时扫描资产台账：**联系状态判定复用 `model.ResolveAssetPresence`（核心即 `ResolvePresence` 纯函数，离线阈值同 `offline_threshold_sec`，引擎/列表富化禁止各自重复实现）**，产出 `overdue`（外派中且过预计归期，带负责人/目的地/预计归期上下文）与 `missing`（无外派豁免且心跳超阈值，带最近心跳）两类告警，批量合并为单次 POST（`event: itam.alert`）；**secret 非空时携带 `X-ITAM-Signature` 头 = hex(HMAC-SHA256(secret, 原始请求体))**，接收端重算即可校验来源与完整性；出站 HTTP 强制超时（10s），接收端非 2xx 视为失败。**冷却去重**：同资产同类型在冷却窗口内不重复推送（默认 60 分钟，可配），期满仍未处理再次提醒；类型升级（missing→overdue）独立计窗；推送失败不落冷却状态、下一轮自动重试。配置与测试端点走 gin `/api/v1/webhook-alerts/*`（JWT + RoleMiddleware("admin")），secret 永不回传只回 `secret_set`；新增该前缀时已同步双层路由挂载表。**站内信双通道（阶段五收官）**：引擎另带站内信通道，**独立于 WebHook 开关**（系统内通道，WebHook 未配置也该收到）——判定同源（同一轮 BuildAlerts 产物），去重复用 `webhook_alert_states` 同表但用 `notify_overdue` / `notify_missing` 独立键（与 WebHook 通道互不干扰、各自计窗）；投递器经 `webhook.AlertNotifier` 函数注入（main 组装 `v1.NewAlertNotifier` 闭包扇出公司管理员——**api/v1 已 import webhook 包，引擎反向 import 会循环依赖，注入是唯一方向**）；单条投递失败不落冷却状态（下轮重试）也绝不拖垮 WebHook 通道（旁路语义）；冷却窗口时长与 WebHook 通道共用 `cooldown_minutes` 配置。

**盘点任务契约（阶段五 P0-β，CIYO 对标）**：`stocktakes`（任务：`name` / `status` / `started_at` / `finished_at` / `created_by` / `scan_token_hash`）+ `stocktake_items`（明细快照：`asset_id` / `asset_tag` / `expected_location` / `expected_status` / `actual_location` / `result` / `scanned_by` / `scanned_at`，`(stocktake_id, asset_id)` 唯一）。任务状态机 `10 草稿 → 20 盘点中 → 30 已完成 / 40 已取消`（取消对草稿/盘点中开放，完成仅对盘点中）；明细状态机 `10 待盘 → 20 正常 / 30 丢失 / 40 损坏 / 50 报废`（盘点中允许改判重核，最后写入生效；结束后只读）。**圈定范围在创建时一次性快照**（管理端显式 asset_ids 或按 category/status/location/department 过滤，已报废资产不入范围），后续台账变动不影响本任务明细。**扫码安全模型（项目首个非 JWT 面）**：开始任务时生成 128-bit 随机盘点码，**库中只存 SHA-256 哈希，明文仅 start/rotate 时一次性返回**（无法找回，"查看"即轮换）；令牌有效期 = 任务处于盘点中（finish/cancel 即失效，rotate 止血）；公开面按 IP 固定窗口限流（默认 120 次/分钟），批量核对 ≤100 条、只按 asset_tag 定位；资产详情仅白名单字段（编码/类别/规格/序列号/位置/负责人），价格/备注等台账敏感信息与范围外资产信息一律不下发。**履历联动**：异常结果（丢失/损坏/报废）逐资产记 `stocktake` AssetEvent（ReviewStatus=10）；**"正常"结果自动确认该资产 pending 的 `hardware_change` 待审事件（A3 协同：盘点即天然的人工确认）**。标签 PDF：`internal/server/label` 纯 Go 渲染（go-pdf/fpdf + boombuler/barcode，零 CGO；fpdf 核心字体仅 Latin-1，**CJK 字段不入标签**——识别靠资产编码/SN/QR，中文详情在扫码后的移动页展示），A4 3×7 65×35mm，QR 内容 `${origin}/#/a/${asset_tag}`（base_url 由前端注入，二维码指向免登录移动路由）。移动页三条 hash 路由共用一个 Vue 视图：`/m/t/:token`（换码入口）→ `/m/scan`（扫码台）→ `/a/:number`（标签直达核对），盘点码本地记忆直至失效。

**设备申请审批契约（阶段五 P0-β，CIYO 对标）**：`asset_requests`（`company_id` / `asset_id` / `applicant_id` + `applicant_name` 姓名快照 / `is_long_term` / `expected_return_at` / `reason` / `status` / `approved_by` / `approved_at` / `decision_remark`）。状态机 `10 待审批 → 20 已通过 / 30 已驳回 / 40 已取消`；**申请提交即指定目标资产**（仅库存中且未被领用可申请；权威校验在审批事务内），同一申请人对同一资产仅一条待审批（409），不同申请人竞争同一资产放行、由审批人裁决；短期借用必须带归期（长期领用归期强制清空）。**审批+绑定同事务（CIYO 精髓）**：approve 在单个 GORM 事务内完成「申请 pending→approved（条件更新）+ 资产绑定领用人（条件：status=10 且 user_id IS NULL）+ 台账状态 10→20 + `assign` AssetEvent（TargetPerson=申请人、ReturnDate=短期借用归期、OperatorID=审批人）」；**资产被抢先领用则整单回滚返回 409，竞争失败方保持 pending**。越权收口：user 角色列表/详情强制限定自己的申请（传 applicant_id 也无效，跨人跨公司统一 404）；审批/驳回仅 admin 与超管；管理员可代录（applicant_id 指定 + 姓名快照取自用户表，校验申请人属同公司）。

**折旧规则引擎契约（阶段五 P0-β，CIYO 对标）**：`depreciations` 表（`company_id` / `name` / `months` 总折旧月数 / `floor_type`（`amount` 固定金额｜`percent` 残值率）/ `floor_val` / `stages` 阶梯 JSON / `enabled` / `remark`）；资产经 `assets.depreciation_id` 挂接（NULL = 不参与自动折旧，净值人工维护）。**阶梯语义**：`stages` 为 `[{period, unit(MONTH/YEAR), ratio}]` 数组，按购置时长顺序分段消费——前一段走完才进入下一段，段内按整月线性折算，ratio 为该段累计折旧比例（各段之和 ≤ 1）；配置阶梯时 months 必须等于阶梯覆盖月数（入口校验）；**stages 为空则按 months 直线折旧**。净值 = 原值 ×（1 - 折旧比例），再套残值下限（amount 绝对额 / percent 原值×残值率），四舍五入到分；amount 残值误配大于原值时以原值封顶。计算核心收口在 `internal/server/depreciation` 纯函数包（引擎扫描与 API 校验共用，禁止在模型外复制口径）。**引擎**：goroutine + Ticker（默认每小时）扫全部启用规则 → 加载挂接资产 → 差异 ≥1 分才写库（幂等空转不写）；**停用规则/悬空引用/跨公司引用/缺购入日期/原值非正一律冻结现值不动**；手动重算端点与引擎共用 ScanOnce。**列管资产（财务维度，与运营状态正交）**：`assets.off_book` + `off_book_at`——折旧完且财务销账的资产转"列管"继续跟踪使用，走完报废流程变卖才离场；**严禁把列管塞进 Asset.Status 状态机**（盘点圈定/审批流都依赖现有状态语义），展示层以派生标签渲染（off_book && 未报废）+ 列表 off_book 筛选；销账/恢复动作联动 `off_book` / `off_book_restore` AssetEvent（含发生时净值快照），重复销账/未销账恢复均 409，已报废资产不可销账（400）。模型层注意：`enabled` 列不能加 GORM `default:true` 标签——bool 零值 false（停用）是合法值，带 default 标签时 Create 会把零值替换成列默认值，停用规则永远建不出来。

**台账 Excel 批量导入导出契约（P1 首项）**：xlsx 读写收口在 `internal/server/assetexcel` 纯函数包（excelize v2，纯 Go 零 CGO；行级校验/表头映射/模板生成，gin API 只做装配与落库）。**导入**：multipart 上传（`file` + `company_id` 必填 + `depreciation_id` 可选）；**按表头名定位列**（列序无关、未知表头忽略——导出文件含领用人/公司富化列可直接回传再导入）；解析用 RawCellValue 读原始单元格值（真日期单元格是 Excel 序列号：文本布局优先、序列号兜底并限 1982–2100 区间防把金额误判为日期；货币千分位自动清理）；类别必填，映射收口 `model.AssetCategoryIDByName`（中文名/常见别名精确匹配，落库规范名）；状态中文/数字段位都收、空值默认库存中。**行级校验全量拒收**（HTTP 400 + `code 40006` + `data.errors` 行级明细）：空编码、文件内重复、无法识别的类别/状态/日期/金额，以及 **DB 已占用编码——asset_tag 为全局唯一索引且软删除记录仍占位，查重必须 Unscoped**（含回收站，返回"已存在"）。**净值优先级：Excel 账面净值 > 折旧规则即时计算 > 0**；挂接 depreciation_id 后折旧引擎每小时兜底重刷（导入即闭环）；落库为单事务批量建账 + 每资产一条 `create` AssetEvent（OperatorID 取 JWT 操作人），并发撞码由 DB 唯一索引兜底整体回滚。**上限**：导入 2000 行 / 5MB，导出 5000 行（超限提示缩小范围分批）。**导出**：与列表页共用 `applyAssetListFilter` 过滤口径（所见即所得）；日期写 `yyyy-MM-dd` 文本、类别取规范名、加密软件写 是/否（与导入解析同一映射，回导闭环成立）。**模板**：表头 + 示例行（备注提示删除）+ 类别/状态下拉 data validation。路由挂载：三个端点挂 `/api/v1/assets` 已有前缀（同前缀静态段与 `/{id}` 参数段共存 gin 允许），**无需新增挂载表条目**。

**维度治理契约（P1，CIYO 对标）**：四张维表 `manufacturers`（厂商）/ `suppliers`（供应商：contact_name / phone）/ `locations`（位置库：parent_id 树形，NULL=顶级）/ `asset_models`（型号库：category_id 复用 AssetCategory 段位且 0=不限、manufacturer_id / depreciation_id 可空外键、eol_months 0=不限）——全部公司维度实体，**名称同公司唯一（store 层校验，软删除记录不占名可重建；禁用 DB 唯一索引——asset_tag 软删占位同坑）**，名称 trim 后落库。**台账外键化**：`assets` 增 `manufacturer_id` / `model_id` / `supplier_id` / `location_id` 四个可空外键；原 `brand` / `model_name` / `location` 自由文本列**保留作落库快照**（Excel 导入导出、标签 PDF、履历零回归），挂接外键时服务端把维度名写回快照列。**展示口径单源**：`enrichAssetsDimensions` 批量 IN 富化（防 N+1），列表 / 详情 / Excel 导出三处共用——外键存在时以维表名覆盖响应展示（**维度重命名可传播**），供应商回填 `supplier_name` 富化字段（`gorm:"-"`）；富化只改响应不改库。**挂接语义**：建账传 null/0 = 不挂接，编辑传 0 = 解除挂接（快照文本保留）；外键校验存在 + 同公司（404），型号类别与资产类别不匹配 400（按"本次生效类别"校验，类别可同请求变更）；**建账未显式选厂商时随型号带出；未显式挂折旧规则时继承型号库预挂规则**（编辑不自动改）。**删除引用拦截在 API 层**（需查 assets 表，SQLiteStore 测试库无该表）：厂商被资产/型号引用 409、供应商/型号被资产引用 409、位置被资产引用或有子位置 409，均带引用数；位置父级校验：同公司（404）、禁自引与成环（400，沿父链上溯 ≤100 步）。**Excel 导入不设维度外键**（自由文本建账，导入后按需在台账逐笔挂接治理），导出则经富化呈现治理后口径（回导闭环仍成立——富化值与快照一致性由挂接写回保证）。路由挂载表已补四个前缀。

**路由挂载约定（易踩坑）**：服务端是双层路由——外层 `net/http` ServeMux 负责 Agent 通道，并按**硬编码前缀**把管理 API 转给内层 gin 引擎（`internal/server/api/server.go` 的 `NewHandler`）。新增一类 gin 资源路由时，除了在 `api/router.go` 注册，还必须在该前缀挂载表中补一行 `/api/v1/<resource>`，否则外层 mux 会直接返回 404，且构建、单测都不会报错，只能部署后才会暴露。

**操作日志契约（P2 体验运营首项）**：`operation_logs` 追加式审计表（`company_id`（0=全局面操作）/ `user_id` + `username`/`role` 操作人快照 / `action` / `resource` / `resource_id` / `path` 原始路径兜底 / `detail` 请求体摘要 / `ip` / `user_agent` / `status` HTTP 状态码 / `created_at`）——**不挂 BaseModel 软删除语义，不提供任何修改/删除面（审计流水不可变）**。**写入有两条路径**：① gin 审计中间件 `middleware.AuditLog`（挂在 AuthMiddleware 之后、全部受保护路由）对变更类方法（POST/PUT/DELETE/PATCH）在业务完成后留痕——操作人取 JWT 上下文；**动作/对象派生规则**（`ParseAuditRequest` 纯函数）：路径尾段为数字 → 方法缺省动作（POST=create / PUT=update / DELETE=delete），尾段非数字 → 尾段即动作词（`/dispatches/5/return` → `return`、`/assets/import` → `import`），动作段前一段为数字即对象 ID；多级子资源的 ID 语义歧义以 `path` 原文兜底。② 登录是公开面不经中间件，在登录处理器内单独留痕：成功记 `login`（操作人取用户表）、失败（密码错/被禁用/未设密码）记 `login_failed`（能定位到用户则带身份，查无此人 user_id/company_id 记 0）。**公司归属**：query `company_id` 优先、JSON body `company_id` 兜底、皆无记 0（全局配置类操作）。**请求体采集防护**：仅 Content-Length 明确且 ≤64KB 的 JSON 报文预读（读后复位，业务 handler 无感），明细截断 2048 字节；multipart（Excel 导入等）与超限/分块未知长度报文一律不动流、明细留空——审计绝不改变业务行为。**审计旁路语义**：落库失败只经 `c.Error` 上报 gin 错误链，绝不阻塞业务响应；越权失败尝试（403）同样留痕（安全审计信号）。**时间口径**：审计 `created_at` 与过滤参数统一 UTC（glebarez SQLite 按带时区偏移的文本存取时间，写入本地/查询 UTC 会让 SQL 文本比较错位；MariaDB 驱动统一转换无此问题，但双实现必须同一口径）；列宽防御在 store 边界截断超宽字段（MariaDB 严格模式会因超宽丢弃整条审计）。**查询面**：`GET /api/v1/operation-logs` 仅 admin（RoleMiddleware），company_id 0/缺省 = 全部（含全局行）；无 retention 清理策略（表量级=管理操作频次，暂不构成风险，后续报表中心再评估归档）。

**消息中心契约（P2 体验运营第二项：站内信起步；阶段五收官扩三类事件源）**：`notifications` 表（`company_id` / `user_id` 收件人 / `type` 通知类型 / `title` / `content` / `resource` + `resource_id` 跳转锚点 / `read_at` NULL=未读）。**收件箱安全边界是 user_id**（JWT 本人收口，传参无效；company_id 可选，0 = 不限公司——顶栏铃铛无公司上下文，跨公司用户自己的通知全可见）。**已读语义**：新增通知一律未读（store 归一 ReadAt=nil，已读态只能经标记动作产生）；单条已读幂等（重复标记放行——铃铛轮询与点击天然并发）；全部已读返回受影响数；跨用户/不存在一律 404。**事件源接线是业务旁路**（沿审计旁路先例）：通知投递失败只记日志/经 `c.Error` 上报 gin 错误链，绝不阻塞业务主流程。事件源五类：① 设备申请审批流——提交 → 扇出通知公司全部在册管理员（admin + super_admin，`notifyCompanyAdmins`），通过/驳回 → 回执申请人（文案带资产编码，富化查询失败回落"资产 #ID"，通知不因文案富化失败而丢失）；② A4 告警联动（`asset_alert`：超期未归/疑似失联 → 公司管理员，`v1.NewAlertNotifier` 注入 webhook 引擎，**独立于 WebHook 开关**，去重 `notify_overdue`/`notify_missing` 独立键，详见 A4 契约段）；③ 耗材低库存（`consumable_low_stock`）——**沿触发防轰炸**：只有「旧库存 > 预警线 且 新库存 ≤ 预警线」的那笔流水才投（旧库存由「新库存 - 增量」回推免加读），库存持续低位不重复轰炸、回补后再次击穿才会再投；未配预警线（0）不预警（与列表 low_stock 口径一致）；落点在 consumable postTxn 旁路（流水成功后投递，失败不影响响应）；④ 软件许可到期（`license_expiring`）——`internal/server/licensealert` 引擎每小时扫描（**启动即扫一轮**），窗口口径复用 `ListLicenses` ExpiringDays 过滤（(now, now+N] 且未终止，勿重写）；**去重复用 `webhook_alert_states` 同表**：键 `(company, license_id, 'license_expiring')`（asset_id 列存许可 ID），冷却 = 窗口天数——许可在窗口内至多停留 N 天，天然只提醒一次，续期后再次进入窗口冷却已过期会重新提醒；投递器经 `licensealert.Notifier` 函数注入（main 组装 `v1.NewLicenseExpiringNotifier` 闭包），单条失败不落冷却状态、下一轮重试。引擎场景的扇出走 `notifyCompanyAdminsCtx`（无 gin 上下文版），查不到管理员返回 0（无人可通知是数据状态而非故障）。⑤ 软件超用（`software_overuse`，阶段三合规引擎）——softwareaudit 引擎每小时比对受控池 × 终端软件安装，**超用池项**（挂接许可且安装终端数 > 席位）经 `software_overuse` 独立冷却键（默认 24h，server.json `software_overuse_cooldown_hours` 可配，asset_id 列存池项 ID）提醒公司管理员，resource=`software` 跳软件与授权许可页；**未受控商业软件清单只在合规报表呈现、不投通知**（清单量大易轰炸，管理员在合规视图甄别）。**Web 端**：顶栏铃铛（未读徽标 30s 轮询同预警节奏，max 99）+ popover 收件箱（最近 20 条，点击就地已读并按 resource 跳转，全部已读按钮，**「查看全部」入口**）+ **独立消息中心页 `/notifications`（2026-09-26 落地，全员菜单、我的门户之后）**——五类类型过滤（asset_request 设备申请 / asset_alert 资产告警 / consumable_low_stock 耗材预警 / license_expiring 许可到期 / software_overuse 软件超用）+ 仅看未读开关 + 分页全量收件箱 + 点击就地已读并按 resource 跳转 + 全部已读（带受影响数）；**resource → 路由映射与类型中文映射收口共享模块 `web/src/notifications.js`，铃铛与独立页两处共用、禁止复制两份**。

**软件许可契约（P2 体验运营，CIYO 对标）**：`licenses` 表（`company_id` / `name` 软件名称 / `vendor` 厂商 / `category` 分类 / `license_key` 授权密钥 / `total_seats` 席位总数（0=不限）/ `purchase_date` / `expiration_date` 到期日（NULL=永久授权）/ `termination_date` 合同终止日 / `remark`）。**状态不落库**——由日期经 `model.ResolveLicenseStatus` 实时派生（与外派"超期是计算属性"同口径）：终止日非空优先（合同终止盖过一切）→ 到期日已过 → 在用；改日期即改状态，杜绝脏数据。**已用席位也不落库**——席位分配经 `assets.license_id` 强类型外键挂接到具体资产，used_seats 由 API 层一次 GROUP BY 实时计数，**被压缩席位数造成的历史超用不做追溯修正**，由许可页合规度视图呈现（used > total 标红）。**挂接语义与维度外键一致**：建账传 null/0 不占席位、编辑传 0 解除挂接；挂接校验存在+同公司（404），**席位已满 409**（total_seats>0 且当前占用 ≥ 总数；编辑换绑同一许可时排除自身）。**删除拦截在 API 层**：席位仍被资产**或受控软件池**挂接的许可删除 409 带引用数（维度同款；阶段三池项挂接同拦）。**到期提醒窗口**：`expiring_days=N` 只看「到期日在 (now, now+N] 且未终止」的许可（永久授权不进窗口），时间参数 UTC 归一（P2-1 已知坑）；licensealert 引擎每小时按该窗口扫描并提醒公司管理员（server.json `license_expiring_days` 可配，默认 30 天；同表去重与冷却语义见消息中心契约）。许可不参与 Excel 导入导出（台账 Excel 仍走自由文本建账）。**Web 端**：软件与授权许可页（原 mock 页实装）——席位占用进度条（满员黄/超用红）、密钥脱敏展示、到期窗口过滤；资产表单新增许可下拉（带已用/总数），详情抽屉显示许可名。

**耗材管理契约（P2 体验运营，CIYO 对标）**：`consumables` 表（`company_id` / `name` / `spec` 规格型号 / `unit` 计量单位 / `stock` 当前库存 / `min_quantity` 最低库存预警线（0=不预警）/ `remark`）+ `consumable_txns` 流水表（`consumable_id` / `type`（stock_in/stock_out/adjust）/ `delta` 带符号变动量 / `recipient` 领用人 / `operator_id` + `operator_name` 操作人快照 / `remark` / `created_at`）。**库存只经流水变更**：编辑面 Select 列表不含 stock 列，建账库存恒 0——期初库存走第一笔入库流水，每笔库存变动都有流水对应（账实可追溯）；流水**追加式不可变**（不挂 BaseModel 软删除，无修改/删除面——operation_logs 先例）。**符号语义**：入库恒正、出库恒负（API 接收正数量、服务端换算负增量落库）、调整任意非零（盘盈正/盘亏负），库存恒为「加增量」单一口径。**原子扣减**：`CreateConsumableTxn` 单事务内条件更新 `stock + delta >= 0`，零命中时区分耗材不存在（404，含跨公司）与库存不足（`ErrInsufficient` → 409），并发超卖由条件更新天然拦截、流水不落半截。**名称同公司唯一**（store 层校验，软删不占名可重建，维表同口径）。**预警派生**：low_stock = min_quantity > 0 且 stock ≤ min_quantity（未配预警线的零库存不算预警），列表过滤与响应标记同口径；**低库存站内信沿触发**（postTxn 旁路）见消息中心契约——只有沿触线的那笔流水才提醒公司管理员，持续低位不轰炸。**删除拦截在 API 层**：有出入库流水的耗材删除 409 带流水计数。审计联动：stock-in/stock-out/adjust 由审计中间件按 URL 尾段自动留痕动作。**Web 端**：耗材管理页（资产管理组）——预警行红色标记、出/入/调整对话框、流水抽屉（带领用人/操作人快照）。

**员工自助门户契约（P2 体验运营，CIYO PersonalStatsVO 对标）**：`GET /api/v1/portal/*` 三个只读端点，**全部 JWT 本人收口**（user_id 即安全边界，notifications 先例；传 user_id/applicant_id 一律无效，无公司上下文）。**统计四卡口径**：`device_count` = user_id=本人 且 status∈{20 在用, 30 维修中}（报废不计——维修设备仍归属领用人）；`pending_request_count` = 本人的待审批申请数；`days_in_use` = 本人在册设备最早一次 `assign` 履历距今整日数（无履历记 0——用模型查询而非 MIN 聚合，glebarez 对 MIN() 返回原始字符串、Raw+NullTime 扫描会炸，模型字段的 schema 转换器才能解析时间文本）；`expiring_count` = 已批短期借用中归期落在 (now, now+30d] 的申请数（归还提醒）。**我的设备**带维度富化（license_name/supplier_name 同台账口径）。Web 端：我的门户页（全员可见菜单）——四卡 + 我的设备表 + 我的申请表（跳设备申请页提交新申请）。

**报表中心契约（P2 体验运营，CIYO 对标）**：`GET /api/v1/reports/*` 四个只读端点，**整组 admin-only**（RoleMiddleware 收口在路由组，管理视角数据不下发普通用户）。**口径单源**（`api/v1/report.go` 纯函数 + 一次轻量列拉取后 Go 侧聚合——DB 侧 DATE_FORMAT 是方言，双库不可移植）：① 汇总 8 卡——状态计数为**台账全量口径**（含报废），价值合计与年度价值为**在册口径**（非报废，报废残值出表），列管 = off_book 且未报废（P0-β 正交契约）；② 年度资产价值——近 N 年（默认 6）购入聚合 `{year, count, original, net}`，窗口年降序、空年零填充、窗口外样本丢弃（历史归档口径）；③ 月度建账趋势——近 N 月（默认 12）新建台账数，**建账口径含报废**（反映录入节奏），空月零填充升序，月界按自然月切分；④ 分布环图——dimension=status（标签收口 `model.AssetStatusName`，事实唯一源）/ category（复用 AssetCategoryName），行 `{label, count, percent}` 计数降序（同数按标签字典序稳定），percent 四舍五入整数、总数为零返回空（除零防御）。窗口参数缺省/非法/超大一律收敛到边界（1≤years≤20、1≤months≤36）。**Web 端**：报表中心页——8 卡网格、年度价值双色条（原值蓝/净值绿）、月度趋势 CSS 柱状、SVG 环图（stroke-dasharray 分段）+ 图例，公司维度切换。

**移动存储领用 / 配件出入库契约（阶段一遗留清欠，2026-09-26 纯 Web 收官）**：两张阶段一表此前只有模型与 API、无管理页面。`storage_lendings`（`company_id` / `department` / `borrower` 必填 / `borrow_date` / `brand` / `spec` / `device_code` / `quantity` 缺省 1 / `return_date` / `return_qty` / `sec_certified` 加密认证（通用标记，**产品名不落码**——公司更换加密系统零改动）/ `remark`）——**无删除端点**（误登记走编辑修正），列表 department/borrower 模糊 + company_id（缺省不限）过滤。**PUT 指针字段契约：未传=保持原值、显式 null=清空、有值=更新**——原阶段一实现把未传与显式 null 都归为保持（Go 指针反序列化后两者都是 nil，无法区分），日期编辑清空会静默失败；2026-09-26 以原始键集合判定修复（`assignStorageUpdate` 泛型助手）并用契约测试锁死（storage_lending_api_test.go：未传保持/显式 null 清空/归还回填/404）。**前端编辑对话框必须全量送字段**：清空的日期显式送 null，漏字段即语义退化为保持；归还能力走 PUT 回填 return_date/return_qty（只送这两键，其余保持）。`part_records`（`direction` 必填 `in`/`out`（常量 `model.PartDirectionIn/Out`）/ `operated_at` 缺省服务端 now / `part_type` 必填 / `part_name` / `part_model` / `brand` / `quantity` 缺省 1 / `unit` / `locker_location` IT 储物柜 / `purpose` / `oa_number` / `location` / `asset_tag` / `operator_id`）——**追加式流水无编辑删除面**；**操作人服务端取 JWT**（body 传 operator 无效）；**asset_tag 仅文本关联无外键校验**（台账编码自由填写）；列表 direction/part_type 精确 + asset_tag 模糊 + company_id 过滤；operator 富化可空（渲染 real_name/username 回落，用户软删/未绑定显示 —）。**写面已收口 admin（2026-09-26 RBAC 前后端同步收口）**：storage-lendings POST/PUT 与 part-records POST 挂 RoleMiddleware("admin")（dimension 先例同前缀读/写两组注册），读面保持登录可读；前端登记/编辑/归还/登记流水按钮同步 isAdmin gate——两 API 此前挂 protected 无角色收口属阶段一遗留越权面，本次按「前后端必须同步改」契约一并落地，user 角色写面 403 有测试锁死。**Web 端**：移动存储领用页 `/storage-lendings`（资产管理组，外派出差终端之后）——登记 / 编辑（全量 PUT + 日期 null 清空）/ 归还对话框；配件出入库页 `/part-records`（同组其后）——登记流水（方向/类型必填，操作人自动记录）+ 四条件过滤。

**软件合规比对契约（阶段三，2026-09-26 落地；CIYO/Snipe-IT 均无此能力，自研差异化）**：口径为 implementation_plan「软件与授权管理」——受控软件库 × Agent 采集软件比对，产出合规报表（识别超用与未受控商业软件/盗版嫌疑）。**数据地基 `device_software`（终端软件清单结构化快照）**：此前软件清单只在 reports/snapshots 的原始 payload JSON 里"搭车"、无任何结构化消费——阶段三起 ingest 的 full 上报（每小时）经 `syncDeviceSoftware` 落库，**按终端全量覆盖**（先删后插单事务，卸载即消失，无软删除语义）；空名条目（卸载残留注册表键）不入库；**公司归属取自绑定的台账资产**（agent_devices.asset_id → assets.company_id）——未绑定资产的终端不参与合规统计，绑定后下一条 full 上报自动纳入；落库是上报主流程的旁路（失败只记日志，`store.DB == nil` 防御与 syncToAssetLedger 同款）。**受控软件池 `software_pools`**（`company_id` / `name` 匹配键 / `vendor` / `category` / `license_id` 可空挂接 licenses（席位来源；NULL = 不限席位，永不超用）/ `remark`）：入池即受控；名称同公司唯一（409，软删不占名）；许可挂接校验存在+同公司（404）、0/null 解除；**池项不做席位余量校验**——超用正是合规引擎要发现的问题，不是建池时要拦的；许可删除被资产**或池项**挂接时 409 带计数。**匹配口径单源 `softwareaudit.MatchPool`**：采集名与池名精确相等优先，无精确时按「采集名包含池名」命中（注册表 DisplayName 变体多，池里写「Microsoft 365」即可命中「Microsoft 365 Apps for enterprise」），多个包含命中取池名最长者（最具体优先）；一个采集名只归一个池项。**超用判定**：挂接许可 total_seats > 0 且去重安装终端数 > total_seats（GROUP BY name + COUNT(DISTINCT device_id) 聚合，报表 API 与引擎共用 `ListSoftwareInstalls` 单源）；许可已删/未挂接按不限席位处理不误报。**未受控商业软件**：不在任何池项的安装，且不命中**内置系统组件白名单**（`softwareaudit.systemSoftwareKeywords`，小写包含匹配——只收运行库/驱动/系统组件与明确免费的常装软件，**关键词防误伤**：不能用裸 "intel"（IntelliJ IDEA 会中招），商业软件一律不收，宁误报给管理员甄别不漏报盗版嫌疑）；未受控清单按安装数降序（同数按名字典序稳定）分页呈现，**只进报表不投通知**。**引擎 `internal/server/softwareaudit`**（webhook/licensealert 同款骨架）：每小时（与 full 上报节奏对齐）+ 启动即扫一轮，逐公司比对，超用池项经 `software_overuse` 冷却键（webhook_alert_states 同表，asset_id 列存池项 ID，默认 24h、`software_overuse_cooldown_hours` 可配）提醒公司管理员（Notifier 闭包注入，resource=`software` 跳软件与授权许可页）；投递失败不落冷却状态下轮重试。**API**：`/api/v1/software-pools` 读面登录可读 + 写面 admin；`/api/v1/software-compliance` 整组 admin-only（报表中心先例）——汇总卡（受控池项/超用池项/受控安装终端数/未受控商业软件种数）+ 池项全量（含超用标记与挂接许可名）+ 未受控分页。**Web 端**：软件与授权许可页扩为三 tab（共用公司维度）——授权许可池（原 P2 面零回归迁移）/ 受控软件池（CRUD，许可下拉 page_size=200 缓存模式）/ 合规审计（admin：四卡 + 池项比对表 + 未受控分页表）。

**组织与公司管理契约（2026-09-26 落地；CIYO 为 RuoYi 框架级通用 CRUD，无 ITAM 特殊语义，本项补齐标准闭环）**：公司（多租户）此前只有列表/新增/详情——**无更新删除、Create 无重名校验、写面全员开放**（阶段一遗留越权面）。本次补齐：① 名称全局唯一 409（Unscoped 查重与 `companies.name` 唯一索引同口径——软删行占名，查重不过滤会「查重说可用、插入撞索引 500」）；② 更新（PUT /:id：改名/编码/域名——公司 ID 不变，历史数据零迁移）；③ **删除活跃引用拦截**：`companyRefTables` 表清单单源（资产/用户/部门/许可/耗材/受控池/终端软件等 18 表）逐表计数，任一**活跃**引用即 409 带「资产 N、人员 N…」明细——**软删行视为已死不计**（VM 烟测捞出的死锁缺陷：资产删除是软删，若计数含软删行则「删资产再删公司」的正常流程被永久阻塞且无 UI 途径清理；孤儿软删行随公司删除残留是可接受代价，展示层不可见）；零引用才可删且**硬删**（软删行永久占名 → 同名公司无法重建，公司零引用即无历史可保）；④ 写面收口 admin（读写两组注册），读面登录可读（各页公司下拉依赖）。**Web 端**：组织架构页实装（原整页 mock「东莞/苏州」写死数据废除）——公司管理卡（CRUD + 409 明细展示）+ 人员总览卡（真实 users 按公司过滤只读，维护入口在用户与权限页）；菜单更名「组织架构」（AD 目录同步属阶段二规划，页内明示）；总览大盘「各子公司资产分布」改真实数据（公司列表 × 逐公司资产 total）——大盘其余统计卡 mock 演示面已于 2026-09-26 全部清零，见「总览大盘契约」。**运维注记（2026-09-26 实测捞出）**：老生产库残留 15 个历史物理外键（9-20 旧源码时代 schema 遗留：assets→companies/users、storage_lendings/depreciations→companies、asset_events/dispatches/requests/repairs/versions/stocktake_items→assets 等）——**与「逻辑外键 + 应用层校验」契约冲突**（物理 FK 会挡公司硬删与 SQL 清理，且 AutoMigrate 从不建 FK、新库无此异物），已一次性 `ALTER TABLE ... DROP FOREIGN KEY` 全部清除（`temp/vm_drop_legacy_fk*.py` 留档）；**克隆旧库升级时需同检 `information_schema.KEY_COLUMN_USAGE`**。

**加密标记抽象契约（2026-09-26 落地）**：`assets.sec_encrypted` 与 `storage_lendings.sec_certified` 的语义是**「已被公司统一部署的终端加密系统纳管/认证」的通用布尔标记**——**加密产品名（绿盾等）不落码、不落库**：字段与文案层一律通用（「加密软件管理 / 已纳管」「加密认证 / 已认证」），具体产品归属公司管理制度与备注字段，**更换加密系统时代码零改动**。历史文档（system_blueprint）中的产品名仅作当时背景记录。Agent 侧注释提及的「绿盾环境」指用户公司真实终端环境事实（EDR 拦截解释器等），非系统绑定。

**总览大盘契约（2026-09-26 落地，Dashboard mock 演示面清零收官）**：`GET /api/v1/dashboard/summary` 是大盘唯一数据源——**一次请求聚合全部数字**（避免前端拼 N 个请求），**全员登录可读**（大盘是全员工作台首页，不带 RoleMiddleware），**全集团跨公司口径**（不筛 company_id——与 admin 单公司的 /reports/summary 是两个面，报表口径红线不可直搬）。**口径单源纪律**：① 资产状态计数 = 台账全量（含报废）+ GORM 默认软删过滤（同列表口径），状态段位复用 `model.AssetStatusName`；② changes_pending 谓词与 `store.ListChangeEvents(includeAcked=false)` 单源（`acked = false`）；③ 终端活跃/失联公式与 `model.ResolvePresence` 完全一致（`now - last_seen > 阈值` 即失联），阈值经 SetupRouter 注入（源头 `offline_threshold_sec`，禁止前端/后端各自写死分钟数）；④ Agent 版本分布按注册终端总数计百分比（四舍五入整数，计数降序、同数字典序稳定），Top5 截断防病态多版本刷载荷。终端量级小（企业数百级），devices 直接拉轻量列 Go 侧聚合——规避跨驱动 SQL 时间比较方言（report.go 先例）。**U8 集成面移除（2026-09-26 拍板）**：采购订单只作台账溯源字段——`assets.u8_order_no` 文本字段保留（建账/编辑可录、详情抽屉可见、资产页全文搜索命中），供应商/采购时间/价格溯源由台账既有字段（supplier_id / purchase_date / original_price）承载，**不做 U8 API 对接**（CIYO 对标同口径：无 ERP 集成，采购信息即台账字段）；大盘「用友 U8 采购追踪」假卡与 Settings「用友 U8 v18 集成」假 tab 一并移除，禁止造假金额。**「AD 组织架构同步」假状态行移除**（无此功能，AD 目录同步属阶段二规划）。Web 端：顶部三卡（资产总数含在册/报废拆分、使用中含在库/维修拆分、待处理告警与变更可点跳 /changes）+ 实时终端与 Agent 状态卡（注册/活跃/失联三数 + 版本分布行 + 刷新按钮；空库 el-empty 空态，文案走 computed 规避 el-empty 内联三元中文引号坑）；拉取失败数字展示 '—'，刷新可重试。

---

## 8. Agent 自动更新与远程推送

### 8.1 自动更新

- **轮询模式**（不是服务端主动连终端）：Agent 每次心跳时服务端在响应中带 `update` 字段；Agent 检测到版本更高则自动升级
- **更新包协议**：
```json
"update": {
  "version": "1.2.0",
  "url": "http://server:8443/api/v1/files/pkg/core-agent-1.2.0.zip",
  "sha256": "…",
  "sign": "…",           // Ed25519 签名（v1.1 起强制）
  "mandatory": false
}
```
- **升级流程**：后台下载到临时目录 → 校验 sha256 + 签名 → 延迟任务替换二进制（Windows 无法替换运行中 exe，由 `updater` 辅助进程完成）→ 旧版退出 → updater 启动新版 → 新版上报自身版本 → 服务端确认灰度比例
- **灰度策略**：服务端按设备分组指定目标版本（`update_groups`：`canary` / `stable`），新版本先 5% → 观察 24h 无异常 → 全量
- **防砖**：新版本启动 10 分钟内必须成功注册一次上报否则自动回滚到备份副本（updater 保留旧二进制）

### 8.2 文件/脚本推送（远程任务）

- 管理端创建任务：`POST /api/v1/tasks`，类型：
  - `fetch_file`（上传一个文件到终端指定路径）
  - `run_script`（下发 PowerShell/PowerShell 7 bash 脚本执行）
  - `collect_file`（从终端取回一个日志/配置文件回服务器）
- 任务投递：Agent 心跳/全量响应里下发 `tasks[]`；Agent 执行后走 `/api/v1/tasks/{id}/ack` 回报结果（stdout 尾部 + exit code）
- **安全约束**：
  - 脚本必须经服务端签名校验（与更新包共用 Ed25519 密钥对）
  - 脚本 = 已知白名单模板的参数化调用（禁止自由文本脚本 v1.0；v1.1 开放自由脚本但需双管理员审批）
  - fetch_file 目标路径白名单（仅安装目录 / ProgramData\itagent\）
  - collect_file 来源白名单（日志目录、指定采集路径）
- **执行窗口限制**：默认只允许工作时间外运行高影响任务（域策略脚本除外）

### 8.3 与 ad-selfservice 的接口级集成

- 不合并代码库（Go ↔ Python），不共享数据库
- **通知通道**：itagent 告警 → POST ad-selfservice 内部 notify API（经管理员 token），复用已调通的企业微信通道
- **身份关联**：itagent 心跳带 `logon_domain\logon_user`；ad-selfservice 前端「员工资产」页按用户反查 itagent `/api/v1/devices?logon_user=<sam>`，实现人↔设备双向查询
- **运维入口统一**：ad-selfservice 管理后台加「IT 资产管理」菜单项，嵌入/跳转 itagent Web UI
- 两端数据库独立演进（各自 SQLite → PG/MySQL 变体），将来迁 PG 可考虑同实例不同 database，便于 DBA 统一备份

**网络暴露面对齐（沿用 ad-selfservice 的 S-6 模式）**：

| 系统 | 侧 | 公网（经 NPM） | 内网 |
|---|---|---|---|
| ad-selfservice | 员工 OAuth 改密 | ✅ `adp.samsuncn.net` | ✅ |
| ad-selfservice | 管理后台 + `/api/v1/admin/*` | ❌ ACL 拦截，伪装 404 | ✅ |
| itagent | Agent ingest/register/config（`/api/v1/*`） | ✅ `agent.samsuncn.net`（备用机） | ✅ |
| itagent | 管理 UI + 管理 API | ❌ ACL 拦截，伪装 404 | ✅ / VPN |
| 集成链路 | itagent → ad-selfservice notify API | 不走公网 | 内网直连 backend:8000 |

原则：**凡是带"管理"性质的入口一律仅内网**；公网只放"机器对机器"的 ingest 通道 + "员工自助"通道。公网上拿不到管理功能，攻击面只剩 ingest（本身有设备 token + 安装令牌双重把关）。

### 8.4 USB 移动存储管控（usbguard）

**策略矩阵**：

| 设备类别 | 默认策略 | 实现 |
|---|---|---|
| U盘/移动硬盘（USB Storage） | 按服务端策略：off / audit / read-only / block | 服务策略键 `USBSTOR Start=4` + `RemovableStorageDevices` 策略 |
| 手机便携存储（MTP/WPD） | 随 USB Storage 策略 | 禁 WPD 类 + wpdmtp |
| 鼠标/键盘/无线 nano 接收器 | 永远放行 | HID 类不动 |
| 无线传屏盒子 | 按 VID/PID 白名单放行 | Device Installation Restrictions |

**联动**：策略走 `agent/config` 下发的 `usb_policy` 字段；**移动介质插拔事件独立进变更中心的安全审计流**（与硬件变更白名单分离）；macOS 侧仅审计+企微预警，阻断需 MDM。

---

## 9. macOS 说明（47 台+，占比提升，优先级上调）

- 分发：`.pkg`（pkgbuild + productbuild，postinstall 注册 launchd）
- 自启动：`/Library/LaunchDaemons/com.company.itagent.plist`，`KeepAlive=true`
- SMART：`smartctl` 打包进安装包；内置磁盘可读
- 签名：v1.0 不签名，部署脚本附带 `xattr -cr`；正式化再申 $99 账号签名+notarize
- 防卸载：root 下无法阻止彻底卸载，在部署文档明确

---

## 9. 安全设计

1. 公网链路 TLS 1.2+，NPM 终止 TLS；内网可 HTTP
2. device_token 走 Header，本地 v1.0 明文 0400 权限，v1.1 DPAPI/Keychain
3. 退出/卸载密码仅存 Argon2id hash，且归服务端集中管理（安装包不烧入任何密码）
4. NPM 只放通 `/api/v1/*`，管理 UI 仅内网/VPN
5. 最小采集：不采密码、文件内容、键盘、屏幕、浏览记录
6. JWT 签名密钥配置化：`server.json jwt_secret` 优先、环境变量 `ITAGENT_JWT_SECRET` 兜底、双缺省回落内置默认并启动告警（`middleware.SetJWTSecret` 启动时注入包级 var，GenerateToken/ParseToken/AuthMiddleware 签名零波及）；secret 严禁提交仓库，变更后存量 token 全失效（401 → 前端跳登录）属预期

---

## 11. 实施路线

| 阶段 | 内容 |
|------|------|
| P0 | 仓库初始化 + 服务端 ingest + SQLite + 简单查询 |
| P1 | Windows 采集 + spool + 上报 + 主备切换 |
| P2 | diff 引擎 + SMART 健康规则 + Web UI（设备/变更/健康预警） |
| P3 | Tray UI + 密码退出 + watchdog |
| P4 | 安装包 + 注册令牌 + 30 台试点（含 10 台 mac 同步开发） |
| P5 | Windows 全量推广 |
| P6 | macOS 全量推广（与 Windows 推广并行） |
| P7 | Snipe-IT 连接器 + 插件框架 + Webhook 通知（对接 ad-selfservice 企微通道） |
| P8 | Agent 自动更新（灰度+回滚）+ 远程文件/脚本推送任务 + ad-selfservice 人↔设备视图 |
