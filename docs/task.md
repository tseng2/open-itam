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
- `[x]` **公司（多租户）管理闭环与加密标记通用化**（2026-09-26 完成实测修复：① 公司此前只有列表/新增/详情且写面全员开放——补齐更新（改名/编码/域名，ID 不变零迁移）、名称全局唯一 409（Unscoped 查重与 DB 唯一索引同口径）、删除全业务表引用拦截 409 带明细（companyRefTables 18 表单源清单，**只算活跃行**——软删行视为已死不阻塞，VM 烟测捞出「删资产（软删）再删公司」死锁缺陷后修正）、零引用硬删（软删永久占名）、写面 admin 收口；② 组织架构页实装（原整页 mock「东莞/苏州」写死废除）：公司管理卡 + 人员总览真实数据，菜单更名「组织架构」明示 AD 同步属阶段二规划；③ 总览大盘「各子公司资产分布」改真实数据（其余 mock 卡已于同日全部清零，见下方「总览大盘实装」条目）；④ 加密标记抽象：assets.sec_encrypted / storage_lendings.sec_certified 文案与注释全部通用化——「已纳管/已认证」，加密产品名不落码不落库，更换加密系统零代码改动。company_api_test 契约测试锁死（CRUD/重名/引用拦截/403））
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
  - `[x]` 移动存储领用、配件记录的前端管理页面（2026-09-26 快赢收官：`/storage-lendings` + `/part-records` 两页挂资产管理组（外派出差终端之后）；顺带修复 storage-lendings PUT 显式 null 清空契约——原阶段一实现"未传"与"显式 null"都归为保持、日期清空静默失败，以原始键集合判定修复并测试锁死；配件为追加式流水无编辑删除面，操作人服务端取 JWT，asset_tag 文本关联）
  - `[x]` 台账 Excel 历史数据导入工具（2026-09-24 随 P1 首项"Excel 批量导入导出"落地，见下方 P1 勾选）
  - `[ ]` 履历附件上传（依赖文件存储体系）

## 阶段二：组织同步与人机绑定
- [ ] 从 AD 自动同步组织与人员名单
- [ ] Logon_user 关联匹配逻辑

## 阶段三：软件合规与深度采集

