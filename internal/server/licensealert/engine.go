package licensealert

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"

	"gorm.io/gorm"
)

// DefaultScanInterval 到期提醒扫描节奏：窗口以天计，每小时足够
//（webhook/depreciation 引擎先例：单进程 goroutine + Ticker）
const DefaultScanInterval = time.Hour

// DefaultExpiringWindowDays 到期提醒窗口默认 30 天；
// server.json license_expiring_days 可覆盖（0 = 用默认）
const DefaultExpiringWindowDays = 30

// StateAlertType 冷却去重键：复用 webhook_alert_states 表（A4 同表
// 模式），asset_id 列存许可 ID——同一许可窗口期内只提醒一次
//（冷却 = 窗口天数：许可在窗口内至多停留 N 天，天然只投一次；
// 续期后再次进入窗口时冷却早已过期，会重新提醒）
const StateAlertType = "license_expiring"

// scanPageSize ListLicenses 分页批量大小（与 store 分页上限对齐）
const scanPageSize = 100

// Notifier 单条到期提醒投递器：收件人策略（公司管理员扇出）与文案归
// 消息中心侧（api/v1），main 注入闭包组装，避免引擎反向依赖 api/v1
type Notifier func(ctx context.Context, l model.License, daysLeft int) error

// Engine 软件许可到期提醒引擎：每小时扫描各公司许可池，落在
// ExpiringDays 窗口内且未终止的许可 → 提醒公司管理员。
// 窗口口径复用 store.ListLicenses 的 ExpiringDays 过滤（单源，勿重写）
type Engine struct {
	db         *gorm.DB
	store      store.Store
	windowDays int
	notifier   Notifier
}

func NewEngine(db *gorm.DB, st store.Store, windowDays int, notifier Notifier) *Engine {
	if windowDays <= 0 {
		windowDays = DefaultExpiringWindowDays
	}
	return &Engine{db: db, store: st, windowDays: windowDays, notifier: notifier}
}

// ScanOnce 执行一轮扫描提醒，返回本轮送达条数。notifier 未注入时
// 安静空转；单条投递失败不落冷却状态（下一轮重试），不中断本轮其他
// 许可（通知是业务旁路，永远弱于业务成功）
func (e *Engine) ScanOnce(ctx context.Context) (int, error) {
	if e.notifier == nil {
		return 0, nil
	}

	// 全部持有许可的公司（软删除自动过滤）
	var companyIDs []int64
	if err := e.db.WithContext(ctx).Model(&model.License{}).
		Distinct().Pluck("company_id", &companyIDs).Error; err != nil {
		return 0, fmt.Errorf("list license companies: %w", err)
	}
	if len(companyIDs) == 0 {
		return 0, nil
	}

	// 冷却状态：只关心 license_expiring 键且仍在窗口期内的行
	states, err := e.store.ListWebhookAlertStates(ctx)
	if err != nil {
		return 0, fmt.Errorf("list alert states: %w", err)
	}
	now := time.Now().UTC()
	cooldown := time.Duration(e.windowDays) * 24 * time.Hour
	lastSent := make(map[int64]struct{}, len(states))
	for _, st := range states {
		if st.AlertType == StateAlertType && now.Sub(st.SentAt) < cooldown {
			lastSent[st.AssetID] = struct{}{} // asset_id 列存许可 ID
		}
	}

	sent := 0
	for _, companyID := range companyIDs {
		page := 1
		for {
			items, total, err := e.store.ListLicenses(ctx, store.LicenseListFilter{
				CompanyID:    companyID,
				ExpiringDays: e.windowDays,
				Page:         page,
				PageSize:     scanPageSize,
			})
			if err != nil {
				return sent, fmt.Errorf("list expiring licenses (company %d): %w", companyID, err)
			}
			for i := range items {
				if _, done := lastSent[items[i].ID]; done {
					continue
				}
				daysLeft := int(math.Ceil(items[i].ExpirationDate.Sub(now).Hours() / 24))
				if err := e.notifier(ctx, items[i], daysLeft); err != nil {
					log.Printf("[licensealert] notify license %d (company %d): %v", items[i].ID, companyID, err)
					continue
				}
				sent++
				if err := e.store.PutWebhookAlertState(ctx, model.WebhookAlertState{
					CompanyID: companyID, AssetID: items[i].ID, AlertType: StateAlertType, SentAt: now,
				}); err != nil {
					log.Printf("[licensealert] record state (license %d): %v", items[i].ID, err)
				}
			}
			if int64(page*scanPageSize) >= total {
				break
			}
			page++
		}
	}
	return sent, nil
}

// Run 定时扫描主循环：随服务进程常驻，ctx 取消即退出（部署重启即停）。
// 启动即扫一轮——重启后窗口内的到期许可立即补投，不空等一个周期
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultScanInterval
	}
	if n, err := e.ScanOnce(ctx); err != nil {
		log.Printf("[licensealert] startup scan failed: %v", err)
	} else if n > 0 {
		log.Printf("[licensealert] delivered %d expiring license notification(s) at startup", n)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := e.ScanOnce(ctx)
			if err != nil {
				log.Printf("[licensealert] scan round failed: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("[licensealert] delivered %d expiring license notification(s)", n)
			}
		}
	}
}
