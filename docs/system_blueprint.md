# 自研 ITAM 资产管理系统整体规划与框架约定

为承接新的集团化、自研资产管理的定位，本文档定义了系统整体的功能地图、模块拆分、命名约定以及核心数据模型的字段约定。本规划将作为后续前后端开发和数据库设计的“宪法”。

## 一、 系统功能地图 (Feature Map)

整个系统分为以下几个核心功能域，涵盖了管理后台（Server端）和数据采集侧（Agent端）：

### 1. 仪表盘 (Dashboard)
- 集团与各公司资产大盘（总数、按状态分布）。
- 硬件变更与健康告警（SMART、硬盘异常、内存在线离线）。
- 软件合规状态概览（疑似非标软件安装）。

### 2. 资产管理 (Asset Management)
- **硬件资产 (Hardware Assets)**
  - Agent 自动上报的 PC/服务器台账。
  - 手工录入的外设（显示器、打印机、投影仪、网络设备）。
  - 资产全生命周期状态流转（入库 -> 分配 -> 维修 -> 报废）。
  - 资产时间轴视图（谁用过、更换了什么配件）。
  - U8采购单号关联查询。
- **软件与授权 (Software & Licenses)**
  - 商业软件池定义（如 Office, AutoCAD）。
  - 基于 Agent 采集的软件列表计算超用或盗版情况。
- **耗材与配件管理 (预留) (Consumables)**
  - 鼠标、键盘等低值易耗品的数量库存。

### 3. 组织与人员 (Organization & Identity)
- **集团/公司管理 (Companies)**
  - 定义各个独立核算的子公司实体（隔离数据的最顶层基础）。
- **部门与人员 (Departments & Users)**
  - AD/企微/ad-selfservice 同步而来的人员名单与组织树。
  - 用户名下挂载的资产一览表（入职发放清单，离职回收清单）。

### 4. 采集与变更中心 (Agent & Change Center)
- **预警中心 (Alerts)**
  - 硬件变更预警：内存/硬盘拔插或替换。
  - SMART 健康预警：磁盘坏道、温度过高。
- **采集配置管理 (Agent Config)**
  - 下发到各个 Agent 的采集频率、USB 策略管控。

### 5. 系统设置 (Settings)
- **数据字典 (Data Dictionary)**：自定义资产状态、资产分类。
- **系统集成 (Integrations)**：AD 同步配置、子系统联动 Slot。

---

## 二、 代码模块与架构拆分约定 (Architecture Modules)

系统延续 Go 语言开发，采用前后端分离，前端使用 Vue3。后端采用经典的充血模型与依赖注入分层架构。

### 1. 后端工程结构 (`internal/server/`)
- `api/v1/`: HTTP 路由与控制器层（处理参数校验，调用 service）。
- `service/`: 核心业务逻辑层（执行业务规则、状态机校验）。
- `store/`: 数据库交互层（Repository 模式），隔离底层 SQL。后续使用 GORM 等统一管理 PG/MySQL。
- `model/`: 数据实体层（Entity 与 DTO 结构体定义）。
- `sync/`: 负责 AD 组织架构拉取，以及对外接系统（如 U8、CRM 插槽）的同步。

### 2. 前端工程结构 (`web/src/`)
- `views/asset/`: 资产相关页面。
- `views/org/`: 公司、部门与人员管理。
- `views/alert/`: 变更与健康告警。

---

## 三、 数据库设计与字段约定 (Schema Conventions)

### 1. 全局设计规范
1. **命名约定**：数据库表名、列名一律采用 `snake_case`（全小写+下划线）。
2. **表名复数**：如 `users`, `assets`, `companies`。
3. **主键设计**：自增 `id` (bigint) 或者 雪花算法 ID。
4. **审计字段**：每一张业务核心表必须包含时间戳：
   - `created_at` (创建时间)
   - `updated_at` (最后更新时间)
   - `deleted_at` (软删除标志，留存历史追溯)
5. **多公司隔离约束**：任何属于特定公司的业务表，**必须带有 `company_id` 字段**。

### 2. 核心数据字典与实体抽象

#### 实体 1：公司 (Company) - `companies`
这是隔离的最顶层。
- `id` (PK)
- `name` (公司名称，如：东莞公司A、公司B)
- `domain` (对应的AD域名，如：jg.com, xk.com)
- `code` (简码)

#### 实体 2：部门与用户 (Department & User) - `departments` / `users`
- `users`:
  - `id` (PK)
  - `company_id` (所属公司)
  - `department_id` (所属部门)
  - `username` (AD 登录名 / sAMAccountName)
  - `job_number` (工号)
  - `real_name` (姓名)
  - `status` (在职/离职)

