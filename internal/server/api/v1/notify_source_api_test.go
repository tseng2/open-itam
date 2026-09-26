package v1

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"itagent/internal/server/webhook"
)

// 阶段五收官 · 事件源扩展：三类运营提醒的投递闭包与沿触发链路测试。
// 覆盖：A4 告警扇出（引擎注入闭包）、许可到期扇出（引擎注入闭包）、
// 耗材低库存沿触发（postTxn 旁路）——通知落库 + 防轰炸 + 旁路不阻塞

func setupNotifySourceDB(t *testing.T) {
	t.Helper()
	db, err := store.InitDB("sqlite", t.TempDir()+"/notify_source_test.db")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
}

// seedNotifySourceFixture 公司 + admin + super_admin + 普通用户各一
func seedNotifySourceFixture(t *testing.T) (companyID, adminID, superID, userID int64) {
	t.Helper()
	company := model.Company{Name: "事件源测试公司-" + t.Name() + time.Now().Format("150405.000000000")}
	if err := store.DB.Create(&company).Error; err != nil {
		t.Fatalf("seed company: %v", err)
	}
	admin := model.User{CompanyID: company.ID, Username: "itadmin", RealName: "管理员", Role: "admin", Status: "active"}
	super := model.User{CompanyID: company.ID, Username: "root", RealName: "超管", Role: "super_admin", Status: "active"}
	user := model.User{CompanyID: company.ID, Username: "zhangsan", RealName: "张三", Role: "user", Status: "active"}
	for _, u := range []*model.User{&admin, &super, &user} {
		if err := store.DB.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	return company.ID, admin.ID, super.ID, user.ID
}

func listNotificationsByType(t *testing.T, companyID int64, ntype string) []model.Notification {
	t.Helper()
	var items []model.Notification
	if err := store.DB.Where("company_id = ? AND type = ?", companyID, ntype).
		Order("id").Find(&items).Error; err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	return items
}

func TestAlertNotifierFansOutToCompanyAdmins(t *testing.T) {
	setupNotifySourceDB(t)
	companyID, adminID, superID, userID := seedNotifySourceFixture(t)
	notifier := NewAlertNotifier()

	// 超期未归告警 → 扇出给该公司全部在册管理员（admin + super_admin）
	err := notifier(context.Background(), webhook.Alert{
		CompanyID: companyID, AssetID: 42, AssetTag: "AST-ALERT-1",
		AlertType: model.WebhookAlertOverdue,
		Message:   "外派超期未归：资产 AST-ALERT-1（ThinkPad X1）负责人 李四，目的地 上海，预计归期 2026-09-23 18:00",
	})
	if err != nil {
		t.Fatalf("alert notifier: %v", err)
	}
	items := listNotificationsByType(t, companyID, model.NotificationTypeAssetAlert)
	if len(items) != 2 {
		t.Fatalf("expected 2 admin notifications, got %+v", items)
	}
	recipients := map[int64]bool{}
	for _, n := range items {
		if n.Title != "资产告警：超期未归" || n.Resource != "assets" || n.ResourceID != "42" {
			t.Fatalf("alert notification content: %+v", n)
		}
		if !strings.Contains(n.Content, "AST-ALERT-1") {
			t.Fatalf("alert content must carry asset context: %+v", n)
		}
		recipients[n.UserID] = true
	}
	if !recipients[adminID] || !recipients[superID] || recipients[userID] {
		t.Fatalf("recipients must be company admins only: %+v", recipients)
	}

	// 疑似失联告警 → 标题区分；普通用户仍不收
	if err := notifier(context.Background(), webhook.Alert{
		CompanyID: companyID, AssetID: 43, AlertType: model.WebhookAlertMissing,
		Message: "疑似失联：资产 AST-ALERT-2 最近心跳 2026-09-24 10:00:00",
	}); err != nil {
		t.Fatalf("alert notifier: %v", err)
	}
	items = listNotificationsByType(t, companyID, model.NotificationTypeAssetAlert)
	if len(items) != 4 {
		t.Fatalf("expected 4 after second alert, got %d", len(items))
	}
	if items[2].Title != "资产告警：疑似失联" {
		t.Fatalf("missing-alert title: %+v", items[2])
	}

	// 无管理员的公司：无人可通知是数据状态而非故障，不算错误
	empty := model.Company{Name: "无管理员公司-" + t.Name(), Code: "NADM"}
	if err := store.DB.Create(&empty).Error; err != nil {
		t.Fatalf("seed empty company: %v", err)
	}
	if err := notifier(context.Background(), webhook.Alert{
		CompanyID: empty.ID, AssetID: 1, AlertType: model.WebhookAlertMissing, Message: "x",
	}); err != nil {
		t.Fatalf("no-admin company must not error: %v", err)
	}
	if len(listNotificationsByType(t, empty.ID, model.NotificationTypeAssetAlert)) != 0 {
		t.Fatalf("no-admin company must create nothing")
	}
}

// geo_roaming 第六类通知（GeoIP 二期）：独立类型不复用 asset_alert，
// title 带资产编码、resource=assets 跳资产页、扇出所属公司管理员
func TestAlertNotifierGeoRoamingProducesSixthType(t *testing.T) {
	setupNotifySourceDB(t)
	companyID, adminID, superID, userID := seedNotifySourceFixture(t)
	notifier := NewAlertNotifier()

	err := notifier(context.Background(), webhook.Alert{
		CompanyID: companyID, AssetID: 77, AssetTag: "AST-ROAM-1",
		AlertType: model.WebhookAlertGeoRoaming,
		Message: "异地漫游：资产 AST-ROAM-1（ThinkPad X1）所属公司区域 广东省|东莞市，出口 IP 222.92.0.1 解析区域 江苏省|苏州市；最近心跳 2026-09-26 12:00:00",
	})
	if err != nil {
		t.Fatalf("geo roaming notifier: %v", err)
	}

	// 第六类独立落库：geo_roaming 与 asset_alert 分离
	items := listNotificationsByType(t, companyID, model.NotificationTypeGeoRoaming)
	if len(items) != 2 {
		t.Fatalf("expected 2 geo_roaming notifications, got %+v", items)
	}
	if len(listNotificationsByType(t, companyID, model.NotificationTypeAssetAlert)) != 0 {
		t.Fatal("geo_roaming must not fall into asset_alert type")
	}
	recipients := map[int64]bool{}
	for _, n := range items {
		if n.Title != "异地漫游提醒：AST-ROAM-1" {
			t.Fatalf("geo title must carry asset tag: %+v", n)
		}
		if n.Resource != "assets" || n.ResourceID != "77" {
			t.Fatalf("geo notification must link asset: %+v", n)
		}
		if !strings.Contains(n.Content, "江苏省|苏州市") || !strings.Contains(n.Content, "广东省|东莞市") {
			t.Fatalf("geo content must carry judgment basis: %+v", n)
		}
		recipients[n.UserID] = true
	}
	// 扇出收口：公司 admin + super_admin，普通用户不收
	if !recipients[adminID] || !recipients[superID] || recipients[userID] {
		t.Fatalf("geo notify must reach company admins only: %+v", recipients)
	}
}

func TestLicenseExpiringNotifierFansOut(t *testing.T) {
	setupNotifySourceDB(t)
	companyID, adminID, superID, userID := seedNotifySourceFixture(t)
	notifier := NewLicenseExpiringNotifier()

	expiration := time.Now().UTC().AddDate(0, 0, 20)
	if err := notifier(context.Background(), model.License{
		CompanyID: companyID, Name: "Microsoft 365 商业高级版", Vendor: "Microsoft",
		ExpirationDate: &expiration,
	}, 20); err != nil {
		t.Fatalf("license notifier: %v", err)
	}

	items := listNotificationsByType(t, companyID, model.NotificationTypeLicenseExpiring)
	if len(items) != 2 {
		t.Fatalf("expected 2 admin notifications, got %+v", items)
	}
	recipients := map[int64]bool{}
	for _, n := range items {
		if n.Title != "软件许可到期提醒" || n.Resource != "licenses" {
			t.Fatalf("license notification content: %+v", n)
		}
		if !strings.Contains(n.Content, "Microsoft 365 商业高级版") ||
			!strings.Contains(n.Content, "20") ||
			!strings.Contains(n.Content, expiration.Format("2006-01-02")) {
			t.Fatalf("license content must carry name/days/date: %+v", n)
		}
		recipients[n.UserID] = true
	}
	if !recipients[adminID] || !recipients[superID] || recipients[userID] {
		t.Fatalf("license notify must reach admins only: %+v", recipients)
	}
}

func TestConsumableLowStockEdgeTriggerNotify(t *testing.T) {
	// 真实路由面：沿触发防轰炸——只有「旧库存 > 预警线 且 新库存 ≤ 预警线」
	// 的那笔流水才投；库存持续低位不再轰炸，回补后再次击穿才会再投
	r := setupConsumableRouter(t)
	companyID, adminID, userID := seedConsumableFixture(t)
	admin := userTokenFor(t, adminID, "itadmin", "admin")

	c := createConsumableViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "A4 打印纸", "spec": "80g/500张/包", "unit": "包", "min_quantity": 10,
	})

	count := func() int64 {
		var n int64
		if err := store.DB.Model(&model.Notification{}).
			Where("company_id = ? AND type = ?", companyID, model.NotificationTypeConsumableLowStock).
			Count(&n).Error; err != nil {
			t.Fatalf("count low stock notifications: %v", err)
		}
		return n
	}
	stockTxn := func(path string, body gin.H) {
		t.Helper()
		rec := doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d%s", c.ID, path), admin, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("txn %s: http=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// 首笔入库 0→100：旧库存 0 在线下，不算「沿」（建账低位不投）
	stockTxn("/stock-in", gin.H{"company_id": companyID, "quantity": 100})
	if got := count(); got != 0 {
		t.Fatalf("first stock-in must not notify, got %d", got)
	}
	// 出库 95：100→5 沿触线（旧 100 > 10 且新 5 ≤ 10）→ 投 1
	stockTxn("/stock-out", gin.H{"company_id": companyID, "quantity": 95})
	if got := count(); got != 1 {
		t.Fatalf("edge crossing must notify once, got %d", got)
	}
	// 继续出库 5→3：旧 5 已在线下 → 不投（防轰炸）
	stockTxn("/stock-out", gin.H{"company_id": companyID, "quantity": 2})
	if got := count(); got != 1 {
		t.Fatalf("staying low must not re-notify, got %d", got)
	}
	// 回补 3→53（向线上穿）→ 不投
	stockTxn("/stock-in", gin.H{"company_id": companyID, "quantity": 50})
	if got := count(); got != 1 {
		t.Fatalf("restock must not notify, got %d", got)
	}
	// 再次出库 53→3：第二次沿触发 → 再投 1
	stockTxn("/stock-out", gin.H{"company_id": companyID, "quantity": 50})
	if got := count(); got != 2 {
		t.Fatalf("second crossing must notify again, got %d", got)
	}

	// 内容与收件人：触线提醒投给公司管理员，文案带名称与数量
	items := listNotificationsByType(t, companyID, model.NotificationTypeConsumableLowStock)
	if len(items) != 2 {
		t.Fatalf("low stock notifications: %+v", items)
	}
	for _, n := range items {
		if n.Title != "耗材库存预警" || n.Resource != "consumables" || n.UserID != adminID {
			t.Fatalf("low stock notification content: %+v", n)
		}
		if !strings.Contains(n.Content, "A4 打印纸") || !strings.Contains(n.Content, "预警线 10") {
			t.Fatalf("low stock content: %+v", n)
		}
	}
	// 普通用户不收触线提醒
	var userGot int64
	store.DB.Model(&model.Notification{}).
		Where("company_id = ? AND type = ? AND user_id = ?", companyID, model.NotificationTypeConsumableLowStock, userID).
		Count(&userGot)
	if userGot != 0 {
		t.Fatalf("normal user must not receive low stock alerts")
	}

	// 未配预警线（min_quantity=0）的耗材降到 0 → 永不预警（口径与列表一致）
	c2 := createConsumableViaAPI(t, r, admin, gin.H{
		"company_id": companyID, "name": "签字笔", "unit": "支", "min_quantity": 0,
	})
	rec := doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-in", c2.ID), admin,
		gin.H{"company_id": companyID, "quantity": 5})
	if rec.Code != http.StatusOK {
		t.Fatalf("stock-in c2: %d", rec.Code)
	}
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-out", c2.ID), admin,
		gin.H{"company_id": companyID, "quantity": 5})
	if rec.Code != http.StatusOK {
		t.Fatalf("stock-out c2: %d", rec.Code)
	}
	if got := count(); got != 2 {
		t.Fatalf("no-line consumable must never notify, got %d", got)
	}

	// 旁路不阻塞：出库失败（库存不足 409）不产生通知，响应语义不变
	rec = doDispatchJSON(t, r, http.MethodPost, fmt.Sprintf("/api/v1/consumables/%d/stock-out", c2.ID), admin,
		gin.H{"company_id": companyID, "quantity": 99})
	if rec.Code != http.StatusConflict {
		t.Fatalf("insufficient must 409, got %d", rec.Code)
	}
	if got := count(); got != 2 {
		t.Fatalf("failed txn must not notify, got %d", got)
	}
}
