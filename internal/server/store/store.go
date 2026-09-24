package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"itagent/internal/server/model"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrUnauthorized  = errors.New("invalid credentials")
	ErrAlreadyExists = errors.New("record already exists")
	ErrInvalidState  = errors.New("invalid state transition")
)

// DispatchListFilter 外派登记列表查询条件；Overdue 三态：
// nil 不过滤 / true 仅超期未归（外派中且已过预计归期）/ false 排除超期未归
type DispatchListFilter struct {
	CompanyID int64
	AssetID   int64
	Status    int
	Overdue   *bool
	Page      int
	PageSize  int
}

type Device struct {
	DeviceID     string    `json:"device_id"`
	DeviceToken  string    `json:"-"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	AgentVersion string    `json:"agent_version"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	RegisteredAt time.Time `json:"registered_at"`
}

type Report struct {
	ID         int64           `json:"id"`
	DeviceID   string          `json:"device_id"`
	ReportType string          `json:"report_type"`
	Payload    json.RawMessage `json:"payload"`
	ReportedAt time.Time       `json:"reported_at"`
	ReceivedAt time.Time       `json:"received_at"`
}

type Snapshot struct {
	DeviceID  string
	Payload   json.RawMessage
	UpdatedAt time.Time
}

type ChangeEvent struct {
	ID        int64             `json:"id"`
	DeviceID  string            `json:"device_id"`
	Kind      string            `json:"kind"`
	Severity  string            `json:"severity"`
	Message   string            `json:"message"`
	Detail    map[string]string `json:"detail,omitempty"`
	Acked     bool              `json:"acked"`
	CreatedAt time.Time         `json:"created_at"`
}

