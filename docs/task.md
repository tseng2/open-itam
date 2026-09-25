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
  - `[x]` 台账 Excel 历史数据导入工具（2026-09-24 随 P1 首项"Excel 批量导入导出"落地，见下方 P1 勾选）
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
- [x] **盘点任务**（2026-09-24 完成：store 测试 11 项 + gin API 9 项 + 标签渲染 4 项全绿）
  - [x] 模型：`stocktakes`（draft10→processing20→finished30/canceled40）+ `stocktake_items`（pending10→normal20/lost30/damaged40/scrapped50，asset_tag 快照 + expected/actual location + scanned_by/at），TDD 先行
  - [x] Store 接口 + GormStore + SQLiteStore 双实现；扫码盘点码只存 SHA-256（明文仅 start/rotate 一次性返回，finish/cancel 即失效，可轮换止血）
  - [x] gin admin API（创建圈定快照/开始/明细核对修正/结束/取消/盘点码轮换）+ 双层挂载表（`/api/v1/stocktakes` + `/api/public`）
  - [x] 免登录移动扫码面 `/api/public/stocktakes/*`（IP 限流 120/min + 白名单字段视图 + 批量 ≤100）：Vue 移动页 `/m/t/:token` → `/m/scan` → `/a/:number`（标签 QR 直达）
  - [x] 标签 PDF：`internal/server/label` 纯 Go（go-pdf/fpdf + boombuler/barcode 零 CGO），A4 3×7，QR 直达移动核对页
  - [x] A3 协同：核对"正常"自动确认 pending 的 hardware_change 待审事件；异常结果（丢失/损坏/报废）记 stocktake AssetEvent
  - [x] Web UI：盘点任务管理页（进度条/明细抽屉/修正核对/盘点码 QR 对话框/标签 PDF 下载）
- [x] **设备申请审批流**（2026-09-24 完成：store 测试 7 项 + gin API 6 项全绿）
  - [x] 模型：`asset_requests`（申请人 + 姓名快照 / 目标资产 / 长期领用 vs 短期借用归期 / 事由 / 审批人与批注），状态机 pending→approved/rejected/canceled
  - [x] **审批+绑定同事务**（CIYO 精髓）：approve 单事务完成申请流转 + 资产绑定领用人 + 台账 10→20 + AssetEvent(assign)；竞争申请 409 回滚保持 pending
  - [x] 越权收口：user 角色只看/只撤自己的申请，审批/驳回仅 admin，管理员可代录（姓名快照取自用户表）
  - [x] Web：设备申请页（用户提交 + admin 审批队列/驳回批注）+ 资产详情抽屉"申请中"卡片（就地通过/驳回）
- [x] **折旧规则引擎**（2026-09-24 完成：纯函数 10 项 + 引擎 7 项 + store 6 项 + gin API 5 项全绿，覆盖率 86.6%+）
  - [x] 模型：`depreciations`（公司维度规则：months 总月数 / floor_type(amount|percent) / floor_val / stages 阶梯 JSON / enabled），资产加 `depreciation_id` 挂接；stages 按时间顺序分段累计、段内整月线性折算，空 stages 走直线折旧
  - [x] 计算核心收口 `internal/server/depreciation` 纯函数包（解析校验/月数/阶梯匹配/残值兜底/净值，API 校验与引擎共用）；坑：`enabled` 列不能加 `default:true` 标签（bool false 是合法值，GORM 会把零值替换成列默认值，停用规则建不出来）
  - [x] Store 双实现 + SQLiteStore schema + AutoMigrate；被引用规则删除拦截在 API 层（409 返回引用数）
  - [x] 引擎：goroutine + Ticker（默认每小时）刷净值，差异 ≥1 分才写库；停用/悬空/跨公司/缺购入日期/原值非正一律冻结现值；手动重算端点共用 ScanOnce
  - [x] gin API（读面登录可读供资产表单下拉，写面/重算仅 admin）+ server.go 双层挂载表
  - [x] **列管资产（财务维度，与运营状态正交）**：Asset 加 `off_book`/`off_book_at`，折旧完且财务销账的资产转"列管"继续跟踪使用直至报废变卖；严禁塞进 Status 状态机，展示层派生标签 + 列表筛选；销账/恢复端点联动 AssetEvent（off_book/off_book_restore）
  - [x] Web：折旧规则管理页（阶梯动态编辑/引用拦截/手动重算）+ 资产表单折旧规则下拉 + 详情抽屉销账/恢复 + 列表"列管"标签与筛选

### P1 维度治理 / P2 体验运营
- [x] **台账 Excel 批量导入导出**（2026-09-24 完成：assetexcel 纯函数包 9 项测试覆盖率 88% + gin API 集成测试 6 项全绿；导入按表头名映射/RawCellValue 日期序列号兜底/行级校验全量拒收（40006 带行级明细）/软删除占用查重 Unscoped/挂折旧规则导入即算净值；导出与列表共用过滤口径、富化领用人/公司列、导出文件可直回导；模板含示例行与类别/状态下拉）
- [x] **型号库 / 供应商 / 厂商 / 位置库**（2026-09-25 完成：4 张公司维度维表 + Asset 四外键列；store 16 方法双实现 TDD 全绿 + gin API 集成测试 8 项全绿；名称同公司唯一（软删不占名）、删除引用拦截 409 带计数、位置树形防自引/成环、型号预挂折旧规则建账自动继承、厂商随型号带出；列表/详情/导出共用 enrichAssetsDimensions 富化（重命名可传播、快照列兼容 Excel/标签）；Web 新增"基础数据（维度库）"管理页 + 台账表单四下拉联动）
- [x] **操作日志（P2 首项）**（2026-09-25 完成：store 契约 4 项 + audit 中间件 4 项 + gin API 集成 2 项全绿；追加式审计表 `operation_logs`，变更类请求经 gin 审计中间件统一留痕 + 登录成功/失败单独留痕，admin 只读查询面，审计失败绝不阻塞业务）
- [ ] 消息中心（WebHook+站内信起步）/ 员工自助门户 / 报表中心 / 软件许可 / 耗材管理

*(详细设计依据：`temp/同类资产管理系统/CIYO功能地图分析.md` 与 `temp/0924/长期出差终端管理可行性分析.md`，内部工作稿不入库)*
