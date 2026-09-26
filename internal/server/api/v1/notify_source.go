package v1

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"itagent/internal/server/licensealert"
	"itagent/internal/server/model"
	"itagent/internal/server/softwareaudit"
	"itagent/internal/server/store"
	"itagent/internal/server/webhook"
)

// 阶段五收官 · 消息中心事件源扩展（三类运营提醒 → 公司管理员）：
// A4 告警联动站内信（webhook 引擎注入）、软件许可到期提醒
//（licensealert 引擎注入）、耗材低库存沿触发（consumable postTxn 旁路）。
// 阶段三追加软件超用提醒（softwareaudit 引擎注入）。
// 引擎场景无 gin 上下文，经 Notifier 函数注入组装（webhook ↔ api/v1
// 循环依赖规避：引擎包只定义回调类型，收件人策略与文案收敛在本包）

// notifyCompanyAdminsCtx 引擎场景（无 gin 上下文）的公司管理员扇出：
// 通知该公司全部在册 admin + super_admin。查不到管理员返回 0（无人可
// 通知是数据状态而非故障）；单条投递失败记日志不中断（旁路语义，与
// gin 版 notifyCompanyAdmins 同口径）。返回成功投递条数
func notifyCompanyAdminsCtx(ctx context.Context, tpl model.Notification) (int, error) {
	var admins []model.User
	if err := store.DB.WithContext(ctx).
		Where("company_id = ? AND role IN ? AND status = ?", tpl.CompanyID, []string{"admin", "super_admin"}, "active").
		Find(&admins).Error; err != nil {
		return 0, fmt.Errorf("list admins for notify: %w", err)
	}
	s := store.NewGormStore(store.DB)
	sent := 0
	for _, a := range admins {
		n := tpl
		n.UserID = a.ID
		if _, err := s.CreateNotification(ctx, n); err != nil {
			log.Printf("[notify] fanout to admin %d (%q): %v", a.ID, tpl.Title, err)
			continue
		}
		sent++
	}
	return sent, nil
}

// NewAlertNotifier A4 告警站内信投递闭包：main 组装注入给 webhook 引擎。
// 告警判定口径在 webhook.BuildAlerts（单源），本闭包只负责收件人扇出与
// 文案；冷却去重在引擎侧（notify_* 独立键），站内信独立于 WebHook 开关。
// geo_roaming 产出第六类通知（GeoIP 二期）：类型独立不复用 asset_alert
//（消息中心独立过滤），title 带资产编码便于铃铛直读
func NewAlertNotifier() webhook.AlertNotifier {
	return func(ctx context.Context, alert webhook.Alert) error {
		nType := model.NotificationTypeAssetAlert
		if alert.AlertType == model.WebhookAlertGeoRoaming {
			nType = model.NotificationTypeGeoRoaming
		}
		_, err := notifyCompanyAdminsCtx(ctx, model.Notification{
			CompanyID:  alert.CompanyID,
			Type:       nType,
			Title:      alertNotificationTitle(alert.AlertType, alert.AssetTag),
			Content:    alert.Message,
			Resource:   "assets",
			ResourceID: strconv.FormatInt(alert.AssetID, 10),
		})
		return err
	}
}

func alertNotificationTitle(alertType, assetTag string) string {
	switch alertType {
	case model.WebhookAlertOverdue:
		return "资产告警：超期未归"
	case model.WebhookAlertMissing:
		return "资产告警：疑似失联"
	case model.WebhookAlertGeoRoaming:
		return "异地漫游提醒：" + assetTag
	default:
		return "资产告警"
	}
}

// NewLicenseExpiringNotifier 许可到期提醒投递闭包：main 组装注入给
// licensealert 引擎。窗口口径与去重在引擎侧（ExpiringDays 单源 +
// webhook_alert_states 同表 license_expiring 键），本闭包只管扇出与文案
func NewLicenseExpiringNotifier() licensealert.Notifier {
	return func(ctx context.Context, l model.License, daysLeft int) error {
		_, err := notifyCompanyAdminsCtx(ctx, model.Notification{
			CompanyID:  l.CompanyID,
			Type:       model.NotificationTypeLicenseExpiring,
			Title:      "软件许可到期提醒",
			Content: fmt.Sprintf("许可 %s 将于 %d 天内到期（到期日 %s），请评估续约或替换",
				l.Name, daysLeft, l.ExpirationDate.Format("2006-01-02")),
			Resource:   "licenses",
			ResourceID: strconv.FormatInt(l.ID, 10),
		})
		return err
	}
}

// NewSoftwareOveruseNotifier 软件超用提醒投递闭包（阶段三合规引擎注入）：
// 比对口径与冷却去重在 softwareaudit 引擎侧，本闭包只管扇出与文案
func NewSoftwareOveruseNotifier() softwareaudit.Notifier {
	return func(ctx context.Context, pool model.SoftwarePool, installs, totalSeats int64) error {
		_, err := notifyCompanyAdminsCtx(ctx, model.Notification{
			CompanyID:  pool.CompanyID,
			Type:       model.NotificationTypeSoftwareOveruse,
			Title:      "软件超用提醒",
			Content: fmt.Sprintf("受控软件「%s」当前安装 %d 台终端，已超过挂接许可席位 %d，请核查授权或扩容（合规报表见软件与授权许可页）",
				pool.Name, installs, totalSeats),
			Resource:   "software",
			ResourceID: strconv.FormatInt(pool.ID, 10),
		})
		return err
	}
}