#### 实体 3：资产主表 (Asset) - `assets`
无论是否安装 Agent，所有的资产（PC、显示器等）都在这。
- `id` (PK)
- `company_id` (所属公司)
- `center_name` (中心) / `department_name` (部门) / `department_sub` (部门完整路径)
- `location` (主要存放位置或用途) / `manager_name` (资产负责人)
- `category_id` / `category_name` (资产分类：笔记本/台式机/显示器/外设)
- `asset_tag` (资产标签号，打印条码用)
- `u8_order_no` (U8 采购订单号；仅作台账明细字段，不在资产列表主页展示)
- `user_id` (当前领用人)
- `status` (状态：10-库存中, 20-使用中, 30-维修中, 40-已报废)
- `brand` (品牌) / `model` (型号) / `serial_number` (硬件序列号 SN)
- 账面硬件规格（人工维护的台账值，覆盖无 Agent 终端；Agent 仅在字段为空时回填初始化，不覆盖人工值）：
  - `cpu_name` / `memory_size` / `main_disk` / `secondary_disk` / `gpu_name` / `mac_address`
- 采购与财务：`purchase_date` / `acceptor` (验收人) / `warranty_period` (保修期) / `original_price` (原值不含税) / `net_value` (净值) / `sec_encrypted` (加密软件绿盾纳管) / `remark`
- `current_version` (当前硬件基线版本号)
- 派生计算列（不入库，前端实时计算）：购入年份、已使用天数/月数

#### 实体 4：Agent 终端指纹 (Device) - `devices`
专用于 Agent 动态上报的网络与 OS 数据，与 `assets` 为 1:1 关系（或者资产主表的一张扩展表）。
- `id` (PK)
- `asset_id` (关联的实物资产)
- `device_id` (Agent 生成的防伪特征码 sha256)
- `hostname` (主机名)
- `os_name` (系统版本)
- `mac_address` / `ip_address`
- `last_seen_at` (最后心跳时间)

#### 实体 5：资产事件轴 (Asset Event) - `asset_events`
对应台账"资产履历记录表"，记录"人员调拨"、"内存升级"、"系统重装"等所有动作。
- `id` (PK)
- `asset_id` (关联资产)
- `event_type` (事件类型：create / auto_discover / assign / return / repair / hardware_change / scrap)
- `title` / `description` (详情或硬件快照 JSON)
- `cost` (处理金额) / `oa_number` (OA 申请单号)
- `target_person` (领用/责任人员)
- 配件流转：`part_type` / `part_model` / `quantity` / `locker_location`
- `warranty_expiry` (维修质保截止) / `return_date` (待归还日期) / `net_value` (发生时净值)
- `review_status` (审核：10-已完成, 20-待审核, 30-已驳回)
- `operator_id` (操作人，IT或系统自动)
- `created_at` (事件发生时间)

#### 实体 6：外寄维修登记 (AssetRepair) - `asset_repairs`
对应台账"IT设备外寄维修登记表"。
- `asset_id` (关联资产)
- `oa_number` / `user_name` (送修时使用人)
- `fault_reason` (故障原因) / `diagnosis` (IT诊断结果) / `suggestion` (IT维修建议)
- `vendor` (维修厂商) / `contact_name` / `contact_phone`
- `send_date` (寄修日期) / `return_date` (寄回日期) / `cost` (维修金额) / `result` (维修结果)
- `status`：repairing(寄修中) / returned(已寄回) / scrapped(报废)
- 状态联动：登记送修时资产自动置 30-维修中，登记寄回(returned)时恢复 20-使用中，均自动写入资产履历

#### 实体 7：移动存储领用 (StorageLending) - `storage_lendings`
对应台账"移动存储领用表"（U 盘、移动硬盘等）。
- `company_id` / `department` / `borrower` (领用人)
- `borrow_date` (领用日期) / `return_date` (归还日期)
- `brand` / `spec` (规格容量) / `device_code` (设备编码)
- `quantity` (领用数量) / `return_qty` (归还数量)
- `sec_certified` (绿盾认证) / `remark`

#### 实体 8：配件出入库流水 (PartRecord) - `part_records`
对应台账"配件记录表"，是独立于单个资产的库存流水。
- `company_id`
- `direction` (出入状态：in-入库 / out-出库，常量 `PartDirectionIn/Out`)
- `operated_at` (业务发生时间，区别于记录创建时间)
- `part_type` (物品类型：内存/硬盘/键鼠等) / `part_name` / `part_model` / `brand`
- `quantity` / `unit` (单位)
- `locker_location` (IT 储物柜位置) / `location` (存放位置)
- `purpose` (用途) / `oa_number` / `asset_tag` (关联固定资产编号)
- `operator_id` (操作人)

---

## 四、 接口定义规范 (API Conventions)

采用 RESTful 风格并统一外壳包装：
- **认证**：后端管理 API 统一走 JWT (`Authorization: Bearer <token>`)。
- **响应体格式**：
  ```json
  {
    "code": 0,             // 0 为成功，非 0 为明确的业务错误
    "message": "success",  // 错误描述（用于前台展示）
    "data": { ... }        // 具体的负载
  }
  ```
- **分页结构**：
  ```json
  "data": {
    "total": 128,
    "items": [ ... ]
  }
  ```
