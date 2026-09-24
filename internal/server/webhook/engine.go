package webhook

import (
	"context"
	"log"
	"net/http"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"

	"gorm.io/gorm"
)

// DefaultScanInterval 定时扫描周期：与最小冷却窗口（1 分钟）对齐，
// 保证冷却到期能在下一分钟内补推；内部运行参数，无需暴露为用户配置
const DefaultScanInterval = time.Minute

// Engine 超期/失联 Webhook 告警引擎（阶段五 A4）。
// 单进程 goroutine + time.Ticker 定时扫描，无需分布式锁；
// 扫描判定复用 model.ResolveAssetPresence（核心即 ResolvePresence），
// 离线阈值与冷却窗口全部走配置，禁止硬编码
type Engine struct {
	db               *gorm.DB
	store            store.Store
	offlineThreshold time.Duration
	client           *http.Client
	now              func() time.Time // 注入时钟，测试冷却边界用
}

func NewEngine(db *gorm.DB, st store.Store, offlineThreshold time.Duration) *Engine {
	return &Engine{
		db:               db,
		store:            st,
		offlineThreshold: offlineThreshold,
		client:           DefaultHTTPClient,
		now:              time.Now,
	}
}

// ScanOnce 执行一轮扫描推送，返回本轮实际推送的告警条数。
// 配置未启用/未配置时安静空转；推送失败不落冷却状态，下一轮自动重试
func (e *Engine) ScanOnce(ctx context.Context) (int, error) {
	cfg, err := e.store.GetWebhookAlertConfig(ctx)
	if err != nil {
		return 0, err
	}
	if !cfg.Enabled || cfg.WebhookURL == "" {
		return 0, nil
	}
	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
	now := e.now().UTC()

	var assets []model.Asset
	if err := e.db.WithContext(ctx).Preload("Device").Find(&assets).Error; err != nil {
		return 0, err
	}
	var dispatches []model.AssetDispatch
	if err := e.db.WithContext(ctx).
		Where("status = ?", model.DispatchStatusActive).Find(&dispatches).Error; err != nil {
		return 0, err
	}
	dispatchByAsset := make(map[int64]*model.AssetDispatch, len(dispatches))
	for i := range dispatches {
		dispatchByAsset[dispatches[i].AssetID] = &dispatches[i]
	}

	alerts := BuildAlerts(assets, dispatchByAsset, now, e.offlineThreshold)
	if len(alerts) == 0 {
		return 0, nil
	}

	states, err := e.store.ListWebhookAlertStates(ctx)
	if err != nil {
		return 0, err
	}
	lastSent := make(map[AlertKey]time.Time, len(states))
	for _, st := range states {
		lastSent[AlertKey{st.CompanyID, st.AssetID, st.AlertType}] = st.SentAt
	}
	due := FilterDue(alerts, lastSent, cooldown, now)
	if len(due) == 0 {
		return 0, nil
	}

	if err := Post(ctx, e.client, cfg.WebhookURL, cfg.Secret, Payload{
		Event: EventTypeAlert, Timestamp: now, Alerts: due,
	}); err != nil {
		return 0, err
	}
	for _, a := range due {
		if err := e.store.PutWebhookAlertState(ctx, model.WebhookAlertState{
			CompanyID: a.CompanyID, AssetID: a.AssetID, AlertType: a.AlertType, SentAt: now,
		}); err != nil {
			// 冷却状态写失败仅影响去重（可能多推一次），不影响主流程
			log.Printf("[webhook] record alert state (asset %d %s): %v", a.AssetID, a.AlertType, err)
		}
	}
	return len(due), nil
}

// Run 定时扫描主循环：随服务进程常驻，ctx 取消即退出（部署重启即停）
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultScanInterval
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
				log.Printf("[webhook] scan round failed: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("[webhook] pushed %d alert(s)", n)
			}
		}
	}
}
