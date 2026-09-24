# ITAM 资产管理系统实施任务清单

本文档跟踪自研 ITAM 系统的实施进度。当前阶段：**阶段五 - ITAM 业务闭环（P0-α 出差终端管理优先）**。

## 阶段一：资产台账底座重构
- `[x]` **文档同步**
  - `[x]` 使用最新的系统规划更新 `docs/FEATURES.md`
  - `[x]` 使用最新的系统规划更新 `docs/architecture.md`
- `[x]` **数据库底座重构**
  - `[x]` 选型并引入合适的 ORM (GORM) 支持 PostgreSQL/MySQL（生产已切换 MySQL）
  - `[x]` 定义多公司实体模型 (Company)
  - `[x]` 定义组织架构实体模型 (Department, User)
  - `[x]` 重构资产实体模型 (Asset, Device分离)
  - `[x]` 定义资产事件模型 (Asset Event)
  - `[x]` 编写迁移脚本 (AutoMigrate)
- `[x]` **基础 API 框架搭建**
  - `[x]` 搭建 API 路由结构 (`api/v1`)
  - `[x]` 实现公司/租户的 CRUD 基础接口
  - `[x]` 实现资产 (Asset) 列表、详情、更新接口（U8 订单号保留为明细字段与查询参数）
- `[x]` **Web 前端基础重构**
  - `[x]` 按多租户/公司视角重构资产列表页
  - `[x]` 新增手工录入/编辑资产页面（台账全字段，含账面硬件规格）
  - `[x]` 资产详情与时间轴页面
- `[x]` **电脑台账对齐**（对齐 Excel 五表）
  - `[x]` 资产账面规格字段（CPU/内存/主从硬盘/显卡/网卡MAC）+ Agent 空值回填
  - `[x]` 外寄维修登记（API + 明细抽屉 tab，联动资产状态机）
  - `[x]` 移动存储领用表、配件记录表（模型 + API）
  - `[ ]` 移动存储领用、配件记录的前端管理页面
  - `[ ]` 台账 Excel 历史数据导入工具
  - `[ ]` 履历附件上传（依赖文件存储体系）

## 阶段二：组织同步与人机绑定
- [ ] 从 AD 自动同步组织与人员名单
- [ ] Logon_user 关联匹配逻辑

## 阶段五：ITAM 业务闭环（当前优先，2026-09-24 起）

### P0-α 差异化线：长期出差终端管理（推荐顺序 A1 → A2 → A3 → A4，✅ 已全部完成 2026-09-24）
- [x] **A1 外派登记**（2026-09-24 完成：store 测试 9 项 + API 集成测试 3 项全绿）
  - [x] 模型：`asset_dispatches`（company_id / asset_id / 负责人 / 目的地 / 外派日期 / 预计归期 / 隔离级别 / 预期格式化标记 / 状态），TDD 先行（store 测试）
  - [x] Store 接口 + GormStore + SQLiteStore 双实现
  - [x] gin admin API（创建/列表/归还/作废）+ 路由注册（含 server.go 双层路由挂载表）
  - [x] Web UI：外派登记页 + 资产详情页外派状态
  - [x] 外派/归还动作联动 AssetEvent 记录
- [x] **A2 失联语义分层**（2026-09-24 完成：presence 纯函数 12 项测试 + API 富化 3 项集成测试全绿）
  - [x] 规则：在线 / 漫游中 / 外派离线(预期内) / 超期未归(高危) / 疑似失联（依据外派登记 × LastSeenAt 计算，五态见 model/presence.go）
  - [x] 资产列表/详情页状态标签渲染（服务端计算 presence 字段，前端只渲染）
- [x] **A3 硬件 Diff 自动比对**（2026-09-24 完成：compareHardware 纯函数 10 项 + ingest 端到端 2 项全绿；首次覆盖 syncToAssetLedger 测试路径）
  - [x] ingest 时比对 AssetVersion 最新基线快照（内存/内置磁盘数量/磁盘序列号/CPU 数量与型号）
  - [x] 变更 → 自动生成 `hardware_change` AssetEvent（ReviewStatus=20 待审核）+ 详情页标红（复用既有警告框与待审核标签）
  - [x] 幂等：待审核事件存在期间不重复生成（审核通过更新基线后才会再次检测）；U 盘插拔不再误报（Removable 过滤）
- [x] **A4 超期/失联 Webhook 告警**（2026-09-24 完成：webhook 包 14 项测试覆盖 90.4% + store 3 项 + gin API 6 项 + presence 批量 1 项全绿）
  - [x] Webhook 配置存储（DB 单例 `webhook_alert_config` + Web UI Settings tab：URL/secret/开关/冷却窗口/手动测试）+ 定时扫描（goroutine+Ticker 每分钟，复用 ResolveAssetPresence 与 offline_threshold_sec）
  - [x] 仅 WebHook 单通道推送（POST JSON 批量合并，secret 非空带 X-ITAM-Signature HMAC-SHA256 签名头，出站 10s 超时）
  - [x] 冷却去重：`webhook_alert_states` 按 (company, asset, type) 唯一键，窗口内不重复推送，期满未处理再提醒，失败自动重试

### P0-β 复刻线：CIYO 对标三件套（A 线完成后接续）
- [ ] **盘点任务**：`stocktakes`/`stocktake_items` 状态机 + 标签 PDF + 免登录移动扫码页 + 明细核对
- [ ] **设备申请审批流**：`asset_requests` 状态机 + 审批即绑定领用人（单事务）
- [ ] **折旧规则引擎**：`depreciations` stages JSON 阶梯匹配 + 残值下限 + 定时任务刷净值

### P1 维度治理 / P2 体验运营
- [ ] 型号库 / 供应商 / 厂商 / 位置库 / Excel 批量导入导出
- [ ] 操作日志 / 消息中心 / 员工自助门户 / 报表中心 / 软件许可 / 耗材管理

*(详细设计依据：`temp/同类资产管理系统/CIYO功能地图分析.md` 与 `temp/0924/长期出差终端管理可行性分析.md`，内部工作稿不入库)*