- [x] **软件合规比对：受控软件库 × Agent 采集**（2026-09-26 完成，CIYO/Snipe-IT 均无此能力的自研差异化项：① 数据地基 `device_software`——软件清单此前只在 reports/snapshots 原始 JSON 搭车、无结构化消费，阶段三起 full 上报（每小时）经 `syncDeviceSoftware` 按终端全量覆盖落库（单事务先删后插、空名键不入、公司归属取绑定资产、未绑定终端不参与统计），旁路失败不阻塞上报；② `software_pools` 受控池——入池即受控，名称同公司唯一 409，license_id 可空挂接做席位来源（**池项不做席位余量校验，超用正是引擎要发现的**），许可删除被资产或池项挂接 409；③ `internal/server/softwareaudit` 纯函数包 + 引擎——匹配口径单源（精确优先、包含匹配取最长池名）、系统组件白名单降噪（防误伤：不能用裸 intel，IntelliJ IDEA 会中招）、超用 = 挂接许可且去重安装终端数 > 席位，引擎每小时 + 启动即扫，超用池项经 `software_overuse` 冷却键（默认 24h，server.json `software_overuse_cooldown_hours` 可配）提醒公司管理员，未受控商业软件清单只进报表不投通知；④ API `/api/v1/software-pools`（读面登录写面 admin）+ `/api/v1/software-compliance`（admin-only 报表）+ 通知第五类 software_overuse 与 resource=software 跳转；⑤ Web 软件与授权许可页扩三 tab（授权许可池零回归迁移 / 受控软件池 CRUD / 合规审计四卡两表）。验证：softwareaudit 纯函数 + 引擎 10 项、v1 集成 4 项（CRUD/RBAC/报表口径/许可删除拦截）、ingest 落库 e2e 1 项全绿；go vet/build/test 全量零回归 + npm build ✅）
- [ ] macOS 全量采集与部署（原 P5）
- [ ] 灰度下发与远程脚本执行（原 P6/P7）

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
- [x] **操作日志（P2 首项）**（2026-09-25 完成：store 契约 4 项 + audit 中间件 4 项 + gin API 集成 2 项全绿；追加式审计表 `operation_logs`，变更类请求经 gin 审计中间件统一留痕 + 登录成功/失败单独留痕，admin 只读查询面，审计失败绝不阻塞业务；VM 生产 MariaDB 实测通过——vm_verify_p2.py 全 PASS 零残留 + 既有回归 59 项零回归）
- [x] **消息中心起步（站内信）**（2026-09-25 完成：store 契约 3 项 + gin API 集成 2 项全绿；`notifications` 收件箱（user_id 即安全边界、company_id 可选、已读幂等），首个事件源接设备申请审批流——提交通知管理员待办、通过/驳回回执申请人，投递失败不阻塞主流程；Web 顶栏铃铛 30s 轮询 + popover 收件箱。WebHook 出站通道已在 A4 落地；VM 生产实测通过——提交/审批/驳回/越权/幂等/全部已读全链 PASS 零残留）
- [x] **员工自助门户**（2026-09-25 完成：portal 三端点 JWT 本人收口——四卡统计（我的设备/待审批/持有天数/30 天内到期归期）+ 我的设备（在用+维修中，带维度富化）+ 我的申请；gin 集成测试 2 项全绿；Web「我的门户」页全员可见）
- [x] **报表中心**（2026-09-25 完成：`/reports/*` 整组 admin-only——汇总 8 卡（状态计数台账全量口径 + 价值合计在册口径）、年度资产价值（近 N 年窗口零填充）、月度建账趋势、状态/类别分布环图；聚合纯函数 3 项 + gin 集成测试 3 项全绿；日期桶 Go 侧聚合双库可移植；Web 图表页）
- [x] **软件许可管理**（2026-09-25 完成：`licenses` 授权池——席位总数/密钥/到期日/终止日，**状态与已用席位均不落库**（日期派生 + 资产挂接计数）；资产经 `license_id` 挂接席位，席位已满 409、挂接中删除 409 带计数、expiring_days 到期提醒窗口；store 契约 3 项 + 状态纯函数 + gin 集成测试 2 项全绿；Web 软件页实装（原 mock）+ 台账表单许可下拉）
- [x] **耗材管理**（2026-09-25 完成：`consumables` + 追加式流水 `consumable_txns`（入库/出库/调整）——**库存只经流水变更**，条件更新原子扣减（击穿零库存 409 ErrInsufficient），最低库存预警派生，有流水删除 409；store 契约 3 项 + gin 集成测试 2 项全绿；Web 耗材管理页 + 流水抽屉）
- [x] **P2 余项 VM 部署实测**（2026-09-25 完成，HEAD `b1774b2` 生产 MariaDB：三表迁移（licenses/consumables/consumable_txns）+ assets.license_id 列 + licenses/consumables/portal/reports 四前缀 401 挂载全绿；新增 `temp/vm_verify_p2b.py` 回归 58 项全 PASS 零残留——许可四状态日期派生/到期窗口双侧过滤/席位超用 409/换绑自占排除/删除引用拦截/续期状态恢复、耗材出入库带符号流水/操作人快照/不足 409/预警派生/有流水删除 409、门户四卡/我的设备富化/本人收口、报表 8 卡口径/年度/月度/双维分布/RBAC 三面；excel 28/28 + dimension 31/31 + p2 既有回归零回归；health 巡检双容器 healthy、3 终端心跳推进、无错误日志）——阶段五 P0/P1/P2 路线全部收官
- [x] **消息中心事件源扩展（阶段五收官：三类运营提醒）**（2026-09-25 完成：① A4 告警联动站内信——webhook 引擎双通道出站（启动即扫一轮），站内信独立于 WebHook 开关，冷却去重复用 webhook_alert_states 同表 notify_overdue/notify_missing 独立键，AlertNotifier 函数注入规避 webhook↔api/v1 循环依赖；② 耗材低库存沿触发——postTxn 旁路，旧库存>预警线且新库存≤线才投（旧库存由新库存-增量回推免加读），持续低位/回补不轰炸，未配预警线不投（与列表 low_stock 口径一致）；③ 许可到期窗口扫描——licensealert 引擎每小时+启动即扫，窗口口径复用 ListLicenses ExpiringDays 单源勿重写，同表 license_expiring 键（asset_id 列存许可 ID）冷却=窗口天数天然只投一次；v1 侧 notifyCompanyAdminsCtx 无 gin 上下文扇出 + NewAlertNotifier/NewLicenseExpiringNotifier 闭包经 main 组装；webhook 引擎 9 项 + licensealert 8 项（覆盖 89.5%）+ v1 扇出/沿触发 3 项 + middleware 4 项测试全绿）
- [x] **JWT secret 配置化（技术债清偿）**（2026-09-25 完成：server.json jwt_secret + 环境变量 ITAGENT_JWT_SECRET 兜底 + 双缺省回落内置默认并启动告警；SetJWTSecret 启动注入包级 var，GenerateToken/ParseToken/AuthMiddleware 签名零波及，login 自动生效；secret 变更后存量 token 全失效（401→前端跳登录）属预期；middleware 单测 4 项：密钥轮换存量失效/空串忽略/错密 token 401/roundtrip）
- [x] **阶段五收官 VM 部署实测**（2026-09-25 完成，HEAD `a15c065` 生产 MariaDB：新增 `temp/vm_verify_events.py` 全 PASS——JWT 三查（注入后登录正常/本次启动无回落告警/篡改签名 401；未配置时回落告警日志在案）+ 三类通知落库（asset_alert 2 条：超期未归+疑似失联，WebHook 关闭下照投；license_expiring 1 条：窗口内投、窗口外对照不投、文案带名称与到期日；consumable_low_stock 2 条：沿触发恰好两次）+ 冷却键隔离（notify_* 2 + license_expiring 1 落库，WebHook 通道键 0——独立开关与独立键双证明）+ 管理员未读计数 5 + 二次重启冷却去重跨重启不重复轰炸 + SQL 清理零残留；VM configs/server.json 已注入 jwt_secret；excel/dimension/p2/p2b 四套既有回归零回归 + health 巡检全绿（双容器 healthy、新前端资产生效、3 终端心跳推进、重启后无错误日志））——**阶段五全部收官（消息中心双通道 + 三类事件源 + 安全债清偿）**
- [x] **快赢双件：阶段一遗留前端清欠 + 独立消息中心页**（2026-09-26 完成：① 移动存储领用页 `/storage-lendings` + 配件出入库页 `/part-records`（模型与 API 阶段一就绪，纯 Web 补页，两 API 写面全员开放前端跟随现状）；**storage-lendings PUT 契约缺陷实测捞出**——原实现"未传"与"显式 null"经指针反序列化无法区分、日期编辑清空静默失败，以原始键集合判定修复（未传=保持、显式 null=清空）并 TDD 契约测试锁死；② 独立消息中心页 `/notifications`（全员菜单，我的门户之后）——四类类型过滤 + 仅看未读开关 + 分页 + 点击就地已读并按 resource 跳转 + 全部已读带受影响数，resource 路由映射抽共享模块 `web/src/notifications.js` 铃铛与页面两处共用，铃铛 popover 加「查看全部」入口）
- [x] **总览大盘实装（Dashboard mock 演示面清零收官）+ U8 集成面移除**（2026-09-26 完成：① 新增 `GET /api/v1/dashboard/summary` 轻聚合端点——**全员登录可读、全集团跨公司口径**（区别于 admin 单公司 /reports/summary，报表口径红线不直搬），一次请求聚合资产状态计数（台账全量含报废、软删行不计、段位复用 AssetStatusName）+ changes 未 ack 计数（谓词与 ListChangeEvents 单源）+ 终端注册/活跃/失联三数（公式与 ResolvePresence 一致、阈值注入 offline_threshold_sec）+ Agent 版本分布 Top5（计数降序、同数字典序稳定、按注册终端总数计百分比）；devices 拉轻量列 Go 侧聚合规避跨驱动时间方言（report.go 先例）；server.go 双层挂载表补 dashboard 两行（A1 的 404 教训）；gin 集成测试 3 项（聚合口径/全员可读 401/空库零值/版本 TopN 截断）TDD 全绿。② Dashboard.vue 清 mock：顶部四卡改三卡真实数据（资产总数含在册/报废拆分、使用中含在库/维修拆分、待处理告警与变更可点跳 /changes）+ Agent 状态卡真实化（注册/活跃/失联 + 版本分布行 + 刷新 + el-empty 空态文案走 computed 防中文引号坑）+ 拉取失败 '—' 展示；**「用友 U8 采购追踪」假卡与「AD 已同步」假状态行移除**。③ **U8 集成面整体移除（用户拍板）**：采购订单只作台账溯源字段——u8_order_no 文本字段保留（资产页可录/详情可见/全文搜索命中），供应商/采购时间/价格溯源由台账既有字段承载，不做 U8 API 对接（CIYO 对标同口径：无 ERP 集成），Settings「用友 U8 v18 集成」假 tab 一并移除）