type Store interface {
	RegisterDevice(ctx context.Context, d Device) (token string, err error)
	Authenticate(ctx context.Context, deviceID, token string) error
	UpsertDeviceSeen(ctx context.Context, d Device) error
	SaveReport(ctx context.Context, r Report) (int64, error)
	GetDevice(ctx context.Context, deviceID string) (Device, error)
	ListDevices(ctx context.Context, limit, offset int) ([]Device, error)
	ListReports(ctx context.Context, deviceID string, limit, offset int) ([]Report, error)
	GetSnapshot(ctx context.Context, deviceID string) (Snapshot, error)
	SaveSnapshot(ctx context.Context, s Snapshot) error
	SaveChangeEvent(ctx context.Context, e ChangeEvent) (int64, error)
	ListChangeEvents(ctx context.Context, includeAcked bool, limit, offset int) ([]ChangeEvent, error)
	AckChangeEvent(ctx context.Context, id int64) error
	// 防护模块（防退出/防卸载）与卸载验证码：密码归服务端集中管理
	GetProtectionModule(ctx context.Context, key string) (model.ProtectionModule, error)
	PutProtectionModule(ctx context.Context, m model.ProtectionModule) error
	CreateUninstallCode(ctx context.Context, deviceID string, ttl time.Duration) (model.UninstallCode, error)
	VerifyUninstallCode(ctx context.Context, deviceID, code string) error
	// 外派登记（阶段五 A1）：一个资产同时只允许一条外派中记录
	CreateDispatch(ctx context.Context, d model.AssetDispatch) (model.AssetDispatch, error)
	ListDispatches(ctx context.Context, f DispatchListFilter) ([]model.AssetDispatch, int64, error)
	ReturnDispatch(ctx context.Context, companyID, id int64, returnedAt time.Time) (model.AssetDispatch, error)
	CancelDispatch(ctx context.Context, companyID, id int64) (model.AssetDispatch, error)
	GetActiveDispatchByAsset(ctx context.Context, companyID, assetID int64) (model.AssetDispatch, error)
	// 超期/失联 Webhook 告警（阶段五 A4）：配置存 DB（单例），冷却状态按
	// (company_id, asset_id, alert_type) 去重，进程重启不丢
	GetWebhookAlertConfig(ctx context.Context) (model.WebhookAlertConfig, error)
	PutWebhookAlertConfig(ctx context.Context, cfg model.WebhookAlertConfig) error
	ListWebhookAlertStates(ctx context.Context) ([]model.WebhookAlertState, error)
	PutWebhookAlertState(ctx context.Context, st model.WebhookAlertState) error
	// 盘点任务（阶段五 P0-β）：任务状态机 + 明细快照 + 扫码核对。
	// 圈定范围的资产查询在 API 层完成（SQLiteStore 测试库无 assets 表），
	// CreateStocktake 收到的即是已快照好的明细；扫码令牌只落哈希，
	// 明文仅在 Start/Rotate 时一次性返回，有效期 = 任务处于盘点中
	CreateStocktake(ctx context.Context, st model.Stocktake, items []model.StocktakeItem) (model.Stocktake, error)
	ListStocktakes(ctx context.Context, f StocktakeListFilter) ([]model.Stocktake, int64, error)
	GetStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, error)
	StartStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, string, error)
	RotateStocktakeToken(ctx context.Context, companyID, id int64) (string, error)
	FinishStocktake(ctx context.Context, companyID, id int64, finishedAt time.Time) (model.Stocktake, error)
	CancelStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, error)
	GetStocktakeByToken(ctx context.Context, token string) (model.Stocktake, error)
	ListStocktakeItems(ctx context.Context, f StocktakeItemListFilter) ([]model.StocktakeItem, int64, error)
	CheckStocktakeItems(ctx context.Context, companyID, stocktakeID int64, checks []model.StocktakeCheck, scannedBy string) ([]model.StocktakeItem, error)
	CountStocktakeResults(ctx context.Context, companyID, stocktakeID int64) (map[int]int64, error)
	// 设备申请（阶段五 P0-β）：提交即指定库存资产。审批通过的
	// 「状态流转 + 资产绑定 + 领用履历」三步同事务在 API 层完成
	//（GORM 跨表事务；SQLiteStore 测试库无 assets 表），这里只管申请表本身的原子流转
	CreateAssetRequest(ctx context.Context, r model.AssetRequest) (model.AssetRequest, error)
	ListAssetRequests(ctx context.Context, f AssetRequestListFilter) ([]model.AssetRequest, int64, error)
	GetAssetRequest(ctx context.Context, companyID, id int64) (model.AssetRequest, error)
	RejectAssetRequest(ctx context.Context, companyID, id int64, approverID int64, remark string, now time.Time) (model.AssetRequest, error)
	CancelAssetRequest(ctx context.Context, companyID, id int64) (model.AssetRequest, error)
	// 折旧规则（阶段五 P0-β）：规则 CRUD，引擎按规则定时刷资产净值。
	// 规则被资产引用时的删除拦截在 API 层完成（需查 assets 表，
	// SQLiteStore 测试库无该表），store 只管规则表本身
	CreateDepreciationRule(ctx context.Context, r model.DepreciationRule) (model.DepreciationRule, error)
	ListDepreciationRules(ctx context.Context, f DepreciationRuleListFilter) ([]model.DepreciationRule, int64, error)
	GetDepreciationRule(ctx context.Context, companyID, id int64) (model.DepreciationRule, error)
	UpdateDepreciationRule(ctx context.Context, r model.DepreciationRule) (model.DepreciationRule, error)
	DeleteDepreciationRule(ctx context.Context, companyID, id int64) error
	Close() error
}

// StocktakeListFilter 盘点任务列表查询条件
type StocktakeListFilter struct {
	CompanyID int64
	Status    int
	Page      int
	PageSize  int
}

// StocktakeItemListFilter 盘点明细列表查询条件：Keyword 模糊匹配 asset_tag
type StocktakeItemListFilter struct {
	CompanyID   int64
	StocktakeID int64
	Result      int
	Keyword     string
	Page        int
	PageSize    int
}

// AssetRequestListFilter 设备申请列表查询条件
type AssetRequestListFilter struct {
	CompanyID   int64
	Status      int
	ApplicantID int64
	AssetID     int64
	Page        int
	PageSize    int
}

// DepreciationRuleListFilter 折旧规则列表查询条件（公司维度全量，量级小）
type DepreciationRuleListFilter struct {
	CompanyID int64
	Page      int
	PageSize  int
}
