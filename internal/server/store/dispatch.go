package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"itagent/internal/server/model"

	"gorm.io/gorm"
)

const maxDispatchPageSize = 100

// validateDispatch 外派登记必填校验：公司边界、资产、负责人、预计归期。
// ExpectedReturnAt 是 A2 失联分层与 A4 超期告警的数据依据，缺失则整条登记失去业务意义
func validateDispatch(d model.AssetDispatch) error {
	if d.CompanyID <= 0 {
		return fmt.Errorf("company_id required")
	}
	if d.AssetID <= 0 {
		return fmt.Errorf("asset_id required")
	}
	if d.BorrowerName == "" {
		return fmt.Errorf("borrower_name required")
	}
	if d.ExpectedReturnAt.IsZero() {
		return fmt.Errorf("expected_return_at required")
	}
	return nil
}

// normalizeDispatchPage 分页兜底，防止零/负值与超大页拖垮查询
func normalizeDispatchPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > maxDispatchPageSize {
		pageSize = maxDispatchPageSize
	}
	return page, pageSize
}

// CreateDispatch 登记外派：事务内先校验"一个资产仅一条外派中"再落库，
// 避免并发窗口内漏检产生双活跃记录；状态由服务端控制，只允许以"外派中"创建
func (s *GormStore) CreateDispatch(ctx context.Context, d model.AssetDispatch) (model.AssetDispatch, error) {
	if err := validateDispatch(d); err != nil {
		return model.AssetDispatch{}, err
	}
	if d.Status == 0 {
		d.Status = model.DispatchStatusActive
	}
	if d.Status != model.DispatchStatusActive {
		return model.AssetDispatch{}, fmt.Errorf("new dispatch must be in active status")
	}
	if d.DispatchedAt.IsZero() {
		d.DispatchedAt = time.Now().UTC()
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.AssetDispatch{}).
			Where("company_id = ? AND asset_id = ? AND status = ?",
				d.CompanyID, d.AssetID, model.DispatchStatusActive).
			Count(&count).Error; err != nil {
			return fmt.Errorf("count active dispatches: %w", err)
		}
		if count > 0 {
			return ErrAlreadyExists
		}
		return tx.Create(&d).Error
	})
	if err != nil {
		return model.AssetDispatch{}, fmt.Errorf("create dispatch: %w", err)
	}
	return d, nil
}

// ListDispatches 分页查询外派登记：公司边界、资产、状态、超期四类过滤，
// Overdue 依据"外派中且已过预计归期"的实时计算，不落独立状态位
func (s *GormStore) ListDispatches(ctx context.Context, f DispatchListFilter) ([]model.AssetDispatch, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)

	q := s.db.WithContext(ctx).Model(&model.AssetDispatch{})
	if f.CompanyID > 0 {
		q = q.Where("company_id = ?", f.CompanyID)
	}
	if f.AssetID > 0 {
		q = q.Where("asset_id = ?", f.AssetID)
	}
	if f.Status > 0 {
		q = q.Where("status = ?", f.Status)
	}
	if f.Overdue != nil {
		now := time.Now().UTC()
		if *f.Overdue {
			q = q.Where("status = ? AND expected_return_at < ?", model.DispatchStatusActive, now)
		} else {
			q = q.Where("NOT (status = ? AND expected_return_at < ?)", model.DispatchStatusActive, now)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count dispatches: %w", err)
	}
	items := make([]model.AssetDispatch, 0, size)
	if err := q.Preload("Asset").Order("id desc").
		Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list dispatches: %w", err)
	}
	return items, total, nil
}

// transitionDispatchStatus 原子条件更新：仅"外派中"可流转到归还/作废；
// 未命中时二次区分"不存在或跨公司"与"状态不允许"，两类失败语义不同
func (s *GormStore) transitionDispatchStatus(ctx context.Context, companyID, id int64, toStatus int, returnedAt *time.Time) (model.AssetDispatch, error) {
	updates := map[string]interface{}{"status": toStatus, "updated_at": time.Now().UTC()}
	if returnedAt != nil {
		updates["returned_at"] = *returnedAt
	}
	res := s.db.WithContext(ctx).Model(&model.AssetDispatch{}).
		Where("id = ? AND company_id = ? AND status = ?", id, companyID, model.DispatchStatusActive).
		Updates(updates)
	if res.Error != nil {
		return model.AssetDispatch{}, fmt.Errorf("update dispatch status: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		var exists int64
		if err := s.db.WithContext(ctx).Model(&model.AssetDispatch{}).
			Where("id = ? AND company_id = ?", id, companyID).
			Count(&exists).Error; err != nil {
			return model.AssetDispatch{}, fmt.Errorf("check dispatch exists: %w", err)
		}
		if exists == 0 {
			return model.AssetDispatch{}, ErrNotFound
		}
		return model.AssetDispatch{}, ErrInvalidState
	}

	var d model.AssetDispatch
	if err := s.db.WithContext(ctx).Preload("Asset").First(&d, id).Error; err != nil {
		return model.AssetDispatch{}, fmt.Errorf("reload dispatch: %w", err)
	}
	return d, nil
}

// ReturnDispatch 归还外派：记录实际归还时间并流转到"已归还"
func (s *GormStore) ReturnDispatch(ctx context.Context, companyID, id int64, returnedAt time.Time) (model.AssetDispatch, error) {
	return s.transitionDispatchStatus(ctx, companyID, id, model.DispatchStatusReturned, &returnedAt)
}

// CancelDispatch 作废外派登记（误登记等管理修正，不记归还时间）
func (s *GormStore) CancelDispatch(ctx context.Context, companyID, id int64) (model.AssetDispatch, error) {
	return s.transitionDispatchStatus(ctx, companyID, id, model.DispatchStatusCanceled, nil)
}

// GetActiveDispatchByAsset 查资产的进行中外派，供详情卡片与 A2 失联分层计算使用
func (s *GormStore) GetActiveDispatchByAsset(ctx context.Context, companyID, assetID int64) (model.AssetDispatch, error) {
	var d model.AssetDispatch
	err := s.db.WithContext(ctx).
		Where("company_id = ? AND asset_id = ? AND status = ?",
			companyID, assetID, model.DispatchStatusActive).
		First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AssetDispatch{}, ErrNotFound
	}
	if err != nil {
		return model.AssetDispatch{}, fmt.Errorf("get active dispatch by asset: %w", err)
	}
	return d, nil
}