- [x] **终端联系状态与采集体系实测修复四件 + Agent 采集频率配置面（2026-09-26 下午）**（用户报告三案排查收官，实测根因全链实锤：① **cmp001「疑似失联」= 假失联窗口**——联系状态判定读 `agent_devices.last_seen_at`，该字段此前只有 full 上报（1 小时周期）更新而 600s 心跳只刷 `devices` 鉴权表不触碰绑定表，full 周期 > 15 分钟阈值 ⇒ 每小时必现 45 分钟「终端健康却判失联」；② **.160 五小时真离线（8:14-13:13，重装恢复）**——update.log 实锤 9-22 自更新风暴前科（5 秒级 8 轮 apply 循环）+ 自更新链死路：RunSelfApply 停服务后 backup/replace 失败直接 return，此时 watchdog 已被杀（`agent-watchdog.exe`/-guardian 实为死代码从未运行）、SCM failure recovery 只覆盖崩溃不覆盖正常 stop，系统无人能再拉起服务；③ **「漫游中」误判**——PublicIP 非空粗判 + 公司统一出口 NAT（两台终端出口同为 61.142.9.88），内网终端 100% 误判漫游。修复四件：① **假失联修复**：heartbeat ingest 同步刷新绑定行 agent_devices.last_seen_at（只刷时间戳，IP/硬件仍由 full 的 syncToAssetLedger 维护），e2e 测试锁死；② **漫游双维判定**（替代 PublicIP 非空粗判，用户否决出口 IP 白名单方案——多专线/拨号线路难维护，零清单维护定案）：`roaming = 本机 IP 公网可路由（网络维：直连公网/4G/拨号） || 出口 IP 异省/海外（地理维）`，地理维 = **ip2region v4 离线库**内嵌（`internal/server/geoip`，xdb 单文件 ~11MB go:embed 纯 Go 零 CGO；**新版段序国家|省份|城市|ISP|国家代码**，海外 IP 取国家段触发漫游）+ `agent_settings.company_province` 比对基准（默认广东省）；解析不出/库不可用跳过地理维宁漏报不误报；presence 纯函数 PresenceInput 扩 LocalIP/EgressCountry/EgressProvince/HomeProvince + GeoContext 注入，测试 12→18 项；③ **自更新死路修复**：RunSelfApply 所有失败 return 前强制重启服务（回滚旧包兜底）消灭「停了无人救」+ updater.Apply 全路径写 update.log（下载/sha 失败同样留痕）；④ **agent_settings 配置面**：DB 单例（心跳/full/失联阈值三联动 + 公司省份），设置页「Agent 采集与失联判定」tab（分钟呈现/秒存储；阈值>心跳、full≥心跳保存校验——阈值小于心跳周期则健康终端心跳间隔本身就击穿阈值全员假失联），**保存即生效**（`v1.EffectiveAgentSettings()` 实时读库无缓存），**阈值注入链从启动静态注入改造为判定时实时读取**（RegisterAssetRoutes/RegisterDashboardRoutes 删参、webhook Engine 闭包注入、main 组装），server.json 的 offline_threshold_sec/default_heartbeat_sec/default_full_sec 废弃删除；下发双通道（agent/config 响应 + ingest next_*_sec）；**Agent v0.2.6**：心跳 tick 拉取 config 时消费频率下发动态调周期（clamp 60s~24h，老版本不消费互不影响）+ 默认档位用户拍板：**心跳 1 小时 / full 6 小时 / 阈值 65 分钟**。验证：geoip 2 项（含并发安全——xdb Searcher 非线程安全须全局锁）+ presence 18 项 + store 契约 1 项 + agent-settings gin 2 项（默认回落/联动校验/RBAC）+ 假失联 e2e 1 项 + go vet/build/test 全量零回归 + npm build）

