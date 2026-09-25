package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"itagent/internal/server/model"
)

// P2 消息中心起步（站内信）：store 契约测试（SQLiteStore 实现）。
// 契约要点：收件箱只查自己（user_id 必填）；未读过滤与计数；标记已读
// 幂等（重复标记放行）；跨用户/跨公司一律 NotFound；全部已读返回受影响数

func newNotification(companyID, userID int64, title string) model.Notification {
	return model.Notification{
		CompanyID:  companyID,
		UserID:     userID,
		Type:       model.NotificationTypeAssetRequest,
		Title:      title,
		Content:    "设备申请审批通知：" + title,
		Resource:   "asset-requests",
		ResourceID: "9",
	}
}

func TestNotificationCreateAndInbox(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateNotification(ctx, newNotification(1, 42, "新设备申请待审批"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.ReadAt != nil || created.Type != model.NotificationTypeAssetRequest {
		t.Fatalf("created mismatch: %+v", created)
	}

	// 收件箱只看自己：跨用户/跨公司一律空
	items, total, err := s.ListNotifications(ctx, NotificationListFilter{
		CompanyID: 1, UserID: 42, Page: 1, PageSize: 10,
	})
	if err != nil || total != 1 || len(items) != 1 || items[0].Title != "新设备申请待审批" {
		t.Fatalf("inbox: total=%d items=%v err=%v", total, items, err)
	}
	if _, total, _ = s.ListNotifications(ctx, NotificationListFilter{CompanyID: 1, UserID: 43, Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("other user inbox must be empty, got %d", total)
	}
	if _, total, _ = s.ListNotifications(ctx, NotificationListFilter{CompanyID: 2, UserID: 42, Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("other company inbox must be empty, got %d", total)
	}

	// company 0（顶栏铃铛无公司上下文）：user_id 即安全边界，跨公司自己的通知全可见
	if _, err := s.CreateNotification(ctx, newNotification(2, 42, "跨公司通知")); err != nil {
		t.Fatalf("seed cross-company: %v", err)
	}
	if _, total, _ = s.ListNotifications(ctx, NotificationListFilter{UserID: 42, Page: 1, PageSize: 10}); total != 2 {
		t.Fatalf("company 0 must see own rows across companies, got %d", total)
	}
	if n, _ := s.CountUnreadNotifications(ctx, 0, 42); n != 2 {
		t.Fatalf("company 0 unread count: %d", n)
	}
}

func TestNotificationUnreadFiltersAndCount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, spec := range []struct {
		user  int64
		title string
	}{
		{1, "待办 A"},
		{1, "待办 B"},
		{1, "已读 C"},
		{2, "待办 D"},
	} {
		if _, err := s.CreateNotification(ctx, newNotification(1, spec.user, spec.title)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// 已读态只能经标记动作产生（新增一律未读）：把"已读 C"标记已读
	readC, _, _ := s.ListNotifications(ctx, NotificationListFilter{
		CompanyID: 1, UserID: 1, Type: model.NotificationTypeAssetRequest, Page: 1, PageSize: 10,
	})
	for _, n := range readC {
		if n.Title == "已读 C" {
			if err := s.MarkNotificationRead(ctx, 1, 1, n.ID, time.Now().UTC()); err != nil {
				t.Fatalf("seed mark read: %v", err)
			}
		}
	}

	unread := true
	read := false
	// 未读过滤 + 计数
	items, total, err := s.ListNotifications(ctx, NotificationListFilter{
		CompanyID: 1, UserID: 1, Unread: &unread, Page: 1, PageSize: 10,
	})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("unread list: total=%d err=%v", total, err)
	}
	if _, total, _ = s.ListNotifications(ctx, NotificationListFilter{CompanyID: 1, UserID: 1, Unread: &read, Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("read list: total=%d", total)
	}
	if n, _ := s.CountUnreadNotifications(ctx, 1, 1); n != 2 {
		t.Fatalf("unread count user1: %d", n)
	}
	if n, _ := s.CountUnreadNotifications(ctx, 1, 2); n != 1 {
		t.Fatalf("unread count user2: %d", n)
	}
	// 分页倒序：最新在前
	if items[0].Title != "待办 B" {
		t.Fatalf("newest first: %+v", items)
	}

	// 类型过滤（防未注册类型混入收件箱口径）
	if _, total, _ = s.ListNotifications(ctx, NotificationListFilter{
		CompanyID: 1, UserID: 1, Type: "bogus", Page: 1, PageSize: 10,
	}); total != 0 {
		t.Fatalf("type filter: total=%d", total)
	}
}

func TestNotificationValidation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 必填校验：公司/收件人/类型/标题逐项拒绝
	for _, bad := range []model.Notification{
		{UserID: 1, Type: "x", Title: "t"},
		{CompanyID: 1, Type: "x", Title: "t"},
		{CompanyID: 1, UserID: 1, Title: "t"},
		{CompanyID: 1, UserID: 1, Type: "x"},
		{CompanyID: 1, UserID: 1, Type: "x", Title: "   "},
	} {
		if _, err := s.CreateNotification(ctx, bad); err == nil {
			t.Fatalf("must reject %+v", bad)
		}
	}

	// ReadAt 归一：即便事件源带了已读时间，新增通知也一律未读
	n := newNotification(1, 5, "归一校验")
	read := time.Now().UTC()
	n.ReadAt = &read
	created, err := s.CreateNotification(ctx, n)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ReadAt != nil {
		t.Fatal("new notification must always be unread")
	}
}

func TestNotificationMarkRead(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	n, _ := s.CreateNotification(ctx, newNotification(1, 7, "审批结果"))
	s.CreateNotification(ctx, newNotification(1, 7, "第二条"))

	now := time.Now().UTC()
	// 标记已读：未读计数 2 → 1
	if err := s.MarkNotificationRead(ctx, 1, 7, n.ID, now); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if cnt, _ := s.CountUnreadNotifications(ctx, 1, 7); cnt != 1 {
		t.Fatalf("unread after mark: %d", cnt)
	}
	// 幂等：重复标记放行（鼓励语义，铃铛轮询与点击天然并发）
	if err := s.MarkNotificationRead(ctx, 1, 7, n.ID, now); err != nil {
		t.Fatalf("re-mark read must be idempotent: %v", err)
	}
	// companyID 0（不限公司）同样可标记自己的通知（已读幂等路径）
	if err := s.MarkNotificationRead(ctx, 0, 7, n.ID, now); err != nil {
		t.Fatalf("company 0 mark must pass: %v", err)
	}
	// 越权：跨用户一律 NotFound
	if err := s.MarkNotificationRead(ctx, 1, 8, n.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user mark must be NotFound, got %v", err)
	}
	if err := s.MarkNotificationRead(ctx, 2, 7, n.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company mark must be NotFound, got %v", err)
	}
	if err := s.MarkNotificationRead(ctx, 1, 7, 99999, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bogus id must be NotFound, got %v", err)
	}

	// 全部已读：只影响自己的未读，返回受影响数
	affected, err := s.MarkAllNotificationsRead(ctx, 1, 7, now)
	if err != nil || affected != 1 {
		t.Fatalf("mark all: affected=%d err=%v", affected, err)
	}
	if cnt, _ := s.CountUnreadNotifications(ctx, 1, 7); cnt != 0 {
		t.Fatalf("all read: %d", cnt)
	}
	// 幂等空转：再次全部已读不报错不误报
	if affected, err = s.MarkAllNotificationsRead(ctx, 1, 7, now); err != nil || affected != 0 {
		t.Fatalf("mark all idempotent: affected=%d err=%v", affected, err)
	}
}
