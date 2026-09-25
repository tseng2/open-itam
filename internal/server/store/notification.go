package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"itagent/internal/server/model"
)

// 消息中心起步（P2 体验运营）：站内信收件箱的 GormStore 生产实现。
// SQLiteStore 同步实现见 sqlite.go（契约测试 notification_test.go）。
// 通知是业务旁路：任何推送失败只上报错误，绝不阻塞主流程（写入方保证）

// validateNotification 站内信最小必填 + 归一：收件人 + 公司边界 + 类型 +
// 标题；ReadAt 强制 nil——新增通知一律未读，已读态只能经标记动作产生
//（与"新增外派必须 status=10"同模式，防止事件源带偏已读语义）
func validateNotification(n *model.Notification) error {
	if n.CompanyID <= 0 {
		return fmt.Errorf("company_id required")
	}
	if n.UserID <= 0 {
		return fmt.Errorf("user_id required")
	}
	if n.Type == "" {
		return fmt.Errorf("type required")
	}
	if strings.TrimSpace(n.Title) == "" {
		return fmt.Errorf("title required")
	}
	n.ReadAt = nil
	return nil
}

func (s *GormStore) CreateNotification(ctx context.Context, n model.Notification) (model.Notification, error) {
	if err := validateNotification(&n); err != nil {
		return model.Notification{}, err
	}
	if err := s.db.WithContext(ctx).Create(&n).Error; err != nil {
		return model.Notification{}, fmt.Errorf("create notification: %w", err)
	}
	return n, nil
}

func (s *GormStore) ListNotifications(ctx context.Context, f NotificationListFilter) ([]model.Notification, int64, error) {
	if f.UserID <= 0 {
		return []model.Notification{}, 0, nil
	}
	page, size := normalizeOperationLogPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ?", f.UserID)
	// CompanyID 0 = 不过滤公司（顶栏铃铛无公司上下文；user_id 即安全边界）
	if f.CompanyID > 0 {
		q = q.Where("company_id = ?", f.CompanyID)
	}
	if f.Unread != nil {
		if *f.Unread {
			q = q.Where("read_at IS NULL")
		} else {
			q = q.Where("read_at IS NOT NULL")
		}
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	var total int64
	if err := q.WithContext(ctx).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}
	items := make([]model.Notification, 0, size)
	if err := q.WithContext(ctx).Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	return items, total, nil
}

func (s *GormStore) CountUnreadNotifications(ctx context.Context, companyID, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	q := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID)
	if companyID > 0 {
		q = q.Where("company_id = ?", companyID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return n, nil
}

// MarkNotificationRead 标记单条已读：条件更新未读行；零命中时区分
// 不存在（含跨用户/跨公司）与已读（幂等放行）。companyID 0 = 不限公司
func (s *GormStore) MarkNotificationRead(ctx context.Context, companyID, userID, id int64, readAt time.Time) error {
	if userID <= 0 {
		return ErrNotFound
	}
	q := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", id, userID)
	if companyID > 0 {
		q = q.Where("company_id = ?", companyID)
	}
	res := q.Updates(map[string]any{"read_at": readAt, "updated_at": readAt})
	if res.Error != nil {
		return fmt.Errorf("mark notification read: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		return nil
	}
	check := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("id = ? AND user_id = ?", id, userID)
	if companyID > 0 {
		check = check.Where("company_id = ?", companyID)
	}
	var exists int64
	if err := check.Count(&exists).Error; err != nil {
		return fmt.Errorf("check notification: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	return nil // 已读重复标记：幂等放行
}

func (s *GormStore) MarkAllNotificationsRead(ctx context.Context, companyID, userID int64, readAt time.Time) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	q := s.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID)
	if companyID > 0 {
		q = q.Where("company_id = ?", companyID)
	}
	res := q.Updates(map[string]any{"read_at": readAt, "updated_at": readAt})
	if res.Error != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", res.Error)
	}
	return res.RowsAffected, nil
}