- [x] **GeoIP 市级漫游判定（按公司基准，2026-09-26 晚拍板一次收官）**（业务定案：漫游基准 = **资产所属公司的区域**而非全局白名单——总部东莞、分公司苏州/深圳，东莞公司的电脑跑到苏州算漫游、反之亦然，多公司各归各基准；ip2region 库本身城市级（实测公司出口 61.142.9.88 → 广东省|东莞市），此前只取省份段导致「整个广东省内出差都不算漫游」且多公司无法区分归属。落点六件：① **companies 表加 `region varchar(64)`**「省|市」格式（与 ip2region 库名口径一致；空 = 该公司资产跳过地理维宁漏报不误报），company Create/Update API 支持（分段 trim 归一/三段以上 400）+ 组织页公司管理卡维护（对话框 placeholder 示例 + 用途提示 + 列表「区域（漫游基准）」列）；② **geoip.RegionOf 升级三段返回 (country, province, city)**（v4 段序「国家|省份|城市|ISP|国家代码」，城市段 '0' 归一空串；顺带收口保留段缺口——Reserved 段国家与省份同名「视为解析不出」此前只写在注释里没落码，ProvinceOf 的中国过滤一直掩盖着它）；③ **presence 市级比对按公司基准**：PresenceInput EgressProvince/HomeProvince → EgressRegion/HomeRegion「省|市」；地理维 = 出口「省|市」≠ 所属公司 region（**同省异市也算漫游**；任一侧城市段缺失按省级宽口径降级，宁漏报不误报）；本机公网 IP 网络维、海外出口判定保持；基准注入 `PresenceGeo.RegionByCompany` 映射——调用方对本批资产涉及公司**一次批量 IN 查**防 N+1（`v1.PresenceGeoFor`，列表富化与 A4 引擎共用装配，webhook 引擎 geoFn 签名随之改收公司 ID 集合）；④ **agent_settings.company_province 全链删除**（当天刚加无生产依赖：model + GormStore/SQLiteStore schema + gin API PUT/GET + Settings.vue 字段同步清理）；⑤ 生产公司补 region（东莞总部「广东省|东莞市」、苏州「江苏省|苏州市」、深圳「广东省|深圳市」）；⑥ vm_verify_agentcfg 升级多公司夹具反向断言——同一出口 IP 东莞公司资产→漫游、苏州公司资产→在线，按公司基准直接实证。验证：geoip 三段 4 项 + presence 市级 case（异省/同省异市/同城/公司无 region 跳过/省级降级/regionMismatch 表驱动/多公司批量反向断言）+ company API region round-trip（归一/更新/清空/三段 400）+ PresenceGeoFor 契约（空 ID 安全/空 region 不入映射）+ agent-settings 响应去 company_province 断言 + go vet/build/test 全量零回归 + npm build。**VM 部署实测（2026-09-26 晚，HEAD `4e699c4`，生产 MariaDB）**：companies.region 列迁移 + 生产公司补 region（东莞「广东省|东莞市」/苏州「江苏省|苏州市」；**生产无深圳公司**，「默认集团公司」留空 = 跳过地理维，组织页可补）+ agent_settings.company_province 物理列 DROP；`vm_verify_agentcfg` 升级版 **22/22 PASS 零残留**——**同一出口双反向断言**（东莞出口 61.142.9.88：东莞资产在线/苏州资产漫游；苏州出口 222.92.0.1（内嵌库实测解析「江苏省|苏州市」）：东莞资产漫游/苏州资产在线，按公司基准直接实证）+ 异省（南京出口）/海外/无基准跳过 + 配置面无 company_province 断言 + 假失联修复回归；p2b/excel/dimension/p2/storage/software/events 七套既有回归 + dashboard 零回归（**dashboard 烟测捞出脚本级坑**：其 900s 阈值测试行 INSERT 仍携带已删的 company_province 列，列不存在整条 INSERT 静默失败、阈值不生效致失联差分断言挂——**直写 SQL 的列清单必须随 schema 删列同步**，烟测脚本已修）；health 全绿（双容器 healthy、容器内前端 index-cnkHPRgN（与本地构建 hash 不同属已知差异）、cmp001 心跳推进、重启后无错误日志）。顺手验证：**cmp001 实战二连自更新收官 0.2.4→0.2.6→0.2.7**（devices 表实证 0.2.7 + 心跳推进）；.160（DESKTOP-R6QDB7E）仍死于 0.2.5（14:00 后无心跳、收不到推送），重装需 RDP 人工执行 `temp/itagent-setup-0.2.7.exe` 后观察 `C:\ProgramData\ITAgent\logs\agent.log`。二期候选独立立项勿混做：异地漫游告警（geo_roaming 冷却键 + 所属公司管理员扇出）、资产分布地图大屏（ECharts china map）、出差轨迹审计（geo 变更历史表））

