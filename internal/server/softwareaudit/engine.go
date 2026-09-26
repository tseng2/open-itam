package softwareaudit

import (
	"context"
	"fmt"
	"log"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"

	"gorm.io/gorm"
)

// DefaultScanInterval 与 Agent full 上报节奏（每小时）对齐
//（webhook/licensealert 引擎先例：单进程 goroutine + Ticker）
const DefaultScanInterval = time.Hour

// DefaultCooldown 同一池项超用提醒的默认冷却窗口（24h）；
// server.json software_overuse_cooldown_hours 可覆盖（0 = 用默认）
const DefaultCooldown = 24 * time.Hour

// StateAlertType 冷却去重键：复用 webhook_alert_states 同表
//（licensealert 先例），asset_id 列存池项 ID——同一池项冷却窗内只投一次
const StateAlertType = "software_overuse"

// Notifier 超用提醒投递器：收件人策略（公司管理员扇出）与文案归
// 消息中心侧（api/v1），main 注入闭包组装——引擎反向 import api/v1
// 会循环依赖，函数注入是唯一方向（webhook/licensealert 同款）
type Notifier func(ctx context.Context, pool model.SoftwarePool, installs, totalSeats int64) error

// Engine 软件合规引擎：每小时扫描各公司受控池 × 终端软件安装，
// 超用池项（挂接许可且安装数 > 席位）经冷却去重后提醒公司管理员。
// 未受控商业软件只在报表页呈现、不投通知（清单量大易轰炸，
// 管理员在合规视图甄别——口径见契约文档）
type Engine struct {
	db       *gorm.DB
	store    store.Store
	notifier Notifier
	cooldown time.Duration
	now      func() time.Time
}

func NewEngine(db *gorm.DB, st store.Store, cooldown time.Duration, n Notifier) *Engine {
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	return &Engine{db: db, store: st, notifier: n, cooldown: cooldown, now: time.Now}
}

// ScanOnce 执行一轮合规扫描，返回本轮送达条数。notifier 未注入时安静
// 空转；单条投递失败不落冷却状态（下一轮重试），不中断本轮其他池项
//（通知是业务旁路，永远弱于业务成功）
func (e *Engine) ScanOnce(ctx context.Context) (int, error) {
	if e.notifier == nil {
		return 0, nil
	}

	// 全部配置了受控池的公司（软删除自动过滤）
	var companyIDs []int64
	if err := e.db.WithContext(ctx).Model(&model.SoftwarePool{}).
		Distinct().Pluck("company_id", &companyIDs).Error; err != nil {
		return 0, fmt.Errorf("list pool companies: %w", err)
	}
	if len(companyIDs) == 0 {
		return 0, nil
	}

	// 冷却状态：只关心 software_overuse 键且仍在冷却窗内的行
	states, err := e.store.ListWebhookAlertStates(ctx)
	if err != nil {
		return 0, fmt.Errorf("list alert states: %w", err)
	}
	now := e.now().UTC()
	lastSent := make(map[int64]struct{}, len(states))
	for _, st := range states {
		if st.AlertType == StateAlertType && now.Sub(st.SentAt) < e.cooldown {
			lastSent[st.AssetID] = struct{}{} // asset_id 列存池项 ID
		}
	}

	sent := 0
	for _, companyID := range companyIDs {
		var pools []model.SoftwarePool
		if err := e.db.WithContext(ctx).Where("company_id = ?", companyID).Find(&pools).Error; err != nil {
			return sent, fmt.Errorf("list pools (company %d): %w", companyID, err)
		}
		if len(pools) == 0 {
			continue
		}

		licenseMap := make(map[int64]model.License, len(pools))
		for _, p := range pools {
			if p.LicenseID != nil {
				var lic model.License
				if err := e.db.WithContext(ctx).First(&lic, *p.LicenseID).Error; err == nil {
					licenseMap[lic.ID] = lic
				}
			}
		}

		installs, err := ListSoftwareInstalls(ctx, e.db, companyID)
		if err != nil {
			return sent, fmt.Errorf("list installs (company %d): %w", companyID, err)
		}

		report := BuildCompliance(pools, licenseMap, installs)
		poolByID := make(map[int64]model.SoftwarePool, len(pools))
		for _, p := range pools {
			poolByID[p.ID] = p
		}
		for _, item := range report.PoolItems {
			if !item.Overused {
				continue
			}
			if _, done := lastSent[item.PoolID]; done {
				continue
			}
			if err := e.notifier(ctx, poolByID[item.PoolID], item.Installs, int64(item.TotalSeats)); err != nil {
				log.Printf("[softwareaudit] notify pool %d (company %d): %v", item.PoolID, companyID, err)
				continue
			}
			sent++
			if err := e.store.PutWebhookAlertState(ctx, model.WebhookAlertState{
				CompanyID: companyID, AssetID: item.PoolID, AlertType: StateAlertType, SentAt: now,
			}); err != nil {
				log.Printf("[softwareaudit] record state (pool %d): %v", item.PoolID, err)
			}
		}
	}
	return sent, nil
}

// Run 定时扫描主循环：随服务进程常驻，ctx 取消即退出。
// 启动即扫一轮——重启后超用池项立即补投，不空等一个周期
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultScanInterval
	}
	if n, err := e.ScanOnce(ctx); err != nil {
		log.Printf("[softwareaudit] startup scan failed: %v", err)
	} else if n > 0 {
		log.Printf("[softwareaudit] delivered %d software overuse notification(s) at startup", n)
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
				log.Printf("[softwareaudit] scan round failed: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("[softwareaudit] delivered %d software overuse notification(s)", n)
			}
		}
	}
}
