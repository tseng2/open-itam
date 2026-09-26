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

// AlertNotifier 告警站内信投递器：引擎判定出告警后回调。站内信的收件人
// 策略（公司管理员扇出）与文案归消息中心侧（api/v1），经函数注入组装，
// 避免 webhook → api/v1 反向依赖循环（api/v1 已 import 本包）
type AlertNotifier func(ctx context.Context, alert Alert) error

// NotifyAlertType 站内信通道的独立冷却键：复用 webhook_alert_states 同表，
// notify_ 前缀与 WebHook 通道键隔离——同一告警两条通道各自计窗
func NotifyAlertType(alertType string) string {
	return "notify_" + alertType
}

// Engine 超期/失联告警引擎（阶段五 A4 + 收官站内信联动）。
// 双通道出站：WebHook（配置开关控制）+ 站内信（注入 notifier，独立于
// WebHook 开关——系统内通道，WebHook 未配置也该收到）。
// 单进程 goroutine + time.Ticker 定时扫描，无需分布式锁；
// 扫描判定复用 model.ResolveAssetPresence（核心即 ResolvePresence），
// 阈值与漫游地理基准经函数注入（唯一源 agent_settings，设置页保存即生效
// ——闭包实时读库，A4 静态注入 + main 组装的先例方向不变），禁止硬编码
type Engine struct {
	db          *gorm.DB
	store       store.Store
	thresholdFn func() time.Duration
	geoFn       func() model.PresenceGeo
	client      *http.Client
	now         func() time.Time // 注入时钟，测试冷却边界用
	notifier    AlertNotifier
}

func NewEngine(db *gorm.DB, st store.Store, thresholdFn func() time.Duration, geoFn func() model.PresenceGeo, notifier AlertNotifier) *Engine {
	return &Engine{
		db:          db,
		store:       st,
		thresholdFn: thresholdFn,
		geoFn:       geoFn,
		client:      DefaultHTTPClient,
		now:         time.Now,
		notifier:    notifier,
	}
}

// ScanOnce 执行一轮扫描投递，返回本轮实际送达条数（站内信 + WebHook）。
// WebHook 通道：配置未启用/未配置时静默；推送失败不落冷却状态，下一轮重试。
// 站内信通道：只要注入了 notifier 就投递（独立于 WebHook 开关）；投递
// 失败同样不落冷却状态（旁路语义，绝不影响另一通道）
func (e *Engine) ScanOnce(ctx context.Context) (int, error) {
	cfg, err := e.store.GetWebhookAlertConfig(ctx)
	if err != nil {
		return 0, err
	}
	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
	webhookOn := cfg.Enabled && cfg.WebhookURL != ""
	// 双通道全关才安静空转（notifier 未注入 = 部署裁剪/测试场景）
	if !webhookOn && e.notifier == nil {
		return 0, nil
	}
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

	alerts := BuildAlerts(assets, dispatchByAsset, now, e.thresholdFn(), e.geoFn())
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

	delivered := 0
	if e.notifier != nil {
		delivered += e.notifyAlerts(ctx, alerts, lastSent, cooldown, now)
	}

	if webhookOn {
		due := FilterDue(alerts, lastSent, cooldown, now)
		if len(due) > 0 {
			if err := Post(ctx, e.client, cfg.WebhookURL, cfg.Secret, Payload{
				Event: EventTypeAlert, Timestamp: now, Alerts: due,
			}); err != nil {
				return delivered, err
			}
			for _, a := range due {
				if err := e.store.PutWebhookAlertState(ctx, model.WebhookAlertState{
					CompanyID: a.CompanyID, AssetID: a.AssetID, AlertType: a.AlertType, SentAt: now,
				}); err != nil {
					// 冷却状态写失败仅影响去重（可能多推一次），不影响主流程
					log.Printf("[webhook] record alert state (asset %d %s): %v", a.AssetID, a.AlertType, err)
				}
			}
			delivered += len(due)
		}
	}
	return delivered, nil
}

// notifyAlerts 站内信通道：按 notify_* 独立键做冷却去重后逐条回调投递。
// 单条投递失败只记日志不落冷却状态（下一轮重试），也绝不中断本轮其他
// 告警与 WebHook 通道（通知语义永远弱于业务成功）
func (e *Engine) notifyAlerts(ctx context.Context, alerts []Alert, lastSent map[AlertKey]time.Time, cooldown time.Duration, now time.Time) int {
	sent := 0
	for _, a := range alerts {
		key := AlertKey{a.CompanyID, a.AssetID, NotifyAlertType(a.AlertType)}
		if last, ok := lastSent[key]; ok && now.Sub(last) < cooldown {
			continue
		}
		if err := e.notifier(ctx, a); err != nil {
			log.Printf("[webhook] notify alert (asset %d %s): %v", a.AssetID, a.AlertType, err)
			continue
		}
		sent++
		if err := e.store.PutWebhookAlertState(ctx, model.WebhookAlertState{
			CompanyID: a.CompanyID, AssetID: a.AssetID, AlertType: key.AlertType, SentAt: now,
		}); err != nil {
			log.Printf("[webhook] record notify state (asset %d): %v", a.AssetID, err)
		}
	}
	return sent
}

// Run 定时扫描主循环：随服务进程常驻，ctx 取消即退出（部署重启即停）。
// 启动即扫一轮——重启后未处理告警立即补投，不空等一个周期
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultScanInterval
	}
	if n, err := e.ScanOnce(ctx); err != nil {
		log.Printf("[webhook] startup scan failed: %v", err)
	} else if n > 0 {
		log.Printf("[webhook] delivered %d alert notification(s) at startup", n)
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
				log.Printf("[webhook] delivered %d alert notification(s)", n)
			}
		}
	}
}
