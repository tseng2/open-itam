package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

// validateDepreciationRule 折旧规则必填校验：公司边界、名称、总月数。
// stages/残值等业务口径校验在 API 层经 depreciation.ValidateRuleSpec 完成，
// store 只守数据完整性底线（SQLiteStore 与 GormStore 同一套规则）
func validateDepreciationRule(r model.DepreciationRule) error {
	if r.CompanyID <= 0 {
		return fmt.Errorf("company_id required")
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name required")
	}
	if r.Months <= 0 {
		return fmt.Errorf("months required")
	}
	return nil
}

// CreateDepreciationRule 新建折旧规则；Enabled 由调用方显式传入
//（API 层把"未传"解析为启用），store 不做缺省替换
func (s *GormStore) CreateDepreciationRule(ctx context.Context, r model.DepreciationRule) (model.DepreciationRule, error) {
	if err := validateDepreciationRule(r); err != nil {
		return model.DepreciationRule{}, err
	}
	if r.FloorType == "" {
		r.FloorType = "percent"
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return model.DepreciationRule{}, fmt.Errorf("create depreciation rule: %w", err)
	}
	return r, nil
}

// ListDepreciationRules 分页查询公司折旧规则，按 id 倒序与其它列表一致
func (s *GormStore) ListDepreciationRules(ctx context.Context, f DepreciationRuleListFilter) ([]model.DepreciationRule, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)

	q := s.db.WithContext(ctx).Model(&model.DepreciationRule{}).
		Where("company_id = ?", f.CompanyID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count depreciation rules: %w", err)
	}
	items := make([]model.DepreciationRule, 0, size)
	if err := q.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list depreciation rules: %w", err)
	}
	return items, total, nil
}

// GetDepreciationRule 按 id 查规则，公司边界强制（跨公司视同不存在）
func (s *GormStore) GetDepreciationRule(ctx context.Context, companyID, id int64) (model.DepreciationRule, error) {
	var r model.DepreciationRule
	err := s.db.WithContext(ctx).
		Where("id = ? AND company_id = ?", id, companyID).
		First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DepreciationRule{}, ErrNotFound
	}
	if err != nil {
		return model.DepreciationRule{}, fmt.Errorf("get depreciation rule: %w", err)
	}
	return r, nil
}

// UpdateDepreciationRule 覆写规则（主键 + 公司边界条件更新），返回更新后的完整记录
func (s *GormStore) UpdateDepreciationRule(ctx context.Context, r model.DepreciationRule) (model.DepreciationRule, error) {
	if err := validateDepreciationRule(r); err != nil {
		return model.DepreciationRule{}, err
	}
	if r.ID == 0 {
		return model.DepreciationRule{}, fmt.Errorf("id required")
	}
	r.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&model.DepreciationRule{}).
		Where("id = ? AND company_id = ?", r.ID, r.CompanyID).
		Select("name", "months", "floor_type", "floor_val", "stages", "enabled", "remark", "updated_at").
		Updates(r)
	if res.Error != nil {
		return model.DepreciationRule{}, fmt.Errorf("update depreciation rule: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.DepreciationRule{}, ErrNotFound
	}
	return s.GetDepreciationRule(ctx, r.CompanyID, r.ID)
}

// DeleteDepreciationRule 软删除规则；幂等语义：不存在/跨公司一律 NotFound
func (s *GormStore) DeleteDepreciationRule(ctx context.Context, companyID, id int64) error {
	res := s.db.WithContext(ctx).Delete(&model.DepreciationRule{}, "id = ? AND company_id = ?", id, companyID)
	if res.Error != nil {
		return fmt.Errorf("delete depreciation rule: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
