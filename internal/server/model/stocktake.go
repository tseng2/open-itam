package model

import "time"

// 盘点任务状态机（CIYO 对标语义）：draft → processing → finished / canceled
const (
	StocktakeStatusDraft      = 10 // 草稿（圈定范围已快照，未开始）
	StocktakeStatusProcessing = 20 // 盘点中（接受扫码核对）
	StocktakeStatusFinished   = 30 // 已完成
	StocktakeStatusCanceled   = 40 // 已取消
)

// 盘点明细核对结果：pending 是唯一起点，核对后四选一；
// 任务结束时仍为 pending 的明细即"漏盘清单"
const (
	StocktakeItemPending  = 10 // 待盘
	StocktakeItemNormal   = 20 // 正常
	StocktakeItemLost     = 30 // 丢失
	StocktakeItemDamaged  = 40 // 损坏
	StocktakeItemScrapped = 50 // 报废待处置
)

// 盘点动作在资产履历（AssetEvent）中留痕的事件类型：
// 异常核对（丢失/损坏/报废）逐资产记事件；"正常"不记（避免海量噪音），
// 其价值在 A3 协同——自动消费 pending 的 hardware_change 待审事件
const AssetEventStocktake = "stocktake"

// Stocktake 盘点任务：范围在创建时快照为明细（不随台账后续变动漂移）。
// 扫码入口令牌只落 SHA-256 哈希，明文仅在 start/rotate 时一次性返回；
// 令牌有效期 = 任务处于"盘点中"，finish/cancel 即刻失效
type Stocktake struct {
	BaseModel
	CompanyID int64  `gorm:"index;not null" json:"company_id"`
	Name      string `gorm:"type:varchar(128);not null" json:"name"`
	Remark    string `gorm:"type:text" json:"remark"`
	Status    int    `gorm:"default:10;index;not null" json:"status"` // 10 草稿 / 20 盘点中 / 30 已完成 / 40 已取消
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedBy  *int64     `gorm:"index" json:"created_by"`
	// 扫码令牌哈希（64 位 hex），永不回传前端
	ScanTokenHash string `gorm:"type:varchar(64);index" json:"-"`
}

func (Stocktake) TableName() string { return "stocktakes" }

// StocktakeItem 盘点明细：asset_tag/expected_* 为建账时点快照，
// 核对时写 result/actual_location/scanned_*；盘点中允许改判重核（最后写入生效）
type StocktakeItem struct {
	BaseModel
	CompanyID   int64  `gorm:"index;not null" json:"company_id"`
	StocktakeID int64  `gorm:"index;not null;uniqueIndex:uk_stocktake_items_task_asset" json:"stocktake_id"`
	AssetID     int64  `gorm:"index;not null;uniqueIndex:uk_stocktake_items_task_asset" json:"asset_id"`
	Asset       *Asset `gorm:"foreignKey:AssetID" json:"asset,omitempty"` // GormStore 列表预载；SQLiteStore 测试库无 assets 表不预载
	AssetTag    string `gorm:"type:varchar(64);index;not null" json:"asset_tag"` // 标签扫码匹配主键
	ExpectedLocation string `gorm:"type:varchar(128)" json:"expected_location"` // 建账位置快照
	ExpectedStatus   int    `json:"expected_status"`                             // 建账状态快照 (10库存/20在用/30维修)
	ActualLocation   string `gorm:"type:varchar(128)" json:"actual_location"`     // 实盘位置
	Result      int        `gorm:"default:10;index;not null" json:"result"` // 10 待盘 / 20 正常 / 30 丢失 / 40 损坏 / 50 报废
	ScannedBy   string     `gorm:"type:varchar(64)" json:"scanned_by"`
	ScannedAt   *time.Time `json:"scanned_at,omitempty"`
	Remark      string     `gorm:"type:varchar(255)" json:"remark"`
}

func (StocktakeItem) TableName() string { return "stocktake_items" }

// StocktakeCheck 明细核对指令：管理端按 item_id 定位，移动端按扫码 asset_tag 定位
type StocktakeCheck struct {
	ItemID         int64  `json:"item_id"`
	AssetTag       string `json:"asset_tag"`
	Result         int    `json:"result"`
	ActualLocation string `json:"actual_location"`
	Remark         string `json:"remark"`
}
