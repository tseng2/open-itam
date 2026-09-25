package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"itagent/internal/server/model"
)

// 操作日志（P2 体验运营）：追加式审计流水的 GormStore 生产实现。
// SQLiteStore 同步实现见 sqlite.go（契约测试 operation_log_test.go）。

// normalizeOperationLogPage 分页缺省与上限（与其余列表口径一致）
func normalizeOperationLogPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

// validateOperationLog 审计记录最小必填 + 时间归一 + 列宽防御：审计是
// 旁路，任何字段超宽都截断落库而非拒写（写入方含中间件、登录处理器等，
// 防止 MariaDB 严格模式因超宽把整条审计打丢）。CreatedAt 统一 UTC：
// glebarez SQLite 按带偏移的文本存取时间，写入本地/查询 UTC 会让文本
// 比较错位（MariaDB 驱动统一转换无此问题，但双实现必须同一口径）
func validateOperationLog(log *model.OperationLog) error {
	if log.Action == "" {
		return fmt.Errorf("action required")
	}
	if log.Resource == "" {
		return fmt.Errorf("resource required")
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	log.Username = truncateAuditColumn(log.Username, 128)
	log.Role = truncateAuditColumn(log.Role, 32)
	log.Action = truncateAuditColumn(log.Action, 32)
	log.Resource = truncateAuditColumn(log.Resource, 64)
	log.ResourceID = truncateAuditColumn(log.ResourceID, 64)
	log.Path = truncateAuditColumn(log.Path, 255)
	log.IP = truncateAuditColumn(log.IP, 64)
	log.UserAgent = truncateAuditColumn(log.UserAgent, 255)
	return nil
}

// truncateAuditColumn 按字节截断到列宽上限（"…" 占 3 字节，总长恰好不超）
func truncateAuditColumn(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	if limit <= 3 {
		return s[:limit]
	}
	return s[:limit-3] + "…"
}

func (s *GormStore) CreateOperationLog(ctx context.Context, log model.OperationLog) error {
	if err := validateOperationLog(&log); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(&log).Error; err != nil {
		return fmt.Errorf("create operation log: %w", err)
	}
	return nil
}

func (s *GormStore) ListOperationLogs(ctx context.Context, f OperationLogListFilter) ([]model.OperationLog, int64, error) {
	page, size := normalizeOperationLogPage(f.Page, f.PageSize)
	q := s.db.WithContext(ctx).Model(&model.OperationLog{})
	if f.CompanyID > 0 {
		q = q.Where("company_id = ?", f.CompanyID)
	}
	if f.UserID > 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.Resource != "" {
		q = q.Where("resource = ?", f.Resource)
	}
	if f.ResourceID != "" {
		q = q.Where("resource_id = ?", f.ResourceID)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		q = q.Where("username LIKE ?", "%"+kw+"%")
	}
	if f.StartTime != nil {
		start := f.StartTime.UTC()
		q = q.Where("created_at >= ?", start)
	}
	if f.EndTime != nil {
		end := f.EndTime.UTC()
		q = q.Where("created_at <= ?", end)
	}

	var total int64
	if err := q.WithContext(ctx).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count operation logs: %w", err)
	}
	items := make([]model.OperationLog, 0, size)
	if err := q.WithContext(ctx).Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list operation logs: %w", err)
	}
	return items, total, nil
}