*(详细设计依据：`temp/同类资产管理系统/CIYO功能地图分析.md` 与 `temp/0924/长期出差终端管理可行性分析.md`，内部工作稿不入库)*

- [x] **异地漫游告警（geo_roaming，2026-09-26 深夜拍板一次收官）**（业务定案：漫游资产 → 通知**所属公司**管理员（东莞公司的电脑漫游到苏州通知东莞公司管理员）；四点定案——① 通知类型新增 `geo_roaming` 第六类（消息中心独立过滤，不复用 asset_alert）；② 触发口径 = 全部 `presence=roaming`（网络维本机公网 IP/4G + 地理维异省/同省异市/海外），文案带判定依据；③ 冷却窗独立可配 `geo_roaming_cooldown_hours`（server.json，默认 24h，不复用 cooldown_minutes——漫游是持续状态而非越催越急的失联事件）；④ WebHook 通道跟随双通道（开关控制批量推送 + 站内信独立投递）。**零新表零新引擎纯增量**：落点七件——① 常量：`model.WebhookAlertGeoRoaming` + `model.NotificationTypeGeoRoaming`；② **判定依据写回**：Asset 加 transient `RoamingReason`（`gorm:"-"`，Presence 先例），`ResolveAssetPresence` 漫游分支写回（内部 `resolvePresence` 判定+依据一次产出，非漫游态恒空防批量串台）——**BuildAlerts 直读禁止重算口径单源**；③ BuildAlerts 加 `case PresenceRoaming`：文案四要素 = 资产编码 + 所属公司区域（companies.region，未配置显示「未配置」）+ 判定依据 + 最近心跳；④ **独立冷却**：Engine 加 `geoCooldownFn func() time.Duration` 注入（main 读 server.json，非正回落默认 24h 常量），冷却时长按类型取——`FilterDue`/`notifyAlerts` 冷却参数化（签名改 `cooldownFor func(alertType string) time.Duration`，既有 FilterDue 边界测试同步改）；⑤ 站内信扇出：`v1.NewAlertNotifier` 按 alert_type 分流产出第六类（type=geo_roaming、title「异地漫游提醒：资产编码」、resource=assets 跳资产页），扇出复用 notifyCompanyAdminsCtx（所属公司 admin+super_admin）；⑥ 前端：notifications.js 三处映射（TYPE_TEXTS 异地漫游 + TYPE_TAGS warning + resource 路由既有 assets）+ Notifications.vue 过滤项（下拉走共享模块自动生效，副标题改六类）；⑦ 配置：server.json `geo_roaming_cooldown_hours` + 模板行 + main.go serverConfig 字段。验证：model RoamingReason 写回 4 case（网络维/地理维/海外/非漫游清空）+ BuildAlerts roaming 文案（地理维含出口解析区域/网络维含本机公网 IP/在线零误报）+ FilterDue 按类型冷却边界（geo 独立窗内抑制、到期放行）+ ScanOnce 双通道双键隔离（geo_roaming/notify_geo_roaming 双键落库）+ 独立冷却窗（基础窗 61min 过后 missing 重投而 geo 抑制、漫游窗到期才再投）+ Run 启动即扫 + NewAlertNotifier 第六类独立落库与所属公司扇出——TDD 全绿；go vet/build/test 全量零回归 + npm build（index-6UD1CA8e.js）。**VM 部署实测（2026-09-26 深夜，HEAD `977526e` 生产 MariaDB）**：`temp/vm_verify_georoam.py` **21/21 PASS 零残留**——东莞夹具公司（region 广东省|东莞市）资产 @ 苏州出口 222.92.0.1（last_seen 直写 DATE_ADD(NOW(), INTERVAL 8 HOUR) 保持新鲜）+ 失联对照 → 重启容器引擎启动即扫投递：东莞公司 admin+super_admin 站内信 2 条（type=geo_roaming、title「异地漫游提醒：资产编码」、resource=assets 指回资产、文案含出口 IP/解析区域 江苏省|苏州市/公司基准 广东省|东莞市/最近心跳，普通用户不收）+ asset_alert 类型隔离（失联对照走原通道）+ webhook_alert_states 四键各 1（geo_roaming/notify_geo_roaming/missing/notify_missing）+ WebHook 批量推送实证（宿主机 python3 listener，URL 走 docker 网关 192.168.240.1:18999，容器→网关可达）+ 二次重启冷却去重（站内信不增/状态行不增/推送日志行数不变）+ 清理零残留（含 webhook 配置 stash/恢复、冷却状态按 ID 差集回滚 638 行基线）。p2b/excel/dimension/p2/storage/software/events/agentcfg/dashboard 九套回归零回归 + health 全绿（双容器 healthy、容器内前端 index-DrzFRSED、cmp001 心跳推进、无错误日志）。**脚本侧新坑两枚**：① `pkill -f georoam_hook.py` 与 listener 启动合并同一条 exec 会按 pattern 匹配到自身 shell 把后续命令一并杀掉（探活 http=000 根因）——pkill 必须独立成条；② 一次批量 POST 的 JSON 里资产 tag 出现两次（asset_tag 字段 + message 内嵌文案），断言 count==1 永假，防重对比取首轮实际计数。**顺手修复（实测捞出的环境冲突）**：agent protection 注册表测试在本机挂——cmp001 生产 Agent 0.2.7 已把（禁用态）防护策略写进 HKLM\SOFTWARE\ITAgent\Protection，而 `Load()` HKLM 优先、测试只写 HKCU（注释预言过「HKLM 可能装有生产 Agent」但没落码防御），往返/缺失断言永远对不上；修复 = 抽 `loadAt(root)` 助手，测试断言收在自控的 HKCU 内（机器无关、注册表 IO 覆盖不减）。**顺手验证**：cmp001 0.2.7 收官在跑（devices 表实证心跳推进）；.160（DESKTOP-R6QDB7E）仍死于 0.2.5（14:00:40 后无心跳），重装 `temp/itagent-setup-0.2.7.exe` 需 RDP 人工执行，装完取 `C:\ProgramData\ITAgent\logs\agent.log`
